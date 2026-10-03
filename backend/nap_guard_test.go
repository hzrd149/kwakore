package backend

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The structural half of D-02 and D-08: a nap_*.go file outside the gate
// layer (nap_sink.go, nap_route.go) may not name a prompt, a raw sink or
// start a bare goroutine. Handlers ask through c.approve, c.grant and
// c.hasGrant and act through the sinks on their call; goroutines start
// through c.async or safeGo (nap.go, which the nap_*.go glob does not match).

// napGuardBannedFuncs are package functions (and sink package vars) only the
// gate layer may name: as a call, a value or anything else. The HTTP clients
// are here too: a handler that called resourceClient.Do or
// napUploadClient.Do itself would reach the network around every sink.
var napGuardBannedFuncs = map[string]bool{
	"askApproval": true, "askActionHandler": true, "openExternalLink": true,
	"publishSigned": true, "napPublishSigned": true, "httpsResource": true,
	"napUploadToServer": true, "napUploadAuth": true,
	"resourceClient": true, "napUploadClient": true,
}

// napGuardAllowed are the few uses of a banned name outside the gate layer
// that the guard lets through, keyed "file func name", each with its reason.
var napGuardAllowed = map[string]string{
	"nap_identity.go zapProvider resourceClient": "the LNURL-pay lookup for the " +
		"user's own lightning address (lud16 from their kind 0), never a " +
		"napplet-chosen URL; consent for zap reads is MISC-03 (Phase 8)",
}

// napGuardBannedSelectors are methods only the gate layer may reach, matched
// by name whatever the receiver, so `k := userKeyer; k.SignEvent` and
// `f := c.sessionGrant` are caught too.
var napGuardBannedSelectors = map[string]bool{
	"sessionGrant": true, "SignEvent": true, "Encrypt": true, "Decrypt": true,
	"Nip04Encrypt": true, "Nip04Decrypt": true, "OpenLink": true,
	"SendNotification": true, "MediaPlay": true, "RequestNotificationPermission": true,
}

// napGuardViolations inspects one parsed file and reports every bare go
// statement, every use of a banned function's name other than its own
// declaration (a top-level func or var), and every banned selector, as
// "position: reason". A use napGuardAllowed lists for its file and enclosing
// function is let through.
func napGuardViolations(fset *token.FileSet, file *ast.File) []string {
	declared := map[*ast.Ident]bool{}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			declared[d.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						declared[name] = true
					}
				}
			}
		}
	}
	base := filepath.Base(fset.Position(file.Pos()).Filename)
	var out []string
	report := func(n ast.Node, reason string) {
		out = append(out, fset.Position(n.Pos()).String()+": "+reason)
	}
	for _, d := range file.Decls {
		fn := ""
		if fd, ok := d.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		ast.Inspect(d, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GoStmt:
				report(x, "bare go statement (use c.async or safeGo)")
			case *ast.SelectorExpr:
				if napGuardBannedSelectors[x.Sel.Name] {
					report(x, x.Sel.Name+" outside the gate layer")
				}
			case *ast.Ident:
				if napGuardBannedFuncs[x.Name] && !declared[x] {
					if _, ok := napGuardAllowed[base+" "+fn+" "+x.Name]; !ok {
						report(x, x.Name+" outside the gate layer")
					}
				}
			}
			return true
		})
	}
	return out
}

// napGuardFiles parses every non-test nap_*.go file, except the ones skip
// names.
func napGuardFiles(t *testing.T, skip map[string]bool) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	names, err := filepath.Glob("nap_*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") || skip[name] {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = f
	}
	return fset, files
}

// TestNapFilesReachSinksOnlyThroughGates: no handler file reaches a prompt,
// a sink or a goroutine except through the gate layer.
func TestNapFilesReachSinksOnlyThroughGates(t *testing.T) {
	fset, files := napGuardFiles(t, map[string]bool{"nap_sink.go": true, "nap_route.go": true})
	// a broken glob would make this a silent no-op
	if len(files) < 12 {
		t.Fatalf("the guard parsed only %d nap_*.go files", len(files))
	}
	for _, f := range files {
		for _, v := range napGuardViolations(fset, f) {
			t.Error(v)
		}
	}
}

// TestNapGuardReportsPlantedViolations: the inspector finds each kind of
// violation, so a passing guard means something.
func TestNapGuardReportsPlantedViolations(t *testing.T) {
	const planted = `package backend

func askApproval() {} // a declaration is not a use

func planted() {
	go f()
	askApproval(nil, "", "", "", "")
	k.SignEvent(ctx, e)
	f := askApproval
	_ = c.sessionGrant
	_ = f
	napPublishSigned(ctx, e, nil)
	resourceClient.Do(req)
	_ = napUploadClient
}

var resourceClient = newClient() // a declaration is not a use

// zapProvider is allowed its resourceClient only in nap_identity.go
func zapProvider() { resourceClient.Do(req) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "nap_planted.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := napGuardViolations(fset, f)
	want := []string{
		"nap_planted.go:6:2: bare go statement",
		"nap_planted.go:7:2: askApproval outside",
		"nap_planted.go:8:2: SignEvent outside",
		"nap_planted.go:9:7: askApproval outside",
		"nap_planted.go:10:6: sessionGrant outside",
		"nap_planted.go:12:2: napPublishSigned outside",
		"nap_planted.go:13:2: resourceClient outside",
		"nap_planted.go:14:6: napUploadClient outside",
		"nap_planted.go:20:22: resourceClient outside",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d violations, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("violation %d = %q, want %q...", i, got[i], want[i])
		}
	}

	// the same zapProvider in the file the allowance names passes
	allowed, err := parser.ParseFile(fset, "nap_identity.go", "package backend\n\nfunc zapProvider() { resourceClient.Do(req) }\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := napGuardViolations(fset, allowed); len(got) != 0 {
		t.Fatalf("an allowed use was reported: %v", got)
	}
}

// napGuardProse are the old prose codes D-07 replaced with invalid-request
// and internal-error.
var napGuardProse = map[string]bool{"internal error": true, "invalid request": true}

// TestNapReplyErrorsUseTheVocabulary: a napplet reply's error is a code, never
// a Go error's text and never the old prose (D-07). It looks at every
// "error": value in a composite literal of a non-test nap_*.go file, and at
// the old prose anywhere in them.
func TestNapReplyErrorsUseTheVocabulary(t *testing.T) {
	fset, files := napGuardFiles(t, nil)
	if len(files) < 12 {
		t.Fatalf("the guard parsed only %d nap_*.go files", len(files))
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.KeyValueExpr:
				key, ok := x.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING || key.Value != `"error"` {
					return true
				}
				if call, ok := x.Value.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" {
						t.Errorf("%s: a reply's error carries Go error text", fset.Position(x.Pos()))
					}
				}
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				if s, err := strconv.Unquote(x.Value); err == nil && napGuardProse[s] {
					t.Errorf("%s: %q is not in the vocabulary (invalid-request, internal-error)", fset.Position(x.Pos()), s)
				}
			}
			return true
		})
	}
}
