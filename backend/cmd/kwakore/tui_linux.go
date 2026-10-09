//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

var tuiTabs = []string{"Overview", "Installed", "Discover", "Settings", "Signer", "Diagnostics", "Windows"}

type tuiItem struct {
	label, address, detail                  string
	format                                  string
	author, authorName, description, reason string
	sources                                 []string
	eventID, artifactHash                   string
	createdAt                               int64
	available                               bool
}

type tuiResult struct {
	kind string
	data json.RawMessage
	err  string
	seq  uint64
}

type tuiPrompt struct {
	title, action, value, address string
	secret                        bool
}

type tuiModel struct {
	socket                             string
	timeout                            time.Duration
	tab, cursor, offset, width, height int
	query, notice                      string
	busy, help                         bool
	prompt                             *tuiPrompt
	items                              []tuiItem
	installed, discovered              []tuiItem
	settings                           map[string]json.RawMessage
	sources                            map[string]string
	signer, health, diagnostics        json.RawMessage
	permissions                        json.RawMessage
	selectedAddress                    string
	permissionView                     bool
	detailView                         bool
	pairURI                            string
	page                               int
	more                               bool
	refreshing                         bool
	discoveryLoading                   bool
	loadingStarted                     time.Time
	spinner                            int
	catalogFetched                     string
	catalogComplete                    bool
	discoverySeq                       uint64
}

type tuiTick struct{ seq uint64 }

func tickTUI(seq uint64) tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tuiTick{seq} })
}

func (m *tuiModel) beginDiscovery(refresh bool) tea.Cmd {
	m.discoverySeq++
	m.busy, m.refreshing = true, refresh
	m.discoveryLoading = true
	m.loadingStarted = time.Now()
	m.spinner = 0
	return tea.Batch(m.discoverAll(refresh), tickTUI(m.discoverySeq))
}

func runTUI(socket string, timeout time.Duration) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("tui requires an interactive terminal")
	}
	m := tuiModel{socket: socket, timeout: timeout, settings: make(map[string]json.RawMessage), height: 24, width: 80}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd { return m.request("overview", "status") }

func (m tuiModel) request(kind string, args ...string) tea.Cmd {
	socket, timeout := m.socket, m.timeout
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return tuiResult{kind: kind, err: "cannot locate kwak executable"}
		}
		argv := []string{"--json"}
		if socket != "" {
			argv = append(argv, "--socket", socket)
		}
		if timeout != 0 {
			argv = append(argv, "--timeout", timeout.String())
		}
		argv = append(argv, args...)
		ctx, cancel := context.WithTimeout(context.Background(), 190*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, argv...)
		output, err := cmd.Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				var e struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal(exit.Stderr, &e) == nil && e.Error.Message != "" {
					return tuiResult{kind: kind, err: e.Error.Message}
				}
			}
			return tuiResult{kind: kind, err: "operation unavailable; check daemon state"}
		}
		if !json.Valid(output) {
			return tuiResult{kind: kind, err: "invalid daemon response"}
		}
		return tuiResult{kind: kind, data: json.RawMessage(output)}
	}
}

// discoverAll reads the daemon's cached catalog page by page. Filtering then
// happens entirely in the TUI, including while the user edits a search.
func (m tuiModel) discoverAll(refresh bool) tea.Cmd {
	seq := m.discoverySeq
	return func() tea.Msg {
		items := make([]json.RawMessage, 0)
		offset := 0
		var fetchedAt *string
		var complete bool
		for {
			args := []string{"discover", "--offset", fmt.Sprint(offset), "--limit", "500"}
			if refresh && offset == 0 {
				args = append(args, "--refresh")
			}
			result := m.request("discover", args...)().(tuiResult)
			if result.err != "" {
				result.seq = seq
				return result
			}
			var page struct {
				Items      []json.RawMessage `json:"items"`
				NextOffset *int              `json:"next_offset"`
				FetchedAt  *string           `json:"fetched_at"`
				Complete   bool              `json:"complete"`
			}
			if json.Unmarshal(result.data, &page) != nil {
				return tuiResult{kind: "discover", err: "invalid catalog response", seq: seq}
			}
			items = append(items, page.Items...)
			fetchedAt, complete = page.FetchedAt, page.Complete
			if page.NextOffset == nil {
				break
			}
			if *page.NextOffset <= offset {
				return tuiResult{kind: "discover", err: "invalid catalog pagination", seq: seq}
			}
			offset = *page.NextOffset
		}
		data, _ := json.Marshal(struct {
			Items     []json.RawMessage `json:"items"`
			FetchedAt *string           `json:"fetched_at"`
			Complete  bool              `json:"complete"`
		}{items, fetchedAt, complete})
		return tuiResult{kind: "discover", data: data, seq: seq}
	}
}

