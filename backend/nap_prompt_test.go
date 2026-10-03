package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// ─── test rig ────────────────────────────────────────────────────

// pendingPrompts is every prompt waiting for the user: the active one first,
// then the queue in order.
func pendingPrompts() []*Prompt {
	promptMu.Lock()
	defer promptMu.Unlock()
	var out []*Prompt
	if promptActive != nil {
		out = append(out, promptActive)
	}
	return append(out, promptQueue...)
}

// promptIDs is pendingPrompts by ID, in queue order.
func promptIDs() []int {
	var ids []int
	for _, p := range pendingPrompts() {
		ids = append(ids, p.ID)
	}
	return ids
}

// promptsFor is the pending prompts over one window.
func promptsFor(instance string) []*Prompt {
	var out []*Prompt
	for _, p := range pendingPrompts() {
		if p.Instance == instance {
			out = append(out, p)
		}
	}
	return out
}

// waitPromptsFor waits until exactly n prompts are pending over the window.
func waitPromptsFor(t *testing.T, instance string, n int) []*Prompt {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := promptsFor(instance)
		if len(got) == n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d prompts pending over %s, want %d", len(got), instance, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// cancelAllPrompts takes every pending prompt down, as dismissed.
func cancelAllPrompts() {
	for _, p := range pendingPrompts() {
		cancelPrompt(p)
	}
}

// cleanPrompts starts the test with no prompt pending and leaves none behind.
// Prompts are package state, so these tests never run in parallel.
func cleanPrompts(t *testing.T) {
	t.Helper()
	cancelAllPrompts()
	t.Cleanup(cancelAllPrompts)
}

// promptTestHost opens links (counting them safely across async handlers).
type promptTestHost struct {
	noopHost
	mu     sync.Mutex
	opened []string
}

func (h *promptTestHost) OpenLink(url string) error {
	h.mu.Lock()
	h.opened = append(h.opened, url)
	h.mu.Unlock()
	return nil
}

func (h *promptTestHost) links() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.opened)
}

// withAskRoute registers test.ask: a link.open in miniature, asking
// PermOpenLink per call and opening https://example.com/asked on yes, with
// the given prompt deadline (0 for the default).
func withAskRoute(t *testing.T, deadline time.Duration) {
	t.Helper()
	withTestRoute(t, "test.ask", napRoute{
		h: func(c *napCall) {
			c.async(func(context.Context) {
				ok, err := c.approve(PermOpenLink, "open a test link", "", "")
				if err != nil {
					c.failForPrompt(err)
					return
				}
				if !ok {
					c.failWith(napErrDenied)
					return
				}
				if err := c.openLink("https://example.com/asked"); err != nil {
					return
				}
				c.reply(map[string]any{"status": "opened"})
			})
		},
		gate:     perCallGate(PermOpenLink),
		fail:     failShape(failLink),
		deadline: deadline,
	})
}

// withRouteDeadline gives a registered route another prompt deadline until
// restore runs (or the test ends). The route is replaced, not edited: calls
// already holding the old one keep it.
func withRouteDeadline(t *testing.T, typ string, d time.Duration) (restore func()) {
	t.Helper()
	orig := napRoutes[typ]
	if orig == nil {
		t.Fatalf("no route %s", typ)
	}
	r := *orig
	r.deadline = d
	napRoutes[typ] = &r
	var once sync.Once
	restore = func() { once.Do(func() { napRoutes[typ] = orig }) }
	t.Cleanup(restore)
	return restore
}

// ─── bounds ──────────────────────────────────────────────────────

