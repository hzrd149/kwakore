//go:build linux

package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUISettingsAndCatalogResults(t *testing.T) {
	m := tuiModel{settings: map[string]json.RawMessage{}}
	m.consume(tuiResult{kind: "settings", data: []byte(`{"settings":{"relays":["wss://relay.example"],"blossom_servers":[],"discover_on_user_relays":true,"desktop_entries":false,"gnome_search":true,"sync_id":"laptops","theme":"dark"}}`)})
	if len(m.items) != 7 || m.items[0].address != "relays" || !strings.Contains(m.items[0].detail, "relay.example") {
		t.Fatalf("settings items: %+v", m.items)
	}
	if item := m.items[5]; item.address != "sync_id" || !strings.Contains(item.detail, "laptops") {
		t.Fatalf("sync_id item: %+v", item)
	}
	if last := m.items[6]; last.address != "theme" || !strings.Contains(last.detail, "dark") {
		t.Fatalf("theme item: %+v", last)
	}
	m.consume(tuiResult{kind: "installed", data: []byte(`{"items":[{"address":"35129:abc:notes","name":"Notes\u001b[2J","format":"napplet","available":true}],"next_offset":100}`)})
	if len(m.items) != 1 || m.items[0].label != "Notes[2J" || !m.more {
		t.Fatalf("catalog items: %+v", m.items)
	}
}

func TestTUIConfirmationAndSecretMask(t *testing.T) {
	m := tuiModel{tab: 1, items: []tuiItem{{label: "Notes", address: "35129:abc:notes"}}, height: 24, width: 80}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if m.prompt == nil || m.prompt.action != "uninstall" || !m.prompt.confirm {
		t.Fatalf("uninstall prompt: %+v", m.prompt)
	}
	m.prompt = &tuiPrompt{title: "Secret", action: "signer-nsec", value: "private-key", secret: true}
	view := m.View()
	if strings.Contains(view, "private-key") || !strings.Contains(view, "•••") {
		t.Fatalf("secret leaked or mask missing: %q", view)
	}
}

func TestTUIYesNoConfirmDefaultsToYes(t *testing.T) {
	key := func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
	installed := tuiModel{tab: 1, items: []tuiItem{{label: "Notes", address: "35129:abc:notes"}}, height: 24, width: 80}
	discover := tuiModel{tab: 2, items: []tuiItem{{label: "Notes", address: "35129:abc:notes", available: true}}, height: 24, width: 80}
	for _, tc := range []struct {
		name   string
		m      tuiModel
		open   tea.KeyMsg
		action string
	}{
		{"uninstall", installed, key('x'), "uninstall"},
		{"install", discover, key('i'), "install-selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, _ := tc.m.Update(tc.open)
			m := updated.(tuiModel)
			if m.prompt == nil || m.prompt.action != tc.action || !m.prompt.confirm || m.prompt.no {
				t.Fatalf("prompt is not a yes-default question: %+v", m.prompt)
			}
			// Yes is highlighted and the question names the napplet
			if view := m.View(); !strings.Contains(view, "Notes?") || !strings.Contains(view, "\x1b[7m Yes \x1b[0m") {
				t.Fatalf("yes not highlighted: %q", view)
			}
			// a single Enter accepts the default
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil || updated.(tuiModel).prompt != nil || !updated.(tuiModel).busy {
				t.Fatal("Enter did not accept the default Yes")
			}
			// y answers directly
			updated, cmd = m.Update(key('y'))
			if cmd == nil {
				t.Fatal("y did not confirm")
			}
			// n, Esc, and Enter on No all cancel without a request
			for _, cancel := range []tea.KeyMsg{key('n'), {Type: tea.KeyEsc}} {
				m.prompt = &tuiPrompt{action: tc.action, address: "35129:abc:notes", confirm: true}
				updated, cmd = m.Update(cancel)
				if cmd != nil || updated.(tuiModel).prompt != nil {
					t.Fatalf("%v did not cancel", cancel)
				}
			}
			m.prompt = &tuiPrompt{action: tc.action, address: "35129:abc:notes", confirm: true}
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
			m = updated.(tuiModel)
			if !m.prompt.no || !strings.Contains(m.View(), "\x1b[7m No \x1b[0m") {
				t.Fatal("arrow did not move the highlight to No")
			}
			updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil || updated.(tuiModel).prompt != nil {
				t.Fatal("Enter on No issued a request")
			}
		})
	}
}

