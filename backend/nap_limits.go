package backend

import (
	"time"

	"golang.org/x/time/rate"
)

// Every napplet-facing limit lives in this file (D-14): what one envelope may
// weigh, how long a correlation id may be, and how long a prompt a request
// raises may stay open. The host page (napplet-host.js) keeps its own,
// tighter caps, but those are advisory: a bypassed host page reaches Go with
// whatever it likes, so Go checks everything again after decoding.
//
// Sizes count bytes of the envelope's JSON as Go receives it (UTF-8, after the
// host page's JSON-string wrapping is undone). The host page counts UTF-16
// code units, so a value heavy in multi-byte text or escapes can pass the
// page's cap and still be too large here; it then gets a clear too-large
// answer, never a hang. Every comparison is exact: len > cap rejects,
// len == cap is accepted.

const (
	// napMaxEnvelope is the hard cap on one envelope (D-09): the largest
	// route cap (upload.upload). Anything above it is dropped unanswered;
	// the wire line cap (MaxInboundWireMsg, 02-02) sits just above it.
	napMaxEnvelope = 24 << 20
	// napMaxParams caps the nap.msg params before they are unquoted: the
	// host page sends the envelope as a JSON string, so escapes can double
	// it, plus room for the quotes (D-09).
	napMaxParams = 2*napMaxEnvelope + 1<<20

	// napDefaultMaxRaw is a route's cap unless it declares its own (D-09).
	napDefaultMaxRaw = 256 << 10
	// napMaxRawUpload is upload.upload's cap: a 16 MiB file base64-encoded,
	// plus its metadata (D-09, the host page's MAX_UPLOAD_ENVELOPE).
	napMaxRawUpload = 24 << 20
	// napMaxRawStorageSet leaves room above the 512 KiB storage quota for
	// the key, the scope and JSON escaping (D-09).
	napMaxRawStorageSet = 600 << 10
	// napMaxRawRegisterSchema bounds a config schema declaration (D-09).
	napMaxRawRegisterSchema = 64 << 10
	// napMaxRawPublish fits a signed event carrying a large content field,
	// the host page's MAX_ENVELOPE (D-18).
	napMaxRawPublish = 1 << 20

	// napMaxIDBytes bounds a correlation id (and a subId): it is echoed back
	// in every reply, so it must stay small (D-11). A string id is measured
	// after JSON unescaping, a number on its raw token.
	napMaxIDBytes = 128
	// napMaxTopKeys bounds an envelope's top-level keys: the head parse
	// builds a map of them (D-10).
	napMaxTopKeys = 64
)

// Prompt deadlines (D-20): how long a prompt raised for a request may stay
// open. Routes declare theirs (napRoute.deadline); this is the default.
const (
	// napDeadlineDefault matches the shim's 30 s request timeout: an answer
	// later than that reaches nobody.
	napDeadlineDefault = 30 * time.Second
	// napDeadlineStorage matches the storage shim's 5 s timeout.
	napDeadlineStorage = 5 * time.Second
)

// Counts declared here and enforced elsewhere (D-14, D-15, D-16).
const (
	// napQueueSlots is the dispatch queue per window; a full queue answers
	// rate-limited instead of blocking the reader (D-16, the host page's
	// MAX_PENDING). Enforced by napEnqueue.
	napQueueSlots = 256
	// napMaxPendingPromptsPerWindow and napMaxPendingPromptsGlobal bound the
	// prompts waiting for the user; more are denied rate-limited (D-15).
	// Enforced by enqueueNappPrompt (window_prompt.go).
	napMaxPendingPromptsPerWindow = 3
	napMaxPendingPromptsGlobal    = 32
	// resourceMaxInFlight is NAP-RESOURCE's "10 in-flight" (RS-5), counting
	// resource.bytes and resource.bytesMany. Enforced by resourceAtCapacity
	// (nap_resource.go).
	resourceMaxInFlight = 10
	// incMaxChannels bounds the INC channels a window is an end of (I-3).
	// Enforced by 02-07.
	incMaxChannels = 32
	// uploadMaxActive bounds the uploads pending or uploading per window
	// (U-4). Enforced by 02-07.
	uploadMaxActive = 4
)

// ─── rate limits ─────────────────────────────────────────────────

// Rate limits are token buckets (golang.org/x/time/rate, D-13), one set per
// window (napSession.limits). They never block: a request over its limit is
// answered rate-limited in its route's shape (reply-less types are dropped),
// and only that window is affected. They never reset with the session
// either: a napplet that reloads its frame must not get a full set of
// tokens back for it (notify.send and config.openSettings included).

// napLimitClass is a category of request with a bucket of its own (D-14).
// limitNone, the zero value, is charged nothing beyond the envelope bucket.
type napLimitClass uint8