// TestPromptQueueBoundsPerWindowAndGlobal: a window has at most 3 prompts
// pending and all windows together 32 (D-15). The 3rd and the 32nd are
// accepted, the 4th and the 33rd refused at once with rate-limited in the
// route's shape, leaving the queue as it was.
func TestPromptQueueBoundsPerWindowAndGlobal(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	h := &promptTestHost{}
	host = h
	withAskRoute(t, 0)
	ci, rec := openNapplet(t, "prompt-bounds")
	ready(t, ci, rec, 1)

	for i := 1; i <= 3; i++ {
		post(t, ci, map[string]any{"type": "test.ask", "id": "a" + strconv.Itoa(i)})
	}
	pending := waitPromptsFor(t, ci.instance, 3)
	if CurrentPrompt() != pending[0] || PendingPrompts() != 2 {
		t.Fatalf("want one shown and two queued, got active %v and %d queued", CurrentPrompt(), PendingPrompts())
	}
	before := promptIDs()

	post(t, ci, map[string]any{"type": "test.ask", "id": "a4"})
	got := waitID(t, rec, "test.ask.result", "a4")
	if got["status"] != "denied" || got["error"] != napErrRateLimited {
		t.Fatalf("4th prompt: %v", got)
	}
	if after := promptIDs(); !slices.Equal(before, after) {
		t.Fatalf("a refused prompt changed the queue: %v -> %v", before, after)
	}
	if n := len(rec.find("test.ask.result")); n != 1 {
		t.Fatalf("the refusal answered other requests too: %v", rec.find("test.ask.result"))
	}

	t.Run("global", func(t *testing.T) {
		cancelAllPrompts()
		for i := range napMaxPendingPromptsGlobal {
			p := newPrompt("flood", "global "+strconv.Itoa(i), "", "", nil)
			p.Instance = "global-" + strconv.Itoa(i)
			if !enqueueNappPrompt(p) {
				t.Fatalf("instance prompt %d of %d refused", i+1, napMaxPendingPromptsGlobal)
			}
		}
		before := promptIDs()
		p := newPrompt("flood", "one too many", "", "", nil)
		p.Instance = "global-extra"
		if enqueueNappPrompt(p) {
			t.Fatalf("instance prompt %d accepted", napMaxPendingPromptsGlobal+1)
		}
		if after := promptIDs(); !slices.Equal(before, after) {
			t.Fatalf("a refused prompt changed the queue: %v -> %v", before, after)
		}
	})
}