func (m tuiModel) refresh() tea.Cmd {
	switch m.tab {
	case 0:
		return m.request("overview", "status")
	case 1:
		return m.request("installed", "installed", "--offset", fmt.Sprint(m.page*100), "--limit", "100")
	case 2:
		return m.discoverAll(false)
	case 3:
		return m.request("settings", "settings", "inspect")
	case 4:
		return m.request("signer", "signer", "status")
	case 5:
		return m.request("diagnostics", "diagnostics")
	default:
		return m.request("windows", "windows")
	}
}

func (m *tuiModel) setTab(tab int) tea.Cmd {
	m.tab, m.cursor, m.offset, m.page, m.notice = tab, 0, 0, 0, ""
	m.items = nil
	m.permissionView = false
	m.detailView = false
	m.busy = true
	if tab == 2 {
		return m.beginDiscovery(false)
	}
	m.discoveryLoading = false
	m.refreshing = false
	return m.refresh()
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = x.Width, x.Height
	case tuiResult:
		if x.kind == "discover" && (x.seq != m.discoverySeq || m.tab != 2) {
			return m, nil
		}
		m.busy = false
		if x.kind == "discover" {
			m.refreshing = false
			m.discoveryLoading = false
		}
		if x.err != "" {
			m.notice = x.err
			return m, nil
		}
		if (x.kind == "installed" && m.tab != 1) || (x.kind == "discover" && m.tab != 2) || (x.kind == "windows" && m.tab != 6) {
			return m, nil
		}
		if x.kind == "settings" && !strings.Contains(string(x.data), `"sources"`) {
			m.busy = true
			return m, m.request("settings", "settings", "inspect")
		}
		m.consume(x)
		if x.kind == "install" || x.kind == "update" || x.kind == "uninstall" || x.kind == "stop" {
			if m.tab == 2 {
				return m, m.beginDiscovery(false)
			}
			m.busy = true
			return m, m.refresh()
		}
		if x.kind == "permission-set" || x.kind == "permission-clear" {
			m.busy = true
			return m, m.request("permissions", "permissions", "get", m.selectedAddress)
		}
		if x.kind == "pair-cancel" {
			m.busy = true
			return m, m.request("signer", "signer", "status")
		}
	case tuiTick:
		if m.discoveryLoading && m.tab == 2 && x.seq == m.discoverySeq {
			m.spinner = (m.spinner + 1) % 4
			return m, tickTUI(x.seq)
		}
	case tea.KeyMsg:
		if m.prompt != nil {
			return m.promptKey(x)
		}
		if m.permissionView {
			switch x.String() {
			case "esc", "q":
				m.permissionView = false
			case "a":
				m.prompt = &tuiPrompt{title: "Rule: PERMISSION allow|deny [SUBJECT]", action: "permission-set", address: m.selectedAddress}
			case "d":
				m.prompt = &tuiPrompt{title: "Clear rule: PERMISSION [SUBJECT]", action: "permission-clear", address: m.selectedAddress}
			case "r":
				m.busy = true
				return m, m.request("permissions", "permissions", "get", m.selectedAddress)
			}
			return m, nil
		}
		if m.help {
			m.help = false
			return m, nil
		}
		if m.detailView {
			switch x.String() {
			case "esc", "backspace":
				m.detailView = false
				return m, nil
			case "i", "enter":
				return m.selectedDiscoverInstall()
			case "l":
				return m.selectedDiscoverLaunch()
			}
			return m, nil
		}
		switch x.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.help = true
		case "tab", "right":
			return m, m.setTab((m.tab + 1) % len(tuiTabs))
		case "shift+tab", "left":
			return m, m.setTab((m.tab + len(tuiTabs) - 1) % len(tuiTabs))
		case "1", "2", "3", "4", "5", "6", "7":
			return m, m.setTab(int(x.String()[0] - '1'))
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		case "down", "j":
			if m.cursor+1 < len(m.items) {
				m.cursor++
			}
			if m.cursor >= m.offset+m.listHeight() {
				m.offset++
			}
		case "pgdown":
			if m.tab != 2 && m.more {
				m.page++
				m.cursor, m.offset = 0, 0
				m.busy = true
				return m, m.refresh()
			}
		case "pgup":
			if m.tab != 2 && m.page > 0 {
				m.page--
				m.cursor, m.offset = 0, 0
				m.busy = true
				return m, m.refresh()
			}
		case "r":
			if m.tab == 2 {
				if m.busy {
					return m, nil
				}
				return m, m.beginDiscovery(false)
			}
			m.busy = true
			return m, m.refresh()
		case "R":
			if m.tab == 2 {
				if m.busy {
					return m, nil
				}
				return m, m.beginDiscovery(true)
			}
			if m.tab == 3 {
				m.busy = true
				return m, m.request("settings", "settings", "reload")
			}
		case "/":
			if m.tab == 2 {
				m.prompt = &tuiPrompt{title: "Filter discovered napplets", action: "search", value: m.query}
			}
		case "enter":
			return m.activate()
		case "i":
			if m.tab == 2 {
				return m.selectedDiscoverInstall()
			}
			if m.tab == 1 {
				m.prompt = &tuiPrompt{title: "Install address or naddr", action: "install"}
			}
		case "d":
			if m.tab == 2 && m.cursor < len(m.items) {
				m.detailView = true
			}
		case "u":
			if m.tab == 1 {
				return m.itemAction("update")
			}
		case "l":
			if m.tab == 1 {
				return m.itemAction("launch")
			}
			if m.tab == 2 {
				return m.selectedDiscoverLaunch()
			}
		case "x":
			if m.tab == 1 {
				return m.itemAction("uninstall")
			}
			if m.tab == 6 {
				return m.itemAction("stop")
			}
		case "p":
			if m.tab == 1 {
				return m.itemAction("permissions")
			}
		case "c":
			if m.tab == 3 {
				return m.itemAction("clear")
			}
		case "n":
			if m.tab == 4 {
				m.prompt = &tuiPrompt{title: "Disconnect signer? Type yes", action: "signer-none"}
			}
		case "s":
			if m.tab == 4 {
				m.prompt = &tuiPrompt{title: "System signer socket (blank for default)", action: "signer-system"}
			}
		case "b":
			if m.tab == 4 {
				m.prompt = &tuiPrompt{title: "Bunker URL (hidden)", action: "signer-bunker", secret: true}
			}
		case "e":
			if m.tab == 4 {
				m.prompt = &tuiPrompt{title: "nsec (hidden)", action: "signer-nsec", secret: true}
			}
		case "a":
			if m.tab == 2 {
				m.prompt = &tuiPrompt{title: "Install address or naddr", action: "install"}
			}
			if m.tab == 4 {
				m.busy = true
				return m, m.request("pair", "signer", "pair", "start")
			}
		case "w":
			if m.tab == 4 {
				m.busy = true
				return m, m.request("signer", "signer", "pair", "wait")
			}
		case "C":
			if m.tab == 4 {
				m.busy = true
				m.pairURI = ""
				return m, m.request("pair-cancel", "signer", "pair", "cancel")
			}
		}
	}
	return m, nil
}

