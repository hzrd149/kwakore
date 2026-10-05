package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// conformanceChecklist is the public audit checklist (SPEC-03). Its sections
// follow pinnedSnapshots; this test keeps the skeleton later phases fill in
// from regressing.
const conformanceChecklist = "../spec/CONFORMANCE.md"

// mdTable is one Markdown table: the "## " section it sits in, its header
// cells and its data rows (the separator row dropped).
type mdTable struct {
	section string
	header  []string
	rows    [][]string
}

// col is the index of the header cell named name, or -1.
func (t mdTable) col(name string) int {
	for i, h := range t.header {
		if h == name {
			return i
		}
	}
	return -1
}

// cell is row[i] or "" when the row is short or the column is missing.
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

var mdSeparatorRow = regexp.MustCompile(`^\|(\s*:?-+:?\s*\|)+$`)

// splitMarkdownRow splits "| a | b |" into trimmed cells. A pipe inside a
// backtick code span or escaped as \| does not end a cell.
func splitMarkdownRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case ch == '`':
			inCode = !inCode
			cur.WriteByte(ch)
		case ch == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// parseMarkdownTables finds every table: a "|" header line, then a separator
// line, then "|" data lines until the first line that is not a table row.
func parseMarkdownTables(doc string) []mdTable {
	lines := strings.Split(doc, "\n")
	var tables []mdTable
	section := ""
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
			continue
		}
		if !strings.HasPrefix(line, "|") || i+1 >= len(lines) || !mdSeparatorRow.MatchString(strings.TrimSpace(lines[i+1])) {
			continue
		}
		t := mdTable{section: section, header: splitMarkdownRow(line)}
		i += 2
		for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
			t.rows = append(t.rows, splitMarkdownRow(lines[i]))
		}
		i--
		tables = append(tables, t)
	}
	return tables
}

// curlyQuote is a verbatim spec passage: CONFORMANCE.md puts exactly those,
// and nothing else, in “curly quotes”.
var curlyQuote = regexp.MustCompile(`“([^”]+)”`)