func TestTUISanitizesDaemonText(t *testing.T) {
	if got := safeMultiline("first\n\x1b[31msecond\x07"); got != "first\n[31msecond" {
		t.Fatalf("unsafe text: %q", got)
	}
}

func TestTUIDiscoveryFiltersCachedItemsLocally(t *testing.T) {
	m := tuiModel{tab: 2, discovered: []tuiItem{{label: "Notes", address: "35129:abc:notes", detail: "napplet"}, {label: "Photo Gallery", address: "35129:def:gallery", detail: "napplet"}}}
	m.prompt = &tuiPrompt{title: "Filter", action: "search", value: "PHOTO"}
	if view := m.View(); !strings.Contains(view, "1 matches") || !strings.Contains(view, "Photo Gallery") || strings.Contains(view, "  Notes\n") {
		t.Fatalf("search preview: %q", view)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if cmd != nil || len(m.items) != 1 || m.items[0].label != "Photo Gallery" {
		t.Fatalf("local filter issued request or selected wrong items: %+v", m.items)
	}
	m.prompt = &tuiPrompt{title: "Filter", action: "search", value: ""}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if cmd != nil || len(m.items) != 2 {
		t.Fatalf("clearing filter failed: %+v", m.items)
	}
}

func TestTUIDiscoveryShowsAuthorDetailsAndRefreshProgress(t *testing.T) {
	const author = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	m := tuiModel{tab: 2, width: 100, height: 24}
	m.consume(tuiResult{kind: "discover", data: []byte(`{"items":[{"address":"35129:` + author + `:notes","name":"Notes","description":"A note app","author":"` + author + `","format":"napplet","available":true,"version":{"event_id":"event-id","created_at":42,"artifact_hash":"hash"}}],"fetched_at":"2026-10-09T00:00:00Z","complete":true}`)})
	if len(m.items) != 1 || !strings.Contains(m.items[0].detail, "aaaaaaaa…aaaaaaaa") {
		t.Fatalf("author missing from list: %+v", m.items)
	}
	m.detailView = true
	if view := m.View(); !strings.Contains(view, "Author key: "+author) || !strings.Contains(view, "A note app") || !strings.Contains(view, "Artifact hash: hash") {
		t.Fatalf("detail missing: %q", view)
	}
	m.detailView = false
	_ = m.beginDiscovery(true)
	if view := m.View(); !strings.Contains(view, "Refreshing napplets from relays") {
		t.Fatalf("refresh status missing: %q", view)
	}
}

func TestTUIStaleDiscoveryMessagesDoNotEndRefresh(t *testing.T) {
	m := tuiModel{tab: 2, discoverySeq: 2, busy: true, discoveryLoading: true}
	updated, _ := m.Update(tuiResult{kind: "discover", seq: 1, err: "old request failed"})
	m = updated.(tuiModel)
	if !m.busy || !m.discoveryLoading || m.notice != "" {
		t.Fatalf("stale result changed current refresh: %+v", m)
	}
	updated, _ = m.Update(tuiTick{seq: 1})
	m = updated.(tuiModel)
	if m.spinner != 0 {
		t.Fatalf("stale tick advanced spinner: %d", m.spinner)
	}
}

func TestTUIDiscoveryRefreshKeys(t *testing.T) {
	for _, key := range []rune{'r', 'R'} {
		t.Run(string(key), func(t *testing.T) {
			m := tuiModel{tab: 2}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
			m = updated.(tuiModel)
			if cmd == nil || !m.refreshing || !m.discoveryLoading || m.discoverySeq != 1 {
				t.Fatalf("key %c did not start a relay refresh: %+v", key, m)
			}
			updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
			if cmd != nil || updated.(tuiModel).discoverySeq != 1 {
				t.Fatal("busy refresh started another request")
			}
		})
	}
}

func TestTUILaunchShortcuts(t *testing.T) {
	m := tuiModel{tab: 1, items: []tuiItem{{label: "Notes", address: "35129:abc:notes", format: "napplet"}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if cmd == nil || !m.busy || !strings.Contains(m.notice, "Opening Notes") {
		t.Fatalf("installed Enter did not launch: %+v", m)
	}
	m = tuiModel{tab: 1, items: []tuiItem{{label: "Legacy", address: "35130:abc:legacy"}}}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(tuiModel)
	if cmd != nil || !strings.Contains(m.notice, "Napps cannot launch") {
		t.Fatalf("legacy napp launch was not explained: %+v", m)
	}
	// Discover launches through try, which needs no install
	var argv []string
	tuiCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		argv = args
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { tuiCommand = exec.CommandContext })
	m = tuiModel{tab: 2, items: []tuiItem{{label: "Notes", address: "35129:abc:notes", format: "napplet"}}}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(tuiModel)
	if cmd == nil || !m.busy {
		t.Fatalf("discovery launch did not invoke service: %+v", m)
	}
	cmd()
	if len(argv) < 2 || argv[len(argv)-2] != "try" || argv[len(argv)-1] != "35129:abc:notes" {
		t.Fatalf("discovery launch ran %q, want try ADDRESS", argv)
	}
}

func TestTUISyncIDPrompt(t *testing.T) {
	var argv []string
	tuiCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		argv = args
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { tuiCommand = exec.CommandContext })
	open := func() tuiModel {
		m := tuiModel{tab: 3, settings: map[string]json.RawMessage{}, height: 24, width: 80}
		m.consume(tuiResult{kind: "settings", data: []byte(`{"settings":{"relays":[],"blossom_servers":[],"discover_on_user_relays":true,"desktop_entries":true,"gnome_search":true,"sync_id":"laptops","theme":"system"}}`)})
		m.cursor = 5
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(tuiModel)
		if m.prompt == nil || m.prompt.action != "setting-string" || m.prompt.address != "sync_id" || m.prompt.value != "laptops" {
			t.Fatalf("sync_id prompt: %+v", m.prompt)
		}
		return m
	}
	for _, tc := range []struct {
		value string
		want  []string
	}{
		{"", []string{"settings", "clear", "sync_id"}},
		{"desk top", []string{"settings", "set", "sync_id", `"desk top"`}},
		{"desk", []string{"settings", "set", "sync_id", `"desk"`}},
	} {
		m := open()
		m.prompt.value = tc.value
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%q: no request", tc.value)
		}
		argv = nil
		cmd()
		if len(argv) < len(tc.want) || !slices.Equal(argv[len(argv)-len(tc.want):], tc.want) {
			t.Fatalf("%q ran %q, want %q", tc.value, argv, tc.want)
		}
	}
}