const (
	limitNone napLimitClass = iota
	// limitPrompt: creating a prompt (askApproval, askActionHandler)
	limitPrompt
	// limitLink: link.open
	limitLink
	// limitIntent: intent.invoke
	limitIntent
	// limitColdLaunch: an intent that has to launch a napp first (charged by
	// intent.invoke's actionOptions.BeforeLaunch)
	limitColdLaunch
	// limitUpload: upload.upload
	limitUpload
	// limitResource: resource fetches, one token per URL: the dispatcher
	// charges one per request (the route's class), and resource.bytesMany's
	// handler charges the rest of its URLs
	limitResource
	// limitIncOpen: inc.channel.open
	limitIncOpen
	// limitIncEmit: inc.emit, inc.channel.emit, inc.channel.broadcast
	limitIncEmit
	// limitPublish: everything that signs and publishes
	limitPublish
	// limitNotify and limitNotifyUrgent: notifications (charged by
	// notify.send)
	limitNotify
	limitNotifyUrgent
	// limitOpenSettings: config.openSettings (charged by its handler)
	limitOpenSettings

	limitCount
)

// napLimitSpec is one bucket: its refill rate and its size.
type napLimitSpec struct {
	every rate.Limit
	burst int
}

// napEnvelopeLimit is every envelope a window sends, whatever its type
// (D-14: about 200/s, burst 400).
var napEnvelopeLimit = napLimitSpec{rate.Limit(200), 400}

// napLimitSpecs are the category buckets, by class.
var napLimitSpecs = [limitCount]napLimitSpec{
	// L-1: a napplet that keeps asking gets backed off
	limitPrompt: {rate.Every(6 * time.Second), 5},
	// L-1: links open in the user's browser, one prompt each
	limitLink: {rate.Every(2 * time.Second), 5},
	// N-5: intents route to other napps
	limitIntent: {rate.Every(time.Second), 10},
	// D-14: cold launches per minute (6/min, three at once)
	limitColdLaunch: {rate.Every(10 * time.Second), 3},
	// U-4: uploads are big and go to the network
	limitUpload: {rate.Every(6 * time.Second), 5},
	// D-19, NAP-RESOURCE RS-5: 60 requests a minute, bulk counted per URL;
	// the burst covers resource.info's maxUrls of 100 (Pitfall 10)
	limitResource: {rate.Every(time.Second), 100},
	// I-3: channel opens
	limitIncOpen: {rate.Every(time.Second), 10},
	// I-3: emits are cheap but fan out to every subscriber
	limitIncEmit: {rate.Limit(50), 100},
	// research A6: an "always allow publish" rule must not let a napplet
	// sign and publish at the envelope rate
	limitPublish: {rate.Every(time.Second), 10},
	// notify.send: 20 notifications a minute
	limitNotify: {rate.Every(3 * time.Second), 20},
	// notify.send: 3 urgent notifications a minute, also counted in the 20
	limitNotifyUrgent: {rate.Every(20 * time.Second), 3},
	// config.openSettings: one settings window per 2 s, so a napplet cannot
	// keep throwing its settings window in the user's face
	limitOpenSettings: {rate.Every(2 * time.Second), 1},
}

// napLimiter is one window's buckets. A nil *napLimiter allows everything
// (a napCall built by hand in a test has no session limiter). rate.Limiter
// is safe for concurrent use, so callers take no lock of their own.
type napLimiter struct {
	envelope *rate.Limiter
	classes  [limitCount]*rate.Limiter
}

// newNapLimiter is a window's buckets at the defaults above.
func newNapLimiter() *napLimiter { return newNapLimiterWith(napEnvelopeLimit, napLimitSpecs) }

// newNapLimiterWith builds buckets from the given specs (tests use tiny ones).
func newNapLimiterWith(envelope napLimitSpec, classes [limitCount]napLimitSpec) *napLimiter {
	l := &napLimiter{envelope: rate.NewLimiter(envelope.every, envelope.burst)}
	for class := limitNone + 1; class < limitCount; class++ {
		spec := classes[class]
		l.classes[class] = rate.NewLimiter(spec.every, spec.burst)
	}
	return l
}

// allow takes n tokens from class's bucket, or reports that it cannot. More
// than the bucket's burst never fits. limitNone always fits.
func (l *napLimiter) allow(class napLimitClass, n int) bool {
	if l == nil || class == limitNone || class >= limitCount {
		return true
	}
	return l.classes[class].AllowN(napNow(), n)
}

// allowEnvelope takes one token from the envelope bucket.
func (l *napLimiter) allowEnvelope() bool {
	if l == nil {
		return true
	}
	return l.envelope.AllowN(napNow(), 1)
}

// napNow is the clock every NAP limit reads. Tests freeze it, so a bucket's
// count does not drift while a test loop runs (Pitfall 7).
var napNow = time.Now
