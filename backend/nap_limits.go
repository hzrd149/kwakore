package backend

import "time"

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

// napNow is the clock every NAP limit reads. Tests freeze it, so a bucket's
// count does not drift while a test loop runs (Pitfall 7).
var napNow = time.Now