// TestLauncherPromptsAreExempt: with the global napplet bound reached, a
// prompt the launcher raises itself (an install confirmation) still queues
// and can be answered.
func TestLauncherPromptsAreExempt(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	for i := range napMaxPendingPromptsGlobal {
		p := newPrompt("flood", "flood "+strconv.Itoa(i), "", "", nil)
		p.Instance = "flood-" + strconv.Itoa(i)
		if !enqueueNappPrompt(p) {
			t.Fatalf("instance prompt %d refused", i+1)
		}
	}

	p := newPrompt("Verdana", "Install and open the napp?", "", "", nil)
	answered := make(chan bool, 1)
	go func() {
		enqueuePrompt(p)
		answered <- p.wait().OK
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !slices.Contains(promptIDs(), p.ID) {
		if time.Now().After(deadline) {
			t.Fatal("the launcher prompt was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	// an empty Instance goes through enqueueNappPrompt unbounded too
	q := newPrompt("Verdana", "Another launcher question", "", "", nil)
	if !enqueueNappPrompt(q) {
		t.Fatal("a launcher prompt was refused by the napplet bounds")
	}
	AnswerPrompt(p.ID, Answer{OK: true})
	select {
	case ok := <-answered:
		if !ok {
			t.Fatal("the launcher prompt's answer was lost")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the launcher prompt was never answered")
	}
}

// TestPromptsStayFIFOWhenRefused: a refused request never reorders,
// displaces or answers the prompts already pending, whoever owns them.
func TestPromptsStayFIFOWhenRefused(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	host = &promptTestHost{}
	withAskRoute(t, 0)
	ci, rec := openNapplet(t, "prompt-fifo")
	ready(t, ci, rec, 1)

	// a launcher prompt and another window's prompt between this window's
	launcher := newPrompt("Verdana", "launcher question", "", "", nil)
	enqueuePrompt(launcher)
	post(t, ci, map[string]any{"type": "test.ask", "id": "f1"})
	waitPromptsFor(t, ci.instance, 1)
	other := newPrompt("other", "other window's question", "", "", nil)
	other.Instance = "prompt-fifo-other"
	if !enqueueNappPrompt(other) {
		t.Fatal("the other window's prompt was refused")
	}
	post(t, ci, map[string]any{"type": "test.ask", "id": "f2"})
	post(t, ci, map[string]any{"type": "test.ask", "id": "f3"})
	mine := waitPromptsFor(t, ci.instance, 3)

	before := promptIDs()
	want := []int{launcher.ID, mine[0].ID, other.ID, mine[1].ID, mine[2].ID}
	if !slices.Equal(before, want) {
		t.Fatalf("queue %v, want FIFO %v", before, want)
	}
	active := CurrentPrompt()

	post(t, ci, map[string]any{"type": "test.ask", "id": "f4"})
	if got := waitID(t, rec, "test.ask.result", "f4"); got["error"] != napErrRateLimited {
		t.Fatalf("4th prompt: %v", got)
	}
	if after := promptIDs(); !slices.Equal(before, after) {
		t.Fatalf("queue %v after a refusal, was %v", after, before)
	}
	if CurrentPrompt() != active {
		t.Fatal("a refusal displaced the active prompt")
	}
	if got := rec.find("test.ask.result"); len(got) != 1 {
		t.Fatalf("a refusal answered pending requests: %v", got)
	}
}

// TestPromptBucketLimitsCreation: a window's prompt bucket refuses prompt
// creation past its burst even when the queue is empty again (frozen clock,
// burst 2): the 3rd prompt-creating request answers rate-limited.
func TestPromptBucketLimitsCreation(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	freezeNapNow(t)
	host = &promptTestHost{}
	withAskRoute(t, 0)
	ci, rec := openNapplet(t, "prompt-bucket")
	withLimits(t, ci, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitPrompt: {rate.Every(6 * time.Second), 2},
	}))
	ready(t, ci, rec, 1)

	for i := 1; i <= 2; i++ {
		id := "b" + strconv.Itoa(i)
		post(t, ci, map[string]any{"type": "test.ask", "id": id})
		p := waitPromptsFor(t, ci.instance, 1)[0]
		// a no for this prompt only: nothing is remembered
		AnswerPrompt(p.ID, Answer{OK: false, Scope: ScopeOnce})
		if got := waitID(t, rec, "test.ask.result", id); got["error"] != napErrDenied {
			t.Fatalf("%s: %v", id, got)
		}
	}
	post(t, ci, map[string]any{"type": "test.ask", "id": "b3"})
	if got := waitID(t, rec, "test.ask.result", "b3"); got["status"] != "denied" || got["error"] != napErrRateLimited {
		t.Fatalf("3rd prompt over the bucket: %v", got)
	}
	if n := len(promptsFor(ci.instance)); n != 0 {
		t.Fatalf("%d prompts pending after a bucket refusal", n)
	}
}

// ─── cancellation ────────────────────────────────────────────────

// TestPromptCancelledAtRequestDeadline: a prompt still open when the
// request's deadline passes is taken down as dismissed (DEC-1, P1): the
// napplet gets the route's denial, nothing is remembered, nothing opens.
func TestPromptCancelledAtRequestDeadline(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	h := &promptTestHost{}
	host = h
	withAskRoute(t, 50*time.Millisecond)
	ci, rec := openNapplet(t, "prompt-deadline")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "test.ask", "id": "d1"})
	got := waitID(t, rec, "test.ask.result", "d1")
	if got["status"] != "denied" || got["error"] != napErrDenied {
		t.Fatalf("expired prompt: %v", got)
	}
	if p := CurrentPrompt(); p != nil {
		t.Fatalf("the expired prompt is still up: %+v", p)
	}
	if rule, ok := lookupRule(RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}); ok {
		t.Fatalf("an expired prompt left a rule: %+v", rule)
	}
	if links := h.links(); len(links) != 0 {
		t.Fatalf("opened %v", links)
	}
}