func (m tuiModel) listHeight() int {
	if m.height < 14 {
		return 4
	}
	return m.height - 11
}

func (m tuiModel) itemAction(action string) (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	switch action {
	case "launch":
		if it.format != "napplet" {
			m.notice = "Napps cannot launch on this Linux build: the window runtime supports napplets only"
			return m, nil
		}
		m.busy = true
		m.notice = "Opening " + safeText(it.label) + "…"
		return m, m.request(action, action, it.address)
	case "update":
		m.busy = true
		return m, m.request(action, action, it.address)
	case "uninstall":
		m.prompt = &tuiPrompt{title: "Type REMOVE to uninstall " + safeText(it.label), action: action, address: it.address}
	case "permissions":
		m.selectedAddress = it.address
		m.permissionView = true
		m.busy = true
		return m, m.request("permissions", "permissions", "get", it.address)
	case "clear":
		m.prompt = &tuiPrompt{title: "Type CLEAR to remove override for " + it.address, action: action, address: it.address}
	case "stop":
		m.prompt = &tuiPrompt{title: "Type STOP to close " + safeText(it.label), action: action, address: it.address}
	}
	return m, nil
}

func (m tuiModel) activate() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	switch m.tab {
	case 1:
		return m.itemAction("launch")
	case 2:
		m.detailView = true
	case 3:
		if it.address == "relays" || it.address == "blossom_servers" {
			m.prompt = &tuiPrompt{title: "Edit " + it.address + " (comma-separated URLs; scheme optional)", action: "setting-list", address: it.address, value: strings.Join(parseStringList(m.settings[it.address]), ", ")}
		} else {
			m.prompt = &tuiPrompt{title: "Set " + it.address + " to true or false", action: "setting-bool", address: it.address}
		}
	case 4:
		m.busy = true
		return m, m.request("signer", "signer", "status")
	case 6:
		return m.itemAction("stop")
	}
	return m, nil
}

