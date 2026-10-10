package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"kwakore/backend/webview"
)

// needNode returns node's path, or skips the test when there is none. CI sets
// KWAKORE_REQUIRE_NODE=1, which turns a missing node into a failure: these
// tests guard the napplet sandbox and must never skip silently there.
func needNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("KWAKORE_REQUIRE_NODE") == "1" {
			t.Fatal("node is required (KWAKORE_REQUIRE_NODE=1) but not on PATH")
		}
		t.Skip("node not on PATH")
	}
	return node
}

// scopeProbe runs the activation script in a fresh node vm context standing in
// for the napplet frame's global scope (window is the global itself, parent
// records posts, addEventListener is a no-op), and reports what it left behind
// and what it posted to the host page. A second fresh context runs the bare
// prelude, as a control.
const scopeProbe = `
const vm = require("node:vm")
let input = ""
process.stdin.setEncoding("utf8")
process.stdin.on("data", d => { input += d })
process.stdin.on("end", () => {
  const { script, prelude } = JSON.parse(input)
  const frame = code => {
    const sandbox = { crypto: globalThis.crypto, setTimeout, clearTimeout, console }
    const ctx = vm.createContext(sandbox)
    sandbox.window = ctx
    const posts = []
    sandbox.parent = { postMessage(message, target) { posts.push({ message, target }) } }
    sandbox.addEventListener = () => {}
    const before = new Set(vm.runInContext("Object.getOwnPropertyNames(globalThis)", ctx))
    vm.runInContext(code, ctx)
    const after = vm.runInContext("Object.getOwnPropertyNames(globalThis)", ctx)
    return {
      added: after.filter(name => !before.has(name)).sort(),
      prelude: vm.runInContext("typeof NappletShimPrelude", ctx),
      domains: vm.runInContext("typeof napplet === 'object' && napplet ? Object.keys(napplet) : null", ctx),
      posts: JSON.parse(JSON.stringify(posts)),
    }
  }
  process.stdout.write(JSON.stringify({ wrapped: frame(script), control: frame(prelude) }))
})
`

type scopeReport struct {
	Added   []string    `json:"added"`
	Prelude string      `json:"prelude"`
	Domains []string    `json:"domains"`
	Posts   []scopePost `json:"posts"`
}

// scopePost is one parent.postMessage call the activation made.
type scopePost struct {
	Message map[string]any `json:"message"`
	Target  string         `json:"target"`
}

// activationScript is the launcher's inline script: the text between the
// first <script> after the CSP meta and the next </script>.
func activationScript(t *testing.T, doc string) string {
	t.Helper()
	meta := strings.Index(doc, `<meta http-equiv="Content-Security-Policy"`)
	if meta < 0 {
		t.Fatal("no CSP meta in the srcdoc")
	}
	open := strings.Index(doc[meta:], "<script>")
	if open < 0 {
		t.Fatal("no script after the CSP meta")
	}
	body := doc[meta+open+len("<script>"):]
	end := strings.Index(body, "</script>")
	if end < 0 {
		t.Fatal("activation script is not closed")
	}
	return body[:end]
}

// SHIM-04 (NIP-5D: the namespace contains only the domain objects the shell
// exposes): the prelude runs inside a function scope, so in the frame the only
// new global is window.napplet, holding exactly napDomains, and nothing is
// left that napplet code could call to install domains it was never granted.
func TestSrcdocLeavesOnlyWindowNapplet(t *testing.T) {
	node := needNode(t)

	doc, err := buildSrcdoc([]byte("<p>x</p>"), napDomains)
	if err != nil {
		t.Fatal(err)
	}
	script := activationScript(t, doc)
	if !strings.Contains(script, webview.ShimPrelude()) {
		t.Fatal("the activation script does not inline the pristine prelude verbatim")
	}

	in, err := json.Marshal(map[string]any{"script": script, "prelude": webview.ShimPrelude()})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", scopeProbe)
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var got struct {
		Wrapped scopeReport `json:"wrapped"`
		Control scopeReport `json:"control"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}

	if !slices.Equal(got.Wrapped.Added, []string{"napplet"}) {
		t.Errorf("new frame globals = %v, want [napplet]", got.Wrapped.Added)
	}
	if got.Wrapped.Prelude != "undefined" {
		t.Errorf("typeof NappletShimPrelude = %q in the frame, want undefined", got.Wrapped.Prelude)
	}
	want := append(slices.Clone(napDomains), "shell")
	slices.Sort(want)
	have := slices.Clone(got.Wrapped.Domains)
	slices.Sort(have)
	if !slices.Equal(have, want) {
		t.Errorf("window.napplet domains = %v, want %v", have, want)
	}

	// D-18: the document marker must precede any eager domain subscription.
	if len(got.Wrapped.Posts) != 3 {
		t.Errorf("the activation posted %d messages, want marker, policy check, then gamepad subscription: %+v", len(got.Wrapped.Posts), got.Wrapped.Posts)
	} else {
		post := got.Wrapped.Posts[0]
		if len(post.Message) != 1 || post.Message["type"] != webview.DocumentMarker || post.Target != "*" {
			t.Errorf("posted %+v, want {type: %q} to \"*\"", post, webview.DocumentMarker)
		}
		post = got.Wrapped.Posts[1]
		if post.Message["type"] != "__kwakore.gamepad.policy" || post.Message["denied"] != true || post.Target != "*" {
			t.Errorf("policy report = %+v, want denied native input", post)
		}
		post = got.Wrapped.Posts[2]
		if len(post.Message) != 1 || post.Message["type"] != "gamepad.subscribe" || post.Target != "*" {
			t.Errorf("second post = %+v, want gamepad.subscribe to *", post)
		}
	}
	// the bare prelude posts nothing: the marker is the launcher's, not the shim's
	if len(got.Control.Posts) != 0 {
		t.Errorf("the bare prelude posted %+v", got.Control.Posts)
	}

	// the control proves the probe has teeth: unwrapped, the prelude's
	// top-level var is a frame global napplet code could call
	if got.Control.Prelude == "undefined" || !slices.Contains(got.Control.Added, "NappletShimPrelude") {
		t.Errorf("control: the bare prelude left no NappletShimPrelude global (%+v); the probe cannot tell", got.Control)
	}
}
