package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
)

// Prompts are the launcher's only synchronous conversation with the user: a
// napp asks for something sensitive (signing, encrypting, saving a file,
// copying to the clipboard, publishing) or fires an action several napps can
// handle, and the rpc blocks here until the GUI answers.
//
// Only one prompt shows at a time; the rest queue behind it. Nothing blocks
// forever: an unanswered prompt is dismissed after promptTimeout.
//
// A prompt a napp or napplet window raised belongs to whoever asked (D-15,
// DEC-1): a napplet's request (its session, bounded by the route's prompt
// deadline) or a napp's window. When that ends, the prompt is cancelled as
// dismissed: it comes down, nothing is remembered for it, and the action it
// asked about never runs, even if the user clicks Allow a moment later. A
// window also has at most napMaxPendingPromptsPerWindow prompts pending, and
// all windows together napMaxPendingPromptsGlobal; past that a request is
// refused at once (errPromptLimited) instead of joining the queue. Prompts
// the launcher raises itself (install confirmations) are exempt, so a
// flooding napplet can never keep the user from them.
//
// Locking: promptMu is a leaf. Nothing here holds it while calling out
// (remember, host.PromptsChanged, syncPromptOverlays run after unlocking),
// and no prompt is created or awaited under a NAP session's dispatchMu or mu.
//
// A sensitive question is only asked when no rule has an answer for it (see
// window_permissions.go), and the answer can come back with a scope: for this prompt
// only, for this session, or always. Anything wider than that gets filed away
// by window_permissions.go and settles the next question of the same kind without a
// prompt.

const promptTimeout = 2 * time.Minute

var (
	// errPromptLimited is a prompt refused before it was shown: the window's
	// or the launcher's prompt queue is full, or the napplet's prompt bucket
	// is empty (D-15). Napplets get rate-limited for it.
	errPromptLimited = errors.New("rate-limited")
	// errPromptDismissed is a prompt that ended without an answer that still
	// counts: the asker's context ended (request deadline, session teardown,
	// closed window) or promptTimeout passed.
	errPromptDismissed = errors.New("dismissed")
	// errActionCancelled is a handler chooser the user said no to (or
	// answered with no valid option). The text keeps "cancelled" for the
	// callers that read it.
	errActionCancelled = errors.New("action handler selection cancelled")
)

// Scope is how long the user's answer to a prompt holds.
type Scope string

const (
	// ScopeOnce is this prompt only: nothing is remembered.
	ScopeOnce Scope = "once"

	// ScopeSession holds until the launcher quits, and is never written down.
	ScopeSession Scope = "session"

	// ScopeAlways holds across restarts, until the user takes it back.
	ScopeAlways Scope = "always"
)

// Answer is what the user chose on a prompt: yes or no, which option of a
// picker (ignored otherwise), and how long that answer holds.
type Answer struct {
	OK    bool  `json:"ok"`
	Index int   `json:"index"`
	Scope Scope `json:"scope"`
}

// PromptOption is one choice in a picker prompt.
type PromptOption struct {
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Dev    bool   `json:"dev,omitempty"`

	// For the action picker: which napp (and, when it's already open, which
	// instance) this option routes to.
	NappID   string `json:"nappId"`
	Instance string `json:"instance"`
	Number   int    `json:"number,omitempty"`

	// Suggested says the user has been choosing this napp for this action
	// before, and Uses is how many times, so a UI can put its habitual
	// handlers at the top in a color of their own. Set by the picker (see
	// launcher_usage.go); a UI never has to work it out.
	Suggested bool `json:"suggested,omitempty"`
	Uses      int  `json:"uses,omitempty"`
}