func (m tuiModel) selectedDiscoverInstall() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	if !it.available {
		m.notice = "This manifest is unavailable: " + safeText(it.reason)
		return m, nil
	}
	m.detailView = false
	m.prompt = &tuiPrompt{title: "Type INSTALL to install " + safeText(it.label), action: "install-selected", address: it.address}
	return m, nil
}

func (m tuiModel) selectedDiscoverLaunch() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	if it.format != "napplet" {
		m.notice = "Napps cannot launch on this Linux build: the window runtime supports napplets only"
		return m, nil
	}
	m.busy = true
	m.detailView = false
	m.notice = "Opening " + safeText(it.label) + "…"
	return m, m.request("launch", "launch", it.address)
}

func (m tuiModel) promptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.prompt
	switch key.String() {
	case "esc":
		m.prompt = nil
		return m, nil
	case "backspace", "ctrl+h":
		r := []rune(p.value)
		if len(r) > 0 {
			p.value = string(r[:len(r)-1])
		}
		return m, nil
	case "enter":
		m.prompt = nil
		value := strings.TrimSpace(p.value)
		var cmd tea.Cmd
		switch p.action {
		case "search":
			m.query = value
			m.applyDiscoverFilter()
		case "install":
			if value != "" {
				cmd = m.request("install", "install", value)
			}
		case "install-selected":
			if value == "INSTALL" {
				cmd = m.request("install", "install", p.address)
			}
		case "uninstall":
			if value == "REMOVE" {
				cmd = m.request("uninstall", "uninstall", "--yes", p.address)
			}
		case "stop":
			if value == "STOP" {
				cmd = m.request("stop", "stop", p.address)
			}
		case "clear":
			if value == "CLEAR" {
				cmd = m.request("settings", "settings", "clear", p.address)
			}
		case "setting-bool":
			if value == "true" || value == "false" {
				cmd = m.request("settings", "settings", "set", p.address, value)
			} else {
				m.notice = "Enter true or false"
			}
		case "setting-list":
			list := []string{}
			if value != "" {
				for _, s := range strings.Split(value, ",") {
					list = append(list, strings.TrimSpace(s))
				}
			}
			b, _ := json.Marshal(list)
			cmd = m.request("settings", "settings", "set", p.address, string(b))
		case "signer-none":
			if value == "yes" {
				cmd = m.request("signer", "signer", "switch", "none")
			}
		case "signer-system":
			args := []string{"signer", "switch", "system"}
			if value != "" {
				args = append(args, "--signer-socket", value)
			}
			cmd = m.request("signer", args...)
		case "signer-nsec", "signer-bunker":
			// Secret input is passed through a pipe, never an argument or a file.
			if value != "" {
				mode := strings.TrimPrefix(p.action, "signer-")
				cmd = m.secretRequest(mode, value)
			}
		case "permission-set":
			parts := strings.Fields(value)
			if len(parts) >= 2 && len(parts) <= 3 && (parts[1] == "allow" || parts[1] == "deny") {
				args := []string{"permissions", "set", p.address, parts[0], parts[1]}
				if len(parts) == 3 {
					args = append(args, "--subject", parts[2])
				}
				cmd = m.request("permission-set", args...)
			} else {
				m.notice = "Use PERMISSION allow|deny [SUBJECT]"
			}
		case "permission-clear":
			parts := strings.Fields(value)
			if len(parts) >= 1 && len(parts) <= 2 {
				args := []string{"permissions", "clear", p.address, parts[0]}
				if len(parts) == 2 {
					args = append(args, "--subject", parts[1])
				}
				cmd = m.request("permission-clear", args...)
			} else {
				m.notice = "Use PERMISSION [SUBJECT]"
			}
		}
		if cmd != nil {
			m.busy = true
			m.discoveryLoading = false
		}
		return m, cmd
	default:
		if len(p.value) < 4096 {
			for _, r := range key.Runes {
				if unicode.IsPrint(r) {
					p.value += string(r)
				}
			}
		}
	}
	return m, nil
}