// TestLateAllowDoesNotRunTheAction: an Allow that arrives after the request's
// deadline neither runs the action nor creates a rule, whether it comes
// after the prompt came down or races its cancellation.
func TestLateAllowDoesNotRunTheAction(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	h := &promptTestHost{}
	host = h
	withAskRoute(t, 300*time.Millisecond)
	ci, rec := openNapplet(t, "prompt-late")
	ready(t, ci, rec, 1)
	key := RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}
	t.Cleanup(func() { clearSessionRule(key) })

	post(t, ci, map[string]any{"type": "test.ask", "id": "late"})
	p := waitPromptsFor(t, ci.instance, 1)[0]
	if got := waitID(t, rec, "test.ask.result", "late"); got["error"] != napErrDenied {
		t.Fatalf("expired prompt: %v", got)
	}
	AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeOnce})
	AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeSession})
	time.Sleep(50 * time.Millisecond)
	if links := h.links(); len(links) != 0 {
		t.Fatalf("a late Allow opened %v", links)
	}
	if rule, ok := lookupRule(key); ok {
		t.Fatalf("a late Allow left a rule: %+v", rule)
	}
	if got := rec.find("test.ask.result"); len(got) != 1 {
		t.Fatalf("answers: %v", got)
	}

	t.Run("a click racing the cancellation", func(t *testing.T) {
		for range 50 {
			p := newPrompt("race", "race", "", "", nil)
			p.Instance = "prompt-late-race"
			if !enqueueNappPrompt(p) {
				t.Fatal("refused")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeOnce})
			if a, err := p.waitCtx(ctx); !errors.Is(err, errPromptDismissed) || a.OK {
				t.Fatalf("a click after the asker gave up returned %+v, %v", a, err)
			}
		}
	})
}

// TestPromptCancelledOnSessionEnd: a new session (nap.start) cancels the old
// session's pending prompt: it comes down, nothing is remembered, nothing
// opens, and the old request is never answered into the new document.
func TestPromptCancelledOnSessionEnd(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	h := &promptTestHost{}
	host = h
	ci, rec := openNapplet(t, "prompt-session")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "link.open", "id": "s1", "url": "https://example.com/s"})
	p := waitPromptsFor(t, ci.instance, 1)[0]
	ready(t, ci, rec, 2)
	waitPromptsFor(t, ci.instance, 0)

	AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeAlways})
	time.Sleep(50 * time.Millisecond)
	if links := h.links(); len(links) != 0 {
		t.Fatalf("opened %v", links)
	}
	if rule, ok := lookupRule(RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}); ok {
		t.Fatalf("a cancelled prompt left a rule: %+v", rule)
	}
	if got := rec.find("link.open.result"); len(got) != 0 {
		t.Fatalf("the old session's request was answered: %v", got)
	}
}

