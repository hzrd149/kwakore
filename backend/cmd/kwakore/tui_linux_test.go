//go:build linux

package main

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUISettingsAndCatalogResults(t *testing.T) {
	m := tuiModel{settings: map[string]json.RawMessage{}}
	m.consume(tuiResult{kind: "settings", data: []byte(`{"settings":{"relays":["wss://relay.example"],"blossom_servers":[],"discover_on_user_relays":true,"desktop_entries":false,"gnome_search":true}}`)})
	if len(m.items) != 5 || m.items[0].address != "relays" || !strings.Contains(m.items[0].detail, "relay.example") {
		t.Fatalf("settings items: %+v", m.items)
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
	if m.prompt == nil || m.prompt.action != "uninstall" {
		t.Fatalf("uninstall prompt: %+v", m.prompt)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if cmd != nil {
		t.Fatal("empty confirmation triggered uninstall")
	}
	m.prompt = &tuiPrompt{title: "Secret", action: "signer-nsec", value: "private-key", secret: true}
	view := m.View()
	if strings.Contains(view, "private-key") || !strings.Contains(view, "•••") {
		t.Fatalf("secret leaked or mask missing: %q", view)
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
	m = tuiModel{tab: 2, items: []tuiItem{{label: "Notes", address: "35129:abc:notes", format: "napplet"}}}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(tuiModel)
	if cmd == nil || !m.busy {
		t.Fatalf("discovery launch did not invoke service: %+v", m)
	}
}
