package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"sync"
	"testing"
	"time"
)

// TestWebView2Args pins the WebView2 browser arguments. Every child of one
// build shares one browser process, and WebView2 refuses a window whose
// arguments differ from the running ones, so a change has to be deliberate.
func TestWebView2Args(t *testing.T) {
	const want = "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"
	if webview2BrowserArgs != want {
		t.Fatalf("webview2BrowserArgs = %q, want %q", webview2BrowserArgs, want)
	}
}

// TestEngineSetupOrder pins where the engine is set up: prepareEngine runs
// for every window kind before the first webview exists, and hardenEngine
// runs on the napplet and settings windows before their page loads.
func TestEngineSetupOrder(t *testing.T) {
	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
	for _, name := range []string{"main.go", "napplet.go", "settings.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	for _, name := range []string{"main", "runNapplet", "runSettings"} {
		if funcs[name] == nil {
			t.Fatalf("func %s not found", name)
		}
	}

	mainFn := funcs["main"]
	check := firstCall(mainFn, "", "checkWebviewLibrary")
	prepare := firstCall(mainFn, "", "prepareEngine")
	newView := firstCall(mainFn, "webview", "New")
	kind := firstCall(mainFn, "", "runSettings")
	for what, pos := range map[string]token.Pos{
		"checkWebviewLibrary()": check, "prepareEngine()": prepare,
		"webview.New": newView, "runSettings": kind,
	} {
		if !pos.IsValid() {
			t.Fatalf("main has no %s", what)
		}
	}
	if !(check < prepare) {
		t.Errorf("prepareEngine() must come after checkWebviewLibrary() in main")
	}
	if !(prepare < newView) {
		t.Errorf("prepareEngine() must come before webview.New in main: WebView2 reads its arguments when the first view is created")
	}
	if !(newView < kind) {
		t.Errorf("webview.New must come before the window-kind branch in main, so every kind gets the same engine setup")
	}
	if firstCall(mainFn, "", "hardenEngine").IsValid() {
		t.Errorf("main calls hardenEngine: napp windows keep engine defaults (DEC-6); only runNapplet and runSettings harden")
	}

	// WR-02: a napplet window fails closed, so runNapplet must leave when
	// hardenEngine reports an error, and (IN-06) tell the launcher why
	// first, so the user gets a reason and not just a vanished window
	if !exitsOnError(funcs["runNapplet"], "hardenEngine") {
		t.Errorf("runNapplet must call os.Exit when hardenEngine(w) returns an error")
	}
	if body := errBranch(funcs["runNapplet"], "hardenEngine"); body != nil {
		report := firstCallIn(body, "", "reportWindowFailed")
		exit := firstCallIn(body, "os", "Exit")
		if !report.IsValid() || !exit.IsValid() || !(report < exit) {
			t.Errorf("runNapplet must call reportWindowFailed before os.Exit when hardenEngine(w) returns an error")
		}
	}

	for _, name := range []string{"runNapplet", "runSettings"} {
		fn := funcs[name]
		harden := firstCall(fn, "", "hardenEngine")
		navigate := firstCall(fn, "w", "Navigate")
		if !harden.IsValid() || !navigate.IsValid() {
			t.Errorf("%s: hardenEngine(w) or w.Navigate missing", name)
			continue
		}
		if !(harden < navigate) {
			t.Errorf("%s: hardenEngine(w) must come before w.Navigate", name)
		}
	}
}

// firstCall is the position of the first call in fn to name, or to
// recv.name when recv is set; NoPos when there is none.
func firstCall(fn *ast.FuncDecl, recv, name string) token.Pos {
	return firstCallIn(fn.Body, recv, name)
}

// firstCallIn is firstCall over any node.
func firstCallIn(node ast.Node, recv, name string) token.Pos {
	pos := token.NoPos
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || pos.IsValid() {
			return !pos.IsValid()
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			if recv == "" && f.Name == name {
				pos = call.Pos()
			}
		case *ast.SelectorExpr:
			if x, ok := f.X.(*ast.Ident); ok && recv != "" && x.Name == recv && f.Sel.Name == name {
				pos = call.Pos()
			}
		}
		return !pos.IsValid()
	})
	return pos
}