// TestBridgePromptCancelledOnWindowClose: a bridge napp's prompt belongs to
// its window: closing the window takes it down as dismissed.
func TestBridgePromptCancelledOnWindowClose(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	ci := &Instance{
		instance:   "bridge-close-" + randomID()[:6],
		napp:       Napp{ID: "napp~0123456789abcdef~bridge-close", D: "bridge-close", Name: "bridge"},
		subs:       map[int]context.CancelFunc{},
		actions:    map[string]int{},
		changed:    make(chan struct{}),
		dispatches: map[int]chan WireMsg{},
		gone:       make(chan struct{}),
	}
	registerInstance(ci)
	ci.attach(newRecTransport())
	t.Cleanup(func() { WindowClosed(ci.instance) })

	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		ctx, cancel := ci.windowPromptCtx()
		defer cancel()
		ok, err := askApproval(ctx, ci, PermSaveFile, "save a file to your disk", "", "")
		done <- result{ok, err}
	}()
	waitPromptsFor(t, ci.instance, 1)
	WindowClosed(ci.instance)

	select {
	case r := <-done:
		if r.ok || !errors.Is(r.err, errPromptDismissed) {
			t.Fatalf("closed window's prompt: %v, %v", r.ok, r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the prompt outlived its window")
	}
	waitPromptsFor(t, ci.instance, 0)
	if rule, ok := lookupRule(RuleKey{Napp: ci.napp.ID, Permission: PermSaveFile}); ok {
		t.Fatalf("a cancelled prompt left a rule: %+v", rule)
	}
}

// ─── session grants ──────────────────────────────────────────────

// TestSessionGrantRecordsOnlyExplicitAnswers: concurrent requests share one
// session question; a question dismissed by the deadline records nothing,
// so the next request asks again; an explicit allow is recorded and later
// requests do not prompt (02-RESEARCH Pattern 9).
func TestSessionGrantRecordsOnlyExplicitAnswers(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	sinks := recordSinks(t)
	png := []byte("\x89PNG\r\n\x1a\nnot really an image")
	prev := resourceClient
	resourceClient = &http.Client{Transport: napRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(png))), Request: r}, nil
	})}
	t.Cleanup(func() { resourceClient = prev })
	ci, rec := openNapplet(t, "grant-explicit")
	ready(t, ci, rec, 1)
	grant := func() (bool, bool) {
		ci.nap.mu.Lock()
		defer ci.nap.mu.Unlock()
		ok, decided := ci.nap.grants[PermFetch]
		return ok, decided
	}

	restore := withRouteDeadline(t, "resource.bytes", 300*time.Millisecond)
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "r1", "url": "https://8.8.8.8/1.png"})
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "r2", "url": "https://8.8.8.8/2.png"})
	waitPromptsFor(t, ci.instance, 1)
	time.Sleep(50 * time.Millisecond)
	if n := len(promptsFor(ci.instance)); n != 1 {
		t.Fatalf("two concurrent requests raised %d prompts", n)
	}
	for _, id := range []string{"r1", "r2"} {
		if got := waitID(t, rec, "resource.bytes.error", id); got["error"] != "blocked-by-policy" {
			t.Fatalf("%s after a dismissed question: %v", id, got)
		}
	}
	if _, decided := grant(); decided {
		t.Fatal("a dismissed question was recorded as the session's answer")
	}
	ci.nap.mu.Lock()
	inFlight := len(ci.nap.asking)
	ci.nap.mu.Unlock()
	if inFlight != 0 {
		t.Fatalf("%d questions still in flight", inFlight)
	}
	waitPromptsFor(t, ci.instance, 0)
	restore()

	// the next request asks again, and an explicit allow is kept
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "r3", "url": "https://8.8.8.8/3.png"})
	p := waitPromptsFor(t, ci.instance, 1)[0]
	AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeOnce})
	if got := waitID(t, rec, "resource.bytes.result", "r3"); got["mime"] != "image/png" {
		t.Fatalf("r3 after an allow: %v", got)
	}
	if ok, decided := grant(); !decided || !ok {
		t.Fatalf("the allow was not recorded: %v %v", ok, decided)
	}
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "r4", "url": "https://8.8.8.8/4.png"})
	if got := waitID(t, rec, "resource.bytes.result", "r4"); got["mime"] != "image/png" {
		t.Fatalf("r4: %v", got)
	}
	if n := len(promptsFor(ci.instance)); n != 0 {
		t.Fatalf("a decided session question prompted again")
	}
	if names := sinks.names(); !slices.Equal(names, []string{"fetch", "fetch"}) {
		t.Fatalf("sinks: %v", names)
	}

	t.Run("concurrent askers share the answer", func(t *testing.T) {
		ready(t, ci, rec, 2)
		if _, decided := grant(); decided {
			t.Fatal("a new session kept the old one's grant")
		}
		post(t, ci, map[string]any{"type": "resource.bytes", "id": "r5", "url": "https://8.8.8.8/5.png"})
		post(t, ci, map[string]any{"type": "resource.bytes", "id": "r6", "url": "https://8.8.8.8/6.png"})
		p := waitPromptsFor(t, ci.instance, 1)[0]
		time.Sleep(50 * time.Millisecond)
		if n := len(promptsFor(ci.instance)); n != 1 {
			t.Fatalf("%d prompts for one session question", n)
		}
		AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeOnce})
		for _, id := range []string{"r5", "r6"} {
			if got := waitID(t, rec, "resource.bytes.result", id); got["mime"] != "image/png" {
				t.Fatalf("%s: %v", id, got)
			}
		}
	})
}