// collapseSpace joins hard line wraps: every run of whitespace becomes one
// space, which is the only liberty a checklist quote takes with the snapshot.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// citedTest is a test name as the checklist cites it; testFuncDecl is its
// declaration in a _test.go file.
var (
	citedTest    = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]*`)
	testFuncDecl = regexp.MustCompile(`(?m)^func (Test[A-Z][A-Za-z0-9_]*)\(`)
)

func TestConformanceChecklistSkeleton(t *testing.T) {
	raw, err := os.ReadFile(conformanceChecklist)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	var headings []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") {
			headings = append(headings, strings.TrimRight(line, " \r"))
		}
	}
	headingAt := func(match func(string) bool) int {
		for i, h := range headings {
			if match(h) {
				return i
			}
		}
		return -1
	}
	for _, want := range []string{"## Runtime baseline", "## Conflicts", "## Dropped shim patches", "## Decisions"} {
		if headingAt(func(h string) bool { return strings.HasPrefix(h, want) }) < 0 {
			t.Errorf("missing section %q", want)
		}
	}

	// one section per pinned spec, headed with its full commit, in
	// SPEC-PINS order; the NAP-RESOURCE tolerance has no section of its own
	last, lastSpec := -1, ""
	pinSection := map[string]string{} // spec -> section title
	for _, s := range pinnedSnapshots {
		if s.Role != "pin" {
			continue
		}
		title := s.Spec + " @ " + s.Commit
		pinSection[s.Spec] = title
		at := headingAt(func(h string) bool { return h == "## "+title })
		if at < 0 {
			t.Errorf("missing section %q", "## "+title)
			continue
		}
		if at <= last {
			t.Errorf("section %s comes before %s, but SPEC-PINS order puts it after", s.Spec, lastSpec)
		}
		last, lastSpec = at, s.Spec
	}

	tables := parseMarkdownTables(doc)
	tableIn := func(section string) (mdTable, bool) {
		for _, tb := range tables {
			if tb.section == section || strings.HasPrefix(tb.section, section) {
				return tb, true
			}
		}
		return mdTable{}, false
	}

	// every pin section carries the D-16 table, even when it has no rows yet
	d16 := []string{"ID", "Requirement", "Level", "Status", "Reason", "Code"}
	for spec, title := range pinSection {
		tb, ok := tableIn(title)
		if !ok {
			t.Errorf("section %s has no table", spec)
			continue
		}
		if strings.Join(tb.header, "|") != strings.Join(d16, "|") {
			t.Errorf("section %s table header is %v, want %v", spec, tb.header, d16)
		}
	}

	// rowsByID indexes a table by its first cell, reporting duplicates: one
	// conflict or one dropped behavior per row, never two sharing an ID
	rowsByID := func(tb mdTable) map[string][]string {
		out := map[string][]string{}
		for _, row := range tb.rows {
			id := cell(row, 0)
			if _, dup := out[id]; dup {
				t.Errorf("%s: row %s appears twice", tb.section, id)
			}
			out[id] = row
		}
		return out
	}
	requireRows := func(section, prefix string, n int, cols ...string) map[string][]string {
		tb, ok := tableIn(section)
		if !ok {
			t.Errorf("section %s has no table", section)
			return nil
		}
		rows := rowsByID(tb)
		for _, c := range cols {
			if tb.col(c) < 0 {
				t.Errorf("%s: table has no %q column (header %v)", section, c, tb.header)
			}
		}
		for i := 1; i <= n; i++ {
			id := fmt.Sprintf("%s%d", prefix, i)
			row, ok := rows[id]
			if !ok {
				t.Errorf("%s: row %s is missing", section, id)
				continue
			}
			for _, c := range cols {
				if cell(row, tb.col(c)) == "" {
					t.Errorf("%s: row %s has an empty %q cell", section, id, c)
				}
			}
		}
		return rows
	}

	// A1-A17 keep their FEATURES.md numbers; A18-A23 were added in Phase 1
	conflicts := requireRows("Conflicts", "A", 23, "Specs", "Conflict", "Chosen reading", "Decided by", "Owner")
	if tb, ok := tableIn("Conflicts"); ok && conflicts != nil {
		// the four required readings quote both sides verbatim
		for _, id := range []string{"A1", "A2", "A3", "A18"} {
			if row, ok := conflicts[id]; ok {
				if n := len(curlyQuote.FindAllString(cell(row, tb.col("Conflict")), -1)); n < 2 {
					t.Errorf("Conflicts: %s quotes %d passages; it must quote both sides verbatim", id, n)
				}
			}
		}
	}

	// P1-P6 in D-12 order, P7 (config.schemaError) appended
	requireRows("Dropped shim patches", "P", 7, "Former patch behavior", "Upstream 0.30.0 behavior", "Impact", "Replacement / owner", "Status")

	// the 30 s timeout decision names where prompt cancellation lands
	if tb, ok := tableIn("Decisions"); !ok {
		t.Error("section Decisions has no table")
	} else {
		found := false
		for _, row := range tb.rows {
			if strings.Contains(strings.Join(row, " "), "DISP-04") {
				found = true
			}
		}
		if !found {
			t.Error("Decisions: no row names DISP-04 (prompt cancellation for timed-out requests)")
		}
	}

	// the spec rows Phases 1 and 4 own, each in its spec's section
	for spec, ids := range map[string][]string{
		"NIP-5D":      {"5D-1", "NIP-5D-presence", "5D-3", "NIP-5D-reload", "5D-8"},
		"WEB-NAPPLET": {"W-1"},
		"NAP-SHELL":   {"NAP-SHELL-1"},
		"NAP-INTENT":  {"NAP-INTENT-1"},
		"NAP-INC":     {"NAP-INC-sender"},
	} {
		tb, ok := tableIn(pinSection[spec])
		if !ok {
			continue // reported above
		}
		rows := rowsByID(tb)
		for _, id := range ids {
			if _, ok := rows[id]; !ok {
				t.Errorf("section %s: row %s is missing", spec, id)
			}
		}
	}

	// Phase 4 closed the reload and embedding rows (SBOX-01, SBOX-02)
	if tb, ok := tableIn(pinSection["NIP-5D"]); ok {
		rows := rowsByID(tb)
		for _, id := range []string{"5D-3", "NIP-5D-reload", "5D-8"} {
			if row, ok := rows[id]; ok && !strings.HasPrefix(cell(row, tb.col("Status")), "fixed (Phase 4)") {
				t.Errorf("section NIP-5D: row %s has status %q, want fixed (Phase 4)", id, cell(row, tb.col("Status")))
			}
		}
	}

	// Phase 5 closed the file-name collision (KEY-04) and the identity and
	// manifest-selection conflicts (KEY-01..04, KEY-02, REG-01)
	if tb, ok := tableIn("Runtime baseline"); ok {
		rows := rowsByID(tb)
		if row, ok := rows["CF-2"]; !ok {
			t.Error("Runtime baseline: row CF-2 is missing")
		} else if st := cell(row, tb.col("Status")); !strings.HasPrefix(st, "fixed (Phase 5)") {
			t.Errorf("Runtime baseline: row CF-2 has status %q, want fixed (Phase 5)", st)
		}
		// the CF-2 residue is gone from the rows that used to point at it
		for _, id := range []string{"CRIT-01"} {
			if row, ok := rows[id]; ok && strings.Contains(cell(row, tb.col("Reason")), "lossy") {
				t.Errorf("Runtime baseline: row %s still describes the CF-2 residue", id)
			}
		}
	}
	if tb, ok := tableIn(pinSection["WEB-NAPPLET"]); ok {
		if row, ok := rowsByID(tb)["W-1"]; ok && strings.Contains(cell(row, tb.col("Reason")), "still normalize") {
			t.Error("section WEB-NAPPLET: row W-1 still describes the CF-2 residue")
		}
	}
	if tb, ok := tableIn("Conflicts"); ok && conflicts != nil {
		for _, id := range []string{"A4", "A7", "A11"} {
			if row, ok := conflicts[id]; ok && !strings.HasPrefix(cell(row, tb.col("Owner")), "fixed (Phase 5)") {
				t.Errorf("Conflicts: %s has owner %q, want fixed (Phase 5)", id, cell(row, tb.col("Owner")))
			}
		}
	}

	// the marker-less self-replacement (a javascript: URL result, an
	// unclosed document.open) is a recorded, open residual, and the rows
	// that close the reload clause point at it instead of claiming it
	if tb, ok := tableIn(pinSection["NIP-5D"]); ok {
		rows := rowsByID(tb)
		const residual = "NIP-5D-reload-residual"
		if row, ok := rows[residual]; !ok {
			t.Errorf("section NIP-5D: row %s is missing", residual)
		} else {
			if st := cell(row, tb.col("Status")); st != "open" {
				t.Errorf("section NIP-5D: row %s has status %q, want open", residual, st)
			}
			// SEED-002 is its tracker: an open MUST with no owner is forgotten
			for _, want := range []string{"javascript:", "document.open()", "0 connections", "not a containment escape", "`SEED-002`"} {
				if !strings.Contains(cell(row, tb.col("Reason")), want) {
					t.Errorf("section NIP-5D: row %s Reason does not say %q", residual, want)
				}
			}
		}
		for _, id := range []string{"5D-3", "NIP-5D-reload", "5D-NG-webkitgtk"} {
			if row, ok := rows[id]; ok && !strings.Contains(strings.Join(row, " "), "`"+residual+"`") {
				t.Errorf("section NIP-5D: row %s does not point at %s", id, residual)
			}
		}
	}
	if tb, ok := tableIn("Decisions"); ok {
		if row, ok := rowsByID(tb)["DEC-5"]; ok && !strings.Contains(strings.Join(row, " "), "`NIP-5D-reload-residual`") {
			t.Error("Decisions: DEC-5 does not say which documents post no marker (NIP-5D-reload-residual)")
		}
	}

	// every engine's residual risk sits under NIP-5D Non-Guarantees
	// (SBOX-04): Level Non-Guarantee, Status N/A, and a Reason recording what
	// was measured on that engine and what was not; one of them quotes the
	// Non-Guarantees sentence itself
	if tb, ok := tableIn(pinSection["NIP-5D"]); ok {
		rows := rowsByID(tb)
		quoted := false
		for _, id := range []string{"5D-NG-webkitgtk", "5D-NG-webview2", "5D-NG-wkwebview", "5D-NG-android"} {
			row, ok := rows[id]
			if !ok {
				t.Errorf("section NIP-5D: row %s is missing", id)
				continue
			}
			if lv := cell(row, tb.col("Level")); lv != "Non-Guarantee" {
				t.Errorf("section NIP-5D: row %s has level %q, want Non-Guarantee", id, lv)
			}
			if st := cell(row, tb.col("Status")); st != "N/A" {
				t.Errorf("section NIP-5D: row %s has status %q, want N/A", id, st)
			}
			reason := cell(row, tb.col("Reason"))
			if reason == "" {
				t.Errorf("section NIP-5D: row %s has an empty Reason cell", id)
			}
			if strings.Contains(cell(row, tb.col("Requirement"))+reason, "“The protocol does NOT protect against") {
				quoted = true
			}
		}
		if !quoted {
			t.Error("section NIP-5D: no 5D-NG row quotes the Non-Guarantees sentence")
		}
	}

	// DEC-5 records the document-start marker (D-18) and DEC-6 the engine
	// hardening scope (D-19)
	requireRows("Decisions", "DEC-", 6, "Decision", "Reason", "Owner")

	// a fixed row is a claim: it cites the code and test behind it
	for _, tb := range tables {
		st, code := tb.col("Status"), tb.col("Code")
		if st < 0 || code < 0 {
			continue
		}
		for _, row := range tb.rows {
			if strings.HasPrefix(cell(row, st), "fixed") && cell(row, code) == "" {
				t.Errorf("%s: row %s is marked fixed but cites no code", tb.section, cell(row, 0))
			}
		}
	}

	// every test the checklist cites exists, so a claim cannot outlive the
	// test behind it
	declared := map[string]bool{}
	for _, root := range []string{".", "../desktop"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range testFuncDecl.FindAllStringSubmatch(string(src), -1) {
				declared[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range citedTest.FindAllString(doc, -1) {
		if !declared[name] {
			t.Errorf("the checklist cites %s, but no test file declares it", name)
		}
	}

	// every curly-quoted passage in a table is verbatim from a snapshot
	var corpus strings.Builder
	for _, s := range pinnedSnapshots {
		b, err := os.ReadFile(filepath.Join(pinnedSpecDir, s.File))
		if err != nil {
			t.Fatal(err)
		}
		_, body, ok := splitPinnedSnapshot(b)
		if !ok {
			t.Fatalf("%s: unreadable front matter", s.File)
		}
		corpus.WriteString(collapseSpace(string(body)))
		corpus.WriteString("\n")
	}
	specText := corpus.String()
	for _, tb := range tables {
		for _, row := range tb.rows {
			for _, c := range row {
				for _, m := range curlyQuote.FindAllStringSubmatch(c, -1) {
					if !strings.Contains(specText, collapseSpace(m[1])) {
						t.Errorf("%s: row %s quotes %q, which no pinned snapshot contains", tb.section, cell(row, 0), m[1])
					}
				}
			}
		}
	}
}
