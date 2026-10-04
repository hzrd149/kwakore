package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"verdana/backend/webview"
)

// adversarialNappletDir is the Phase 4 sandbox fixture (D-13, D-15): a
// single-file napplet that tries every escape the phase research measured
// (self-navigation, document.open, reloads, a reload loop, forged binding
// calls, global probing, network channels) and shows each result on screen.
// It is never embedded; a developer loads the folder from the dev tab, and
// desktop/child/smoke_test.go runs it in the real child under WebKitGTK.
const adversarialNappletDir = "testdata/adversarial-napplet"

// adversarialRun runs the launcher's activation script and then the
// fixture's inline script in one fresh node vm context standing in for the
// frame. parent.postMessage records every post, window.name is preset from
// the input, and the document stub has just enough DOM for the fixture's
// log, its buttons and the elements its steps insert. It prints the posts
// and the log text, then exits at once: the fixture's storage requests are
// never answered here, and its timers would keep node alive.
const adversarialRun = `
const vm = require("node:vm")
let input = ""
process.stdin.setEncoding("utf8")
process.stdin.on("data", d => { input += d })
process.stdin.on("end", () => {
  const { activation, fixture, name } = JSON.parse(input)
  const element = () => ({
    textContent: "", className: "", children: [], style: {},
    appendChild(c) { this.children.push(c); return c },
    setAttribute() {},
    addEventListener() {},
  })
  const log = element()
  const elements = { log }
  const posts = []
  const sandbox = { crypto: globalThis.crypto, setTimeout, clearTimeout, console, URL }
  const ctx = vm.createContext(sandbox)
  sandbox.window = ctx
  sandbox.name = name
  sandbox.parent = { postMessage(message, target) { posts.push({ message, target }) } }
  sandbox.addEventListener = () => {}
  sandbox.document = {
    body: element(),
    head: element(),
    createElement: element,
    addEventListener() {},
    getElementById: id => elements[id] || (elements[id] = element()),
  }
  vm.runInContext(activation, ctx)
  vm.runInContext(fixture, ctx)
  process.stdout.write(JSON.stringify({
    posts: JSON.parse(JSON.stringify(posts)),
    log: log.children.map(c => c.textContent).join(""),
  }))
  process.exit(0)
})
`

type adversarialReport struct {
	Posts []scopePost `json:"posts"`
	Log   string      `json:"log"`
}

// runAdversarial runs the fixture once in the vm with window.name preset.
func runAdversarial(t *testing.T, node, activation, fixture, name string) adversarialReport {
	t.Helper()
	in, err := json.Marshal(map[string]string{"activation": activation, "fixture": fixture, "name": name})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", adversarialRun)
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var rep adversarialReport
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	return rep
}

// The fixture stays loadable as a dev folder (valid UTF-8, one inline
// script, nothing fetched by markup) and its step machine starts the way the
// WebKit smoke relies on: after the launcher's document-start marker, its
// first envelope reads its progress (adv.step) from instance storage, since
// window.name does not survive a rebuild. The document a load-delayed reload
// put in place does one thing only: it tries to get adv.leak to the launcher
// before its load, which the marker must stop.
func TestAdversarialNappletFolderLoads(t *testing.T) {
	napp, err := readDevFolder(adversarialNappletDir)
	if err != nil {
		t.Fatalf("readDevFolder: %v", err)
	}
	if !napp.IsNapplet() {
		t.Fatalf("fixture is not a napplet (format %q)", napp.Format)
	}
	if napp.ID != "dev~verdana-adversarial" {
		t.Errorf("fixture id %q, want dev~verdana-adversarial", napp.ID)
	}

	raw, err := os.ReadFile(filepath.Join(adversarialNappletDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(raw) {
		t.Fatal("index.html is not valid UTF-8")
	}
	html := string(raw)
	for _, ref := range []string{`src="http`, `href="http`, `src='http`, `href='http`} {
		if strings.Contains(html, ref) {
			t.Errorf("index.html references the network in markup (%s)", ref)
		}
	}
	doc, err := buildSrcdoc(raw, napDomains)
	if err != nil {
		t.Fatalf("buildSrcdoc: %v", err)
	}

	node := needNode(t)
	activation := activationScript(t, doc)
	fixture := probeScript(t, html) // exactly one inline <script>

	t.Run("fresh document", func(t *testing.T) {
		rep := runAdversarial(t, node, activation, fixture, "")
		if len(rep.Posts) < 2 {
			t.Fatalf("posts = %v, want the marker and then the fixture's storage reads", rep.Posts)
		}
		if typ, _ := rep.Posts[0].Message["type"].(string); typ != webview.DocumentMarker {
			t.Errorf("first post %v, want the launcher's document-start marker", rep.Posts[0].Message)
		}
		first := rep.Posts[1].Message
		if first["type"] != "storage.get" || first["scope"] != "instance" || first["key"] != "adv.step" {
			t.Errorf("the fixture's first envelope is %v, want an instance storage.get for adv.step", first)
		}
		for _, p := range rep.Posts[1:] {
			if p.Message["type"] != "storage.get" {
				t.Errorf("before its storage answers the fixture posted %v", p.Message)
			}
		}
		if !strings.Contains(rep.Log, "adversarial napplet loaded") {
			t.Errorf("no loaded line in the log:\n%s", rep.Log)
		}
		if strings.Contains(rep.Log, "FAIL") {
			t.Errorf("the fixture logged a failure before any envelope was answered:\n%s", rep.Log)
		}
	})

	t.Run("load-delayed reloaded document", func(t *testing.T) {
		rep := runAdversarial(t, node, activation, fixture, "adv-reload-delayed")
		if len(rep.Posts) != 2 {
			t.Fatalf("posts = %v, want exactly the marker and one storage.set", rep.Posts)
		}
		if typ, _ := rep.Posts[0].Message["type"].(string); typ != webview.DocumentMarker {
			t.Errorf("first post %v, want the document-start marker", rep.Posts[0].Message)
		}
		leak := rep.Posts[1].Message
		if leak["type"] != "storage.set" || leak["key"] != "adv.leak" || leak["scope"] != "instance" {
			t.Errorf("the replaced document posted %v, want an instance storage.set for adv.leak", leak)
		}
		if rep.Log != "" {
			t.Errorf("the replaced document wrote to the log:\n%s", rep.Log)
		}
	})
}