// Prompt is a question the user has to answer before a napp can continue.
type Prompt struct {
	// ID identifies the prompt when answering it.
	ID int `json:"id"`

	// Title is the one-line question, Detail the explanation under it and
	// Code an optional monospace block previewing the payload at stake.
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Code   string `json:"code"`

	// Options is empty for a plain approve/deny prompt.
	Options []PromptOption `json:"options"`

	// Remember says the answer can stick, so a GUI offers the scopes
	// alongside the yes and the no. False for a picker, whose answer is
	// only ever about this one dispatch.
	Remember bool `json:"remember"`

	// AcceptLabel and RejectLabel customize the two buttons on a plain prompt.
	// Permission prompts leave them empty and use Allow/Deny.
	AcceptLabel string `json:"acceptLabel,omitempty"`
	RejectLabel string `json:"rejectLabel,omitempty"`
	// CloseOnReject asks a launcher-hosted prompt window to close after the
	// negative answer instead of revealing the launcher's normal screen.
	CloseOnReject bool `json:"closeOnReject,omitempty"`

	// Napp is the napp that asked, for a GUI that wants to show it.
	Napp string `json:"napp"`

	// Instance is the window the prompt belongs over, when it was fired by
	// a running napp: that screen covers itself with the prompt until it is
	// answered. Empty for launcher-generated prompts (the GUI shows those
	// wherever it likes).
	Instance string `json:"instance"`

	// key is what an answer wider than this prompt is remembered under.
	// Zero for a picker.
	key  RuleKey
	resp chan Answer
	// dismissed is closed by cancelPrompt, so a waiter learns its prompt was
	// taken down by someone else
	dismissed chan struct{}
	done      bool
}

var (
	promptMu     sync.Mutex
	promptActive *Prompt
	promptQueue  []*Prompt
	promptSerial atomic.Int64
)

// CurrentPrompt is the prompt the user should be answering, or nil.
func CurrentPrompt() *Prompt {
	promptMu.Lock()
	defer promptMu.Unlock()
	return promptActive
}

// PendingPrompts is how many are waiting behind the current one.
func PendingPrompts() int {
	promptMu.Lock()
	defer promptMu.Unlock()
	return len(promptQueue)
}

// enqueuePrompt shows the prompt now, or queues it behind the active one,
// whatever is pending. Only the launcher's own prompts (Instance "": install
// confirmations, the trial's "did you like it") use it: they are exempt from
// the napplet bounds, so a window flooding the queue cannot keep the user from
// answering the launcher. Windows go through enqueueNappPrompt.
func enqueuePrompt(p *Prompt) {
	promptMu.Lock()
	placePromptLocked(p)
	promptMu.Unlock()
	promptsChanged()
}

// enqueueNappPrompt queues a prompt a window raised, unless that window
// already has napMaxPendingPromptsPerWindow prompts pending (shown or
// queued) or napMaxPendingPromptsGlobal window prompts are pending in all.
// A refused prompt was never shown and changes nothing: the queue keeps its
// order and every other prompt stays where it was.
func enqueueNappPrompt(p *Prompt) bool {
	if p.Instance == "" {
		enqueuePrompt(p)
		return true
	}
	promptMu.Lock()
	mine, all := 0, 0
	count := func(q *Prompt) {
		if q == nil || q.Instance == "" {
			return
		}
		all++
		if q.Instance == p.Instance {
			mine++
		}
	}
	count(promptActive)
	for _, q := range promptQueue {
		count(q)
	}
	if mine >= napMaxPendingPromptsPerWindow || all >= napMaxPendingPromptsGlobal {
		promptMu.Unlock()
		return false
	}
	placePromptLocked(p)
	promptMu.Unlock()
	promptsChanged()
	return true
}

// placePromptLocked shows p, or queues it last. The caller holds promptMu.
func placePromptLocked(p *Prompt) {
	if promptActive == nil {
		promptActive = p
	} else {
		promptQueue = append(promptQueue, p)
	}
}

// removePromptLocked takes p off the screen or out of the queue, promoting
// the next queued prompt when p was showing. The caller holds promptMu.
func removePromptLocked(p *Prompt) {
	if promptActive == p {
		if len(promptQueue) > 0 {
			promptActive = promptQueue[0]
			promptQueue = promptQueue[1:]
		} else {
			promptActive = nil
		}
		return
	}
	for i, q := range promptQueue {
		if q == p {
			promptQueue = append(promptQueue[:i], promptQueue[i+1:]...)
			return
		}
	}
}