// exitsOnError reports whether fn has `if err := name(...); err != nil {
// ... os.Exit(...) ... }`.
func exitsOnError(fn *ast.FuncDecl, name string) bool {
	body := errBranch(fn, name)
	return body != nil && firstCallIn(body, "os", "Exit").IsValid()
}

// errBranch is the body of the first `if err := name(...); ... { ... }` in
// fn, or nil when there is none.
func errBranch(fn *ast.FuncDecl, name string) *ast.BlockStmt {
	var body *ast.BlockStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || body != nil {
			return body == nil
		}
		assign, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != name {
			return true
		}
		body = ifs.Body
		return false
	})
	return body
}

// TestReportWindowFailed pins the line a napplet window writes before it
// exits on a failed hardening (IN-06): the launcher matches the exact type
// and code (backend TestWindowFailedRaisesNotice sends the same line).
func TestReportWindowFailed(t *testing.T) {
	var buf bytes.Buffer
	saved := outEnc
	outEnc = json.NewEncoder(&buf)
	t.Cleanup(func() { outEnc = saved })

	reportWindowFailed(windowFailedEngineHardening)
	const want = `{"t":"windowFailed","code":"engine-hardening"}` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("reportWindowFailed wrote %q, want %q", got, want)
	}
}

// firstString is the position of the first string literal in fn whose value
// is s; NoPos when there is none.
func firstString(fn *ast.FuncDecl, s string) token.Pos {
	pos := token.NoPos
	want := `"` + s + `"`
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == want && !pos.IsValid() {
			pos = lit.Pos()
		}
		return !pos.IsValid()
	})
	return pos
}

// TestTokenMissLogIsSampled: a forged binding call is refused every time,
// but logs at most once per interval, with the count it swallowed.
func TestTokenMissLogIsSampled(t *testing.T) {
	var m missLog
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	if log, suppressed := m.note(start); !log || suppressed != 0 {
		t.Fatalf("first miss: log=%v suppressed=%d, want true 0", log, suppressed)
	}
	for i := 1; i <= 100; i++ {
		at := start.Add(time.Duration(i) * 9 * time.Millisecond) // all within the first second
		if log, _ := m.note(at); log {
			t.Fatalf("miss %d at +%v was logged inside the interval", i, at.Sub(start))
		}
	}
	if log, _ := m.note(start.Add(missLogInterval - time.Millisecond)); log {
		t.Fatal("a miss just before the interval ended was logged")
	}
	// that one was swallowed too: 101 since the last log
	if log, suppressed := m.note(start.Add(missLogInterval)); !log || suppressed != 101 {
		t.Fatalf("miss after the interval: log=%v suppressed=%d, want true 101", log, suppressed)
	}
	// the count starts over from that log
	if log, _ := m.note(start.Add(missLogInterval + time.Second)); log {
		t.Fatal("a miss one second after the second log was logged")
	}
	if log, suppressed := m.note(start.Add(2 * missLogInterval)); !log || suppressed != 1 {
		t.Fatalf("third log: log=%v suppressed=%d, want true 1", log, suppressed)
	}
}

func TestTokenMissLogIsSampledConcurrent(t *testing.T) {
	var m missLog
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	logged, swallowed := 0, 0
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				log, _ := m.note(now)
				mu.Lock()
				if log {
					logged++
				} else {
					swallowed++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if logged != 1 || swallowed != 3199 {
		t.Fatalf("logged=%d swallowed=%d, want 1 and 3199", logged, swallowed)
	}
	if _, suppressed := m.note(now.Add(missLogInterval)); suppressed != 3199 {
		t.Fatalf("suppressed=%d after the interval, want 3199", suppressed)
	}
}