// ─── intents ─────────────────────────────────────────────────────

// launchTestHost opens napp windows on a recording transport and counts the
// launches.
type launchTestHost struct {
	noopHost
	mu       sync.Mutex
	launched []string
}

func (h *launchTestHost) OpenWindow(spec WindowSpec) (Transport, error) {
	h.mu.Lock()
	h.launched = append(h.launched, spec.NappID)
	h.mu.Unlock()
	return newRecTransport(), nil
}

func (h *launchTestHost) launches() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.launched)
}

// installIntentHandler installs a napplet handling napplet:<archetype>/open
// for the test, with a document on disk so it can be launched. Windows it
// ends up with are closed at cleanup.
func installIntentHandler(t *testing.T, d, archetype string) Napp {
	t.Helper()
	topic := "napplet:" + archetype + "/open"
	n := Napp{
		ID: "napplet~0123456789abcdef~" + d, D: d, Name: d, Format: FormatNapplet, Kind: KindNapplet,
		Conventions: []NappletConvention{{ID: topic}},
		Actions:     []string{topic},
	}
	dir, err := nappBaseDir(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>handler</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[n.ID] = n
	stateMu.Unlock()
	t.Cleanup(func() {
		for _, ci := range runningForNapp(n.ID) {
			WindowClosed(ci.instance)
		}
		stateMu.Lock()
		delete(state.InstalledNapps, n.ID)
		stateMu.Unlock()
	})
	return n
}

// invokeResult posts an intent.invoke and returns its nested result.
func invokeResult(t *testing.T, ci *Instance, rec *recTransport, id string, request map[string]any) map[string]any {
	t.Helper()
	post(t, ci, map[string]any{"type": "intent.invoke", "id": id, "request": request})
	return waitID(t, rec, "intent.invoke.result", id)["result"].(map[string]any)
}

// TestIntentChooserBoundedAndCancellable: the "open with" chooser obeys the
// prompt bounds (rate-limited when the window already holds 3 prompts) and
// the request's lifetime (a session restart dismisses it, answering nothing
// and remembering nothing), while an explicit pick still routes and stores
// the archetype's default.
func TestIntentChooserBoundedAndCancellable(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	host = &launchTestHost{}
	installIntentHandler(t, "chooser-a", "chooser")
	b := installIntentHandler(t, "chooser-b", "chooser")
	defaultKey := intentDefaultKey("chooser")
	t.Cleanup(func() {
		clearSessionRule(defaultKey)
		storeRule(defaultKey, Rule{Decision: DecisionAsk})
	})
	caller, rec := openNapplet(t, "chooser-caller")
	ready(t, caller, rec, 1)

	t.Run("bounded", func(t *testing.T) {
		for i := range napMaxPendingPromptsPerWindow {
			p := newPrompt("caller", "held "+strconv.Itoa(i), "", "", nil)
			p.Instance = caller.instance
			if !enqueueNappPrompt(p) {
				t.Fatal("rig: prompt refused")
			}
		}
		before := promptIDs()
		res := invokeResult(t, caller, rec, "full", map[string]any{"archetype": "chooser", "handler": "choose"})
		if res["ok"] != false || res["handled"] != false || res["error"] != napErrRateLimited {
			t.Fatalf("chooser over the bound: %v", res)
		}
		if after := promptIDs(); !slices.Equal(before, after) {
			t.Fatalf("the refused chooser changed the queue: %v -> %v", before, after)
		}
		if l := host.(*launchTestHost).launches(); len(l) != 0 {
			t.Fatalf("launched %v", l)
		}
		cancelAllPrompts()
	})

	t.Run("dismissed by a session restart", func(t *testing.T) {
		post(t, caller, map[string]any{"type": "intent.invoke", "id": "restart", "request": map[string]any{"archetype": "chooser"}})
		waitPromptsFor(t, caller.instance, 1)
		ready(t, caller, rec, 2)
		waitPromptsFor(t, caller.instance, 0)
		time.Sleep(50 * time.Millisecond)
		for _, r := range rec.find("intent.invoke.result") {
			if r["id"] == "restart" {
				t.Fatalf("the old session's chooser was answered: %v", r)
			}
		}
		if rule, ok := lookupRule(defaultKey); ok {
			t.Fatalf("a dismissed chooser left a default: %+v", rule)
		}
		if l := host.(*launchTestHost).launches(); len(l) != 0 {
			t.Fatalf("launched %v", l)
		}
	})

	t.Run("an explicit pick", func(t *testing.T) {
		post(t, caller, map[string]any{"type": "intent.invoke", "id": "pick", "request": map[string]any{"archetype": "chooser"}})
		p := waitPromptsFor(t, caller.instance, 1)[0]
		pick := slices.IndexFunc(p.Options, func(o PromptOption) bool { return o.NappID == b.ID })
		if pick < 0 || len(p.Options) != 2 {
			t.Fatalf("chooser options: %+v", p.Options)
		}
		AnswerPrompt(p.ID, Answer{OK: true, Index: pick, Scope: ScopeOnce})
		res := waitID(t, rec, "intent.invoke.result", "pick")["result"].(map[string]any)
		if res["ok"] != true || res["handler"] != b.D {
			t.Fatalf("picked handler: %v", res)
		}
		if rule, ok := lookupRule(defaultKey); !ok || rule.Target != b.ID {
			t.Fatalf("the pick was not stored as the default: %+v %v", rule, ok)
		}
	})
}

// TestIntentColdLaunchLimited: intents that have to launch their handler
// draw on the window's cold-launch bucket (frozen clock, burst 1): the first
// launches, a second that needs another window answers rate-limited and
// launches nothing, and routing to the window already open costs nothing.
func TestIntentColdLaunchLimited(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	freezeNapNow(t)
	h := &launchTestHost{}
	host = h
	alpha := installIntentHandler(t, "cold-alpha", "alpha")
	beta := installIntentHandler(t, "cold-beta", "beta")
	caller, rec := openNapplet(t, "cold-caller")
	withLimits(t, caller, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitColdLaunch: {rate.Every(10 * time.Second), 1},
	}))
	ready(t, caller, rec, 1)

	res := invokeResult(t, caller, rec, "first", map[string]any{"archetype": "alpha", "handler": alpha.D})
	if res["ok"] != true || res["handler"] != alpha.D {
		t.Fatalf("first cold launch: %v", res)
	}
	res = invokeResult(t, caller, rec, "second", map[string]any{"archetype": "beta", "handler": beta.D})
	if res["ok"] != false || res["error"] != napErrRateLimited {
		t.Fatalf("second cold launch: %v", res)
	}
	if l := h.launches(); !slices.Equal(l, []string{alpha.ID}) {
		t.Fatalf("launched %v, want only %s", l, alpha.ID)
	}
	if open := runningForNapp(beta.ID); len(open) != 0 {
		t.Fatalf("a refused launch left a window: %v", open)
	}
	res = invokeResult(t, caller, rec, "warm", map[string]any{"archetype": "alpha", "handler": alpha.D})
	if res["ok"] != true || res["handler"] != alpha.D {
		t.Fatalf("routing to the open window: %v", res)
	}
	if l := h.launches(); len(l) != 1 {
		t.Fatalf("routing to an open window launched again: %v", l)
	}
}