func TestTUIThemePrompt(t *testing.T) {
	var argv []string
	tuiCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		argv = args
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { tuiCommand = exec.CommandContext })
	open := func() tuiModel {
		m := tuiModel{tab: 3, settings: map[string]json.RawMessage{}, height: 24, width: 80}
		m.consume(tuiResult{kind: "settings", data: []byte(`{"settings":{"relays":[],"blossom_servers":[],"discover_on_user_relays":true,"desktop_entries":true,"gnome_search":true,"sync_id":"","theme":"light"}}`)})
		m.cursor = len(m.items) - 1
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(tuiModel)
		if m.prompt == nil || m.prompt.action != "setting-choice" || m.prompt.address != "theme" || m.prompt.value != "light" {
			t.Fatalf("theme prompt: %+v", m.prompt)
		}
		return m
	}
	for _, tc := range []struct {
		value string
		want  []string
	}{
		{"", []string{"settings", "clear", "theme"}},
		{"dark", []string{"settings", "set", "theme", `"dark"`}},
		{"system", []string{"settings", "set", "theme", `"system"`}},
	} {
		m := open()
		m.prompt.value = tc.value
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%q: no request", tc.value)
		}
		argv = nil
		cmd()
		if len(argv) < len(tc.want) || !slices.Equal(argv[len(argv)-len(tc.want):], tc.want) {
			t.Fatalf("%q ran %q, want %q", tc.value, argv, tc.want)
		}
	}
	// an unknown theme sends nothing and says why
	m := open()
	m.prompt.value = "blue"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("an unknown theme sent a request")
	}
	if m = updated.(tuiModel); m.notice != "Enter system, light or dark" {
		t.Fatalf("notice = %q", m.notice)
	}
}
