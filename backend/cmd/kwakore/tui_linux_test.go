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