// TestIntentSelfInvokingChainIsBounded: a napplet that handles its own
// convention and invokes itself with newWindow cannot fork without limit
// (CR-02). Every copy it launches draws on the cold-launch bucket of the
// window that started the chain, so with the default burst (and a frozen
// clock) the chain stops after burst launches however many copies try.
func TestIntentSelfInvokingChainIsBounded(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	freezeNapNow(t)
	h := &launchTestHost{}
	host = h
	self := installIntentHandler(t, "fork-self", "fork")
	ci, rec := openNapplet(t, "fork-self")
	if ci.napp.ID != self.ID {
		t.Fatalf("the caller is %s, want the handler %s itself", ci.napp.ID, self.ID)
	}
	ready(t, ci, rec, 1)
	fork := map[string]any{"archetype": "fork", "handler": self.D, "behavior": map[string]any{"newWindow": true}}

	burst := napLimitSpecs[limitColdLaunch].burst
	for i := range burst {
		res := invokeResult(t, ci, rec, "fork-"+strconv.Itoa(i), fork)
		if res["ok"] != true || res["handler"] != self.D {
			t.Fatalf("fork %d: %v", i, res)
		}
		// the copy starts its session and forks in turn
		child := lookupInstance(res["windowId"].(string))
		if child == nil || child == ci {
			t.Fatalf("fork %d opened no new window: %v", i, res)
		}
		child.sendMu.Lock()
		rec = child.transport.(*recTransport)
		child.sendMu.Unlock()
		ci = child
		ready(t, ci, rec, 1)
	}
	res := invokeResult(t, ci, rec, "fork-over", fork)
	if res["ok"] != false || res["error"] != napErrRateLimited {
		t.Fatalf("the copy past the chain's budget: %v", res)
	}
	if l := h.launches(); len(l) != burst {
		t.Fatalf("%d launches, want %d", len(l), burst)
	}
}