// promptsChanged tells the GUIs and the covered windows. Never under promptMu.
func promptsChanged() {
	if host != nil {
		host.PromptsChanged()
	}
	syncPromptOverlays()
}

// AnswerPrompt is what a GUI calls when the user clicks: it remembers the
// answer when the user asked for it to stick, hands it to the waiting rpc and
// promotes the next queued prompt.
func AnswerPrompt(id int, ans Answer) {
	promptMu.Lock()
	var p *Prompt
	if promptActive != nil && promptActive.ID == id {
		p = promptActive
	} else {
		for _, q := range promptQueue {
			if q.ID == id {
				p = q
				break
			}
		}
	}
	if p == nil || p.done {
		promptMu.Unlock()
		return
	}
	p.done = true
	removePromptLocked(p)
	promptMu.Unlock()

	// before the rpc goes on, so the rule is already in place by the time
	// the next question of the same kind comes round
	if p.key.valid() {
		remember(p.key, Rule{Decision: decisionOf(ans.OK)}, ans.Scope)
	}

	select {
	case p.resp <- ans:
	default:
	}
	promptsChanged()
}

// cancelPrompt takes a prompt down without an answer: whoever asked gave up
// (or promptTimeout passed). It never remembers anything, and a click on it
// that comes later finds nothing to answer. Its waiter, if another goroutine
// cancelled it, returns dismissed.
func cancelPrompt(p *Prompt) {
	promptMu.Lock()
	if p.done {
		promptMu.Unlock()
		return
	}
	p.done = true
	removePromptLocked(p)
	if p.dismissed != nil {
		close(p.dismissed)
	}
	promptMu.Unlock()
	promptsChanged()
}

// waitCtx waits for the user's answer for as long as ctx lives, and at most
// promptTimeout. A prompt that ends any other way is cancelled as dismissed
// (errPromptDismissed). A click that lost the race to the end of ctx is
// dismissed all the same: whatever was asked about must not happen after
// the asker gave up (DEC-1), though a rule the click asked to keep stays,
// since that was the user's word.
func (p *Prompt) waitCtx(ctx context.Context) (Answer, error) {
	t := time.NewTimer(promptTimeout)
	defer t.Stop()
	select {
	case a := <-p.resp:
		if ctx.Err() != nil {
			return Answer{}, errPromptDismissed
		}
		return a, nil
	case <-ctx.Done():
	case <-t.C:
	case <-p.dismissed:
	}
	cancelPrompt(p)
	return Answer{}, errPromptDismissed
}

// wait is waitCtx for the launcher's own prompts, which no request owns:
// unanswered after promptTimeout is a plain no, with nothing remembered.
func (p *Prompt) wait() Answer {
	a, _ := p.waitCtx(context.Background())
	return a
}

func newPrompt(napp, title, detail, code string, options []PromptOption) *Prompt {
	return &Prompt{
		ID:      int(promptSerial.Add(1)),
		Napp:    napp,
		Title:   title,
		Detail:  detail,
		Code:    code,
		Options: options,
		resp:    make(chan Answer, 1),

		dismissed: make(chan struct{}),
	}
}