func (m tuiModel) secretRequest(mode, secret string) tea.Cmd {
	socket, timeout := m.socket, m.timeout
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return tuiResult{kind: "signer", err: "cannot locate kwak executable"}
		}
		args := []string{"--json"}
		if socket != "" {
			args = append(args, "--socket", socket)
		}
		if timeout != 0 {
			args = append(args, "--timeout", timeout.String())
		}
		args = append(args, "signer", "switch", mode, "--secret-stdin")
		ctx, cancel := context.WithTimeout(context.Background(), 190*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Stdin = strings.NewReader(secret + "\n")
		out, err := cmd.Output()
		if err != nil {
			return tuiResult{kind: "signer", err: "signer switch failed; check signer status"}
		}
		if !json.Valid(out) {
			return tuiResult{kind: "signer", err: "invalid signer response"}
		}
		return tuiResult{kind: "signer", data: out}
	}
}

func (m *tuiModel) consume(x tuiResult) {
	switch x.kind {
	case "overview":
		m.health = x.data
		m.notice = "Service connected"
	case "installed", "discover":
		var page struct {
			Items []struct {
				Address, Name, Format string
				Description           string   `json:"description"`
				Author                string   `json:"author"`
				AuthorName            string   `json:"author_name"`
				UnavailableReason     string   `json:"unavailable_reason"`
				Sources               []string `json:"sources"`
				Available             bool
				Version               struct {
					CreatedAt    int64  `json:"created_at"`
					EventID      string `json:"event_id"`
					ArtifactHash string `json:"artifact_hash"`
				}
			} `json:"items"`
			NextOffset *int    `json:"next_offset"`
			FetchedAt  *string `json:"fetched_at"`
			Complete   bool    `json:"complete"`
		}
		if json.Unmarshal(x.data, &page) != nil {
			m.notice = "Invalid catalog response"
			return
		}
		items := make([]tuiItem, 0, len(page.Items))
		for _, it := range page.Items {
			name := safeText(it.Name)
			if name == "" {
				name = it.Address
			}
			state := "available"
			if !it.Available {
				state = "unavailable"
			}
			author := it.AuthorName
			if author == "" {
				author = shortAuthor(it.Author)
			}
			items = append(items, tuiItem{label: name, address: it.Address, format: it.Format, detail: it.Format + " · " + safeText(author) + " · " + state,
				author: it.Author, authorName: it.AuthorName, description: it.Description, reason: it.UnavailableReason,
				sources: it.Sources, eventID: it.Version.EventID, artifactHash: it.Version.ArtifactHash,
				createdAt: it.Version.CreatedAt, available: it.Available})
		}
		m.items = items
		m.more = page.NextOffset != nil
		if x.kind == "installed" {
			m.installed = items
			m.items = items
		} else {
			m.discovered = items
			m.catalogComplete = page.Complete
			m.catalogFetched = ""
			if page.FetchedAt != nil {
				m.catalogFetched = *page.FetchedAt
			}
			m.applyDiscoverFilter()
		}
		m.notice = fmt.Sprintf("%d entries", len(m.items))
	case "settings":
		var raw map[string]json.RawMessage
		if json.Unmarshal(x.data, &raw) != nil {
			m.notice = "Invalid settings response"
			return
		}
		if nested, ok := raw["settings"]; ok {
			_ = json.Unmarshal(raw["sources"], &m.sources)
			_ = json.Unmarshal(nested, &raw)
		}
		m.settings = raw
		m.items = nil
		for _, field := range []string{"relays", "blossom_servers", "discover_on_user_relays", "desktop_entries", "gnome_search"} {
			m.items = append(m.items, tuiItem{label: field, address: field, detail: safeText(string(raw[field])) + "  (" + m.sources[field] + ")"})
		}
		m.notice = "Settings loaded"
	case "signer":
		m.signer = x.data
		m.pairURI = ""
		m.notice = "Signer status updated"
	case "diagnostics":
		m.diagnostics = x.data
		m.notice = "Diagnostics updated"
	case "windows":
		// Decode by explicit wire names so the displayed token is the stop token.
		var raw struct {
			Items []struct {
				WindowID string `json:"window_id"`
				Address  string `json:"address"`
				Name     string `json:"name"`
			} `json:"items"`
		}
		if json.Unmarshal(x.data, &raw) != nil {
			m.notice = "Invalid windows response"
			return
		}
		m.items = nil
		for _, w := range raw.Items {
			m.items = append(m.items, tuiItem{label: safeText(w.Name), address: w.WindowID, detail: safeText(w.Address)})
		}
		m.notice = fmt.Sprintf("%d active windows", len(m.items))
	case "permissions":
		m.permissions = x.data
		m.notice = "Permissions for " + safeText(m.selectedAddress)
	case "pair":
		var p struct {
			PairingURI string `json:"pairing_uri"`
		}
		if json.Unmarshal(x.data, &p) == nil {
			m.pairURI = p.PairingURI
			m.notice = "Share the private pairing URI only with your signer; press w to wait"
		}
	default:
		m.notice = safeText(string(x.data))
	}
}

