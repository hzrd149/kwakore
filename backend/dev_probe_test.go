package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// probeNappletDir is the committed smoke probe for the phase 1 human check
// (D-13): a single-file napplet a developer loads from the dev tab to see
// frame scope, config, notify, INC, intent delivery and resource working in a
// real webview.
const probeNappletDir = "testdata/probe-napplet"

// probeRun runs the launcher's activation script and then the probe's own
// inline script in one fresh node vm context standing in for the frame, with
// just enough DOM for the probe to write its log, and prints that log. The
// process exits right away: the shim's pending requests and the probe's 3 s
// controls timer would otherwise keep node alive.
const probeRun = `
const vm = require("node:vm")
let input = ""
process.stdin.setEncoding("utf8")
process.stdin.on("data", d => { input += d })
process.stdin.on("end", () => {
  const { activation, probe } = JSON.parse(input)
  const element = () => ({
    textContent: "", className: "", children: [],
    appendChild(c) { this.children.push(c); return c },
    addEventListener() {},
  })
  const log = element()
  const sandbox = { crypto: globalThis.crypto, setTimeout, clearTimeout, console, URL }
  const ctx = vm.createContext(sandbox)
  sandbox.window = ctx
  sandbox.parent = { postMessage() {} }
  sandbox.addEventListener = () => {}
  sandbox.document = {
    body: element(),
    createElement: element,
    getElementById: id => (id === "log" ? log : element()),
  }
  vm.runInContext(activation, ctx)
  vm.runInContext(probe, ctx)
  process.stdout.write(log.children.map(c => c.textContent).join(""))
  process.exit(0)
})
`

// probeScript is the probe's own inline script: the one <script> in its
// index.html.
func probeScript(t *testing.T, html string) string {
	t.Helper()
	open := strings.Index(html, "<script>")
	end := strings.Index(html, "</script>")
	if open < 0 || end < open || strings.Count(html, "<script") != 1 {
		t.Fatal("the probe must hold exactly one inline <script>")
	}
	return html[open+len("<script>") : end]
}

// The probe stays loadable as a dev folder: a napplet with the profile role
// (so it handles napplet:profile/open, the tray's "open profile" intent),
// valid UTF-8 the srcdoc builder accepts, and nothing fetched over the
// network, which the napplet CSP would block anyway.
func TestProbeNappletFolderLoads(t *testing.T) {
	napp, err := readDevFolder(probeNappletDir)
	if err != nil {
		t.Fatalf("readDevFolder: %v", err)
	}
	if !napp.IsNapplet() {
		t.Fatalf("probe is not a napplet (format %q)", napp.Format)
	}
	if !slices.Contains(napp.Actions, "napplet:profile/open") {
		t.Fatalf("probe actions %v lack napplet:profile/open", napp.Actions)
	}
	if napp.ID != "dev~verdana-probe" {
		t.Errorf("probe id %q, want dev~verdana-probe", napp.ID)
	}

	raw, err := os.ReadFile(filepath.Join(probeNappletDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(raw) {
		t.Fatal("index.html is not valid UTF-8")
	}
	html := string(raw)
	for _, ref := range []string{`src="http`, `href="http`, `src='http`, `href='http`} {
		if strings.Contains(html, ref) {
			t.Errorf("index.html references the network (%s)", ref)
		}
	}
	doc, err := buildSrcdoc(raw, napDomains)
	if err != nil {
		t.Fatalf("buildSrcdoc: %v", err)
	}

	// the probe's top-level checks pass in a frame-like context: the scope
	// and domain lines are what the human check reads first in a real webview
	node := needNode(t)
	in, err := json.Marshal(map[string]string{
		"activation": activationScript(t, doc),
		"probe":      probeScript(t, html),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", probeRun)
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	got := string(out)
	for _, want := range []string{
		"PASS NappletShimPrelude is undefined",
		"PASS domains (17): ",
		"probe loaded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("probe log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "FAIL") {
		t.Errorf("probe logged a failure before any envelope was answered:\n%s", got)
	}
}