// askApproval answers whether a napp may do something, for perm: a rule that
// already covers it decides right away, and only a question nothing has an
// answer to becomes a prompt. It is what makes signEvent, nip04/nip44,
// saveFile, copyText, napp.link and publish "sensitive".
//
// The prompt lives as long as ctx: a napplet passes its request's context
// (session, bounded by the route's deadline), a napp its window's
// (windowPromptCtx). When ctx ends first the prompt comes down and the answer
// is errPromptDismissed; a prompt the bounds refuse is errPromptLimited. Either
// error means no: nothing is remembered, nothing may happen. Never call it
// with a NAP session's dispatchMu or mu held.
func askApproval(ctx context.Context, ci *Instance, perm Permission, title, detail, code string) (bool, error) {
	name := "A napp"
	nappID := ""
	if ci != nil {
		nappID = ci.napp.ID
		if ci.napp.Label() != "" {
			name = ci.napp.Label()
		}
	}

	// a napp with no id asked the launcher itself, and the rules file those
	// under the empty napp, same as an installed configuration that is about
	// the launcher rather than about one napp.
	key := RuleKey{Napp: nappID, Permission: perm}
	if rule, ok := lookupRule(key); ok {
		log.Info().Str("napp", name).Str("ask", title).
			Str("rule", string(rule.Decision)).Msg("approval answered by the rules")
		return rule.Decision.granted(), nil
	}

	// the asker already gave up: nobody would see the answer
	if ctx.Err() != nil {
		return false, errPromptDismissed
	}
	// a napplet that keeps asking is backed off by its prompt bucket (D-14);
	// bridge napps have no NAP session and only meet the queue bounds
	if ci != nil && ci.nap != nil && !ci.nap.limits.allow(limitPrompt, 1) {
		napSampled().Info().Str("napp", name).Str("ask", title).Msg("prompt refused: over the window's prompt rate")
		return false, errPromptLimited
	}

	p := newPrompt(name, name+" wants to "+title, detail, code, nil)
	p.key = key
	p.Remember = true
	if ci != nil {
		p.Instance = ci.instance
	}
	if !enqueueNappPrompt(p) {
		napSampled().Info().Str("napp", name).Str("ask", title).Msg("prompt refused: too many prompts pending")
		return false, errPromptLimited
	}
	log.Info().Str("napp", name).Str("ask", title).Msg("asking the user for approval")
	answer, err := p.waitCtx(ctx)
	if err != nil {
		log.Info().Str("napp", name).Str("ask", title).Msg("approval prompt dismissed")
		return false, err
	}
	log.Info().Str("napp", name).Str("ask", title).Bool("granted", answer.OK).
		Str("scope", string(answer.Scope)).Msg("approval answered")
	return answer.OK, nil
}

