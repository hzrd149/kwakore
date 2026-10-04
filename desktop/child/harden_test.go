package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
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
	kind := firstString(mainFn, "VERDANA_WINDOW_KIND")
	for what, pos := range map[string]token.Pos{
		"checkWebviewLibrary()": check, "prepareEngine()": prepare,
		"webview.New": newView, `"VERDANA_WINDOW_KIND"`: kind,
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
	pos := token.NoPos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
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