// overlayTransport records the prompt overlays the launcher sends a window:
// the id of each prompt shown, and how often the overlay was taken down.
type overlayTransport struct {
	mu    sync.Mutex
	shown []int
	hides int
}

func (o *overlayTransport) Send(m WireMsg) {
	if m.T != "prompt" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if m.Params == "" {
		o.hides++
		return
	}
	var p struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(m.Params), &p); err == nil {
		o.shown = append(o.shown, p.ID)
	}
}
func (o *overlayTransport) Focus() {}
func (o *overlayTransport) Close() {}

func (o *overlayTransport) state() ([]int, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.shown), o.hides
}

// TestSecondPromptForAWindowIsShown: with two prompts pending over one
// window, answering the first puts the second up over that window, instead
// of leaving the overlay on the answered prompt and the second unanswerable
// until its deadline (WR-01). Once nothing is pending the overlay comes down.
func TestSecondPromptForAWindowIsShown(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	ci, _ := openNapplet(t, "two-prompts")
	ov := &overlayTransport{}
	ci.attach(ov)

	first := newPrompt("", "first", "", "", nil)
	first.Instance = ci.instance
	second := newPrompt("", "second", "", "", nil)
	second.Instance = ci.instance
	if !enqueueNappPrompt(first) || !enqueueNappPrompt(second) {
		t.Fatal("prompts refused")
	}
	waitOverlay := func(what string, ok func(shown []int, hides int) bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			shown, hides := ov.state()
			if ok(shown, hides) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: shown %v, hides %d", what, shown, hides)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitOverlay("first prompt shown", func(shown []int, _ int) bool {
		return slices.Equal(shown, []int{first.ID})
	})

	HandleMessage(ci.instance, WireMsg{T: "promptAnswer", ID: first.ID, Params: `{"ok":true}`})
	waitOverlay("second prompt shown after the first was answered", func(shown []int, _ int) bool {
		return slices.Equal(shown, []int{first.ID, second.ID})
	})

	// the second is answerable from the window that now shows it
	HandleMessage(ci.instance, WireMsg{T: "promptAnswer", ID: second.ID, Params: `{"ok":false}`})
	if a := <-second.resp; a.OK {
		t.Fatalf("second answered %+v, want no", a)
	}
	waitOverlay("overlay taken down", func(_ []int, hides int) bool { return hides >= 1 })
}