// windowPromptCtx is the context a napp window's prompts live in: cancelled
// when the window closes (or by the returned cancel). Bridge napps have no NAP
// session, so their prompts belong to the window rather than to a request.
// A nil instance, or one without a gone channel (tests), gets a context only
// cancel ends.
func (ci *Instance) windowPromptCtx() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if ci == nil || ci.gone == nil {
		return ctx, cancel
	}
	go func() {
		select {
		case <-ci.gone:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// askActionHandler asks which napp should handle an action when more than one
// can. Open windows come first — routing into one keeps the user's state.
//
// The chooser lives as long as ctx and meets the same bounds as any other
// prompt a window raises: refused before it shows, it is errPromptLimited
// (charged against a napplet's prompt bucket too); taken down because ctx
// ended or promptTimeout passed, errPromptDismissed. A no, or an answer that
// names no option, is errActionCancelled.
func askActionHandler(ctx context.Context, caller *Instance, action string, payload json.RawMessage, candidates []Napp, open []*Instance) (PromptOption, error) {
	callerName := "launcher"
	callerID := ""
	if caller != nil {
		callerName = caller.napp.Label()
		callerID = caller.napp.ID
	}

	options := make([]PromptOption, 0, len(candidates)+len(open))
	for _, dev := range []bool{true, false} {
		for _, ci := range open {
			if strings.HasPrefix(ci.napp.ID, "dev~") != dev {
				continue
			}
			options = append(options, PromptOption{
				Label:    ci.napp.Label() + " - window #" + strconv.Itoa(ci.number),
				NappID:   ci.napp.ID,
				Instance: ci.instance,
				Number:   ci.number,
				Dev:      dev,
			})
		}
		for _, n := range candidates {
			if strings.HasPrefix(n.ID, "dev~") != dev {
				continue
			}
			options = append(options, PromptOption{
				Label:  n.Label(),
				Detail: n.ID,
				NappID: n.ID,
				Dev:    dev,
			})
		}
	}

	// the napps the user keeps sending this action to come first, so the
	// habitual answer is the one under their finger; everything else is left
	// in the order it arrived in
	sortHandlerOptions(options, callerID, action)

	code := ""
	if len(payload) != 0 && string(payload) != "null" {
		code = preview(string(payload), 500)
	}

	if ctx.Err() != nil {
		return PromptOption{}, errPromptDismissed
	}
	if caller != nil && caller.nap != nil && !caller.nap.limits.allow(limitPrompt, 1) {
		napSampled().Info().Str("action", action).Msg("handler chooser refused: over the window's prompt rate")
		return PromptOption{}, errPromptLimited
	}
	p := newPrompt(callerName, "Open “"+action+"” with…", "", code, options)
	if caller != nil {
		p.Instance = caller.instance
	}
	if !enqueueNappPrompt(p) {
		napSampled().Info().Str("action", action).Msg("handler chooser refused: too many prompts pending")
		return PromptOption{}, errPromptLimited
	}
	log.Info().Str("action", action).Int("options", len(options)).Msg("asking the user to pick a handler")
	answer, err := p.waitCtx(ctx)
	if err != nil {
		log.Info().Str("action", action).Msg("handler chooser dismissed")
		return PromptOption{}, err
	}
	if !answer.OK || answer.Index < 0 || answer.Index >= len(options) {
		return PromptOption{}, errActionCancelled
	}
	return options[answer.Index], nil
}

// ─── showing prompts over the window that asked ──────────────────
//
// A prompt fired by a running napp belongs over that napp's own screen: the
// covered window keeps it up until the answer comes back, and the launcher
// chrome only shows prompts it generated itself.

// promptOverlays tracks the instances currently showing a prompt overlay, so
// each screen gets exactly one and stale ones come down when the prompt is
// answered (or times out).
var promptOverlays = xsync.NewMapOf[string, bool]()

// syncPromptOverlays makes what each napp window shows match the prompt
// state: one overlay per window with a pending prompt, none elsewhere.
// Safe to call after any change to the prompt state.
func syncPromptOverlays() {
	promptMu.Lock()
	targets := make(map[string]*Prompt)
	if promptActive != nil && promptActive.Instance != "" {
		targets[promptActive.Instance] = promptActive
	}
	for _, q := range promptQueue {
		if q.Instance != "" {
			if _, ok := targets[q.Instance]; !ok {
				targets[q.Instance] = q
			}
		}
	}
	promptMu.Unlock()

	var hide []string
	var showList []*Prompt

	for inst := range promptOverlays.Range {
		if _, ok := targets[inst]; !ok {
			hide = append(hide, inst)
			promptOverlays.Delete(inst)
		}
	}
	for inst, p := range targets {
		if _, shown := promptOverlays.LoadOrStore(inst, true); !shown {
			showList = append(showList, p)
		}
	}

	for _, inst := range hide {
		if ci := lookupInstance(inst); ci != nil {
			ci.send(WireMsg{T: "prompt"})
		}
	}
	for _, p := range showList {
		raw, err := json.Marshal(p)
		if err != nil {
			continue
		}
		if ci := lookupInstance(p.Instance); ci != nil {
			ci.send(WireMsg{T: "prompt", Params: string(raw)})
		} else {
			// the window is already gone: never mark it as covered
			promptOverlays.Delete(p.Instance)
		}
	}
}

// handlePromptAnswer is what a shell sends up when its overlay was clicked.
func (ci *Instance) handlePromptAnswer(m WireMsg) {
	var a Answer
	_ = json.Unmarshal([]byte(m.Params), &a)
	AnswerPrompt(m.ID, a)
}

// preview trims an arbitrary payload into something a dialog can show.
func preview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