func filterDiscovered(items []tuiItem, query string) []tuiItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]tuiItem(nil), items...)
	}
	filtered := make([]tuiItem, 0)
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.label), query) || strings.Contains(strings.ToLower(it.address), query) || strings.Contains(strings.ToLower(it.detail), query) ||
			strings.Contains(strings.ToLower(it.author), query) || strings.Contains(strings.ToLower(it.description), query) {
			filtered = append(filtered, it)
		}
	}
	return filtered
}

func shortAuthor(author string) string {
	if len(author) < 16 {
		return author
	}
	return author[:8] + "…" + author[len(author)-8:]
}

func (m *tuiModel) applyDiscoverFilter() {
	m.items = filterDiscovered(m.discovered, m.query)
	m.cursor, m.offset, m.page = 0, 0, 0
}

func parseStringList(raw json.RawMessage) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

func safeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
			continue
		}
		if unicode.IsPrint(r) && r != '\x1b' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncateTUILine(s string, width int) string {
	if width < 8 {
		width = 8
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}

func (m tuiModel) discoverDetail(it tuiItem) string {
	var b strings.Builder
	b.WriteString(safeText(it.label) + "\n")
	b.WriteString("Format: " + safeText(strings.SplitN(it.detail, " · ", 2)[0]) + "\n")
	if it.authorName != "" {
		b.WriteString("Author: " + safeText(it.authorName) + "\n")
	}
	b.WriteString("Author key: " + safeText(it.author) + "\n")
	b.WriteString("Address: " + safeText(it.address) + "\n")
	if it.createdAt > 0 {
		b.WriteString("Published: " + time.Unix(it.createdAt, 0).UTC().Format(time.RFC3339) + "\n")
	}
	if it.eventID != "" {
		b.WriteString("Event: " + safeText(it.eventID) + "\n")
	}
	if it.artifactHash != "" {
		b.WriteString("Artifact hash: " + safeText(it.artifactHash) + "\n")
	}
	if it.available {
		b.WriteString("Status: available\n")
	} else {
		b.WriteString("Status: unavailable · " + safeText(it.reason) + "\n")
	}
	if it.description != "" {
		b.WriteString("\nDescription\n" + safeText(it.description) + "\n")
	}
	if len(it.sources) > 0 {
		b.WriteString("\nSource\n")
		for _, source := range it.sources {
			b.WriteString(safeText(source) + "\n")
		}
	}
	b.WriteString("\nEsc back")
	if it.available {
		b.WriteString("   i install")
	}
	b.WriteString("   l launch installed napplet")
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString("KWAKORE  ")
	for i, t := range tuiTabs {
		if i == m.tab {
			b.WriteString("[" + t + "] ")
		} else {
			b.WriteString(t + " ")
		}
	}
	b.WriteString("\n" + strings.Repeat("─", min(m.width, 90)) + "\n")
	if m.tab == 2 {
		if m.discoveryLoading {
			frames := []string{"◐", "◓", "◑", "◒"}
			activity := "Loading cached catalog"
			if m.refreshing {
				activity = "Refreshing napplets from relays"
			}
			b.WriteString(fmt.Sprintf("%s %s… %ds elapsed\n", frames[m.spinner%len(frames)], activity, int(time.Since(m.loadingStarted).Seconds())))
		} else if m.catalogComplete {
			b.WriteString(fmt.Sprintf("Catalog: %d napplets and napps", len(m.discovered)))
			if m.catalogFetched != "" {
				b.WriteString(" · fetched " + safeText(m.catalogFetched))
			}
			b.WriteString("\n")
		} else {
			b.WriteString("No completed catalog refresh yet · press R to fetch from relays\n")
		}
	}
	if m.help {
		b.WriteString("1-7 or Tab: switch screens   j/k or arrows: select   Enter: open/edit\n")
		b.WriteString("r: reload   R: refresh catalog or reload config   PgUp/PgDn: pages\n")
		b.WriteString("Installed: Enter/l launch, i install, u update, x uninstall, p permissions\n")
		b.WriteString("Discover: / filter, r reload cache, R fetch relays, Enter/d details, i install, l launch installed, a address\n")
		b.WriteString("Settings: Enter edit, c clear override, R reload config\n")
		b.WriteString("Signer: n none, s system, e nsec, b bunker, a pair, w wait, C cancel\n")
		b.WriteString("Windows: Enter or x closes the selected window after confirmation\n")
		b.WriteString("Esc closes prompts; q quits. Press any key to return.\n")
		return b.String()
	}
	if m.prompt != nil {
		b.WriteString(safeText(m.prompt.title) + "\n\n")
		if m.prompt.secret {
			b.WriteString(strings.Repeat("•", len([]rune(m.prompt.value))))
		} else {
			b.WriteString(safeText(m.prompt.value))
		}
		b.WriteString("▌\n\nEnter: confirm   Esc: cancel\n")
		if m.prompt.action == "search" {
			matches := filterDiscovered(m.discovered, m.prompt.value)
			b.WriteString(fmt.Sprintf("\n%d matches in the cached catalog\n", len(matches)))
			for i, it := range matches {
				if i >= m.listHeight() {
					break
				}
				b.WriteString("  " + safeText(it.label) + "\n")
			}
		}
		return b.String()
	}
	if m.permissionView {
		b.WriteString("Permissions for " + safeText(m.selectedAddress) + "\n\n" + prettyJSON(m.permissions) + "\n")
		b.WriteString("a add or change rule   d clear rule   r refresh   Esc back\n")
		return b.String()
	}
	if m.detailView && m.tab == 2 {
		if m.cursor < len(m.items) {
			b.WriteString(m.discoverDetail(m.items[m.cursor]))
		}
		return b.String()
	}
	switch m.tab {
	case 0:
		b.WriteString("Service\n" + prettyJSON(m.health) + "\n")
	case 1, 2, 3, 6:
		if m.tab == 2 {
			b.WriteString("Search: " + safeText(m.query) + "\n")
			b.WriteString("Enter details   i install   l launch installed\n")
		} else if m.tab == 1 {
			b.WriteString("Enter launch   p permissions   u update   x uninstall\n")
		}
		if len(m.items) == 0 {
			if m.tab == 2 && m.query != "" && len(m.discovered) > 0 {
				b.WriteString("No matches. Press / to change the filter.\n")
			} else {
				b.WriteString("No entries loaded. Press r to refresh.\n")
			}
		}
		for i := m.offset; i < len(m.items) && i < m.offset+m.listHeight(); i++ {
			it := m.items[i]
			marker := "  "
			if i == m.cursor {
				marker = "› "
			}
			b.WriteString(truncateTUILine(marker+safeText(it.label)+"  "+safeText(it.detail), m.width) + "\n")
		}
		if m.tab == 1 && len(m.permissions) > 0 {
			b.WriteString("\nSelected permissions:\n" + prettyJSON(m.permissions) + "\n")
		}
	case 4:
		b.WriteString("Signer\n" + prettyJSON(m.signer) + "\n")
		if m.pairURI != "" {
			b.WriteString("\nPrivate pairing URI (share only with signer):\n" + safeText(m.pairURI) + "\n")
		}
	case 5:
		b.WriteString("Diagnostics\n" + prettyJSON(m.diagnostics) + "\n")
	}
	if m.busy {
		b.WriteString("\nWorking…\n")
	}
	if m.notice != "" {
		b.WriteString("\n" + safeText(m.notice) + "\n")
	}
	b.WriteString("\n? help   Tab screens   r refresh   q quit")
	return b.String()
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "Loading…"
	}
	var out strings.Builder
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "Invalid response"
	}
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return safeMultiline(out.String())
}

func safeMultiline(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' {
			b.WriteRune(r)
		} else if r == '\t' {
			b.WriteString("  ")
		} else if unicode.IsPrint(r) && r != '\x1b' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
