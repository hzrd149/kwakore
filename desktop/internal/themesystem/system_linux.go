//go:build linux

package themesystem

import (
	"bufio"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalBus       = "org.freedesktop.portal.Desktop"
	portalPath      = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	portalSettings  = "org.freedesktop.portal.Settings"
	appearanceSpace = "org.freedesktop.appearance"
)

func Watch() (Appearance, <-chan Appearance, func()) {
	changes := make(chan Appearance, 1)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return watchOmarchyWithoutPortal(changes, Appearance{})
	}
	obj := conn.Object(portalBus, portalPath)
	portalAppearance := readPortalAppearance(obj)
	current, omarchyPath, omarchyStamp := preferredAppearance(portalAppearance)

	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	err = conn.AddMatchSignal(
		dbus.WithMatchObjectPath(portalPath),
		dbus.WithMatchInterface(portalSettings),
		dbus.WithMatchMember("SettingChanged"),
	)
	if err != nil {
		conn.RemoveSignal(signals)
		conn.Close()
		return watchOmarchyWithoutPortal(changes, portalAppearance)
	}
	stopped := make(chan struct{})
	go func() {
		defer close(changes)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ticker.C:
				next, path, stamp := preferredAppearance(portalAppearance)
				if path == omarchyPath && stamp == omarchyStamp {
					continue
				}
				omarchyPath, omarchyStamp = path, stamp
				current = next
				sendLatest(changes, current)
			case signal := <-signals:
				if signal == nil || len(signal.Body) < 3 {
					continue
				}
				namespace, _ := signal.Body[0].(string)
				key, _ := signal.Body[1].(string)
				if namespace != appearanceSpace || (key != "color-scheme" && key != "accent-color") {
					continue
				}
				portalAppearance = readPortalAppearance(obj)
				if omarchyPath == "" {
					current = portalAppearance
					sendLatest(changes, current)
				}
			}
		}
	}()
	var stopOnce sync.Once
	return current, changes, func() {
		stopOnce.Do(func() {
			close(stopped)
			conn.RemoveSignal(signals)
			conn.Close()
		})
	}
}

func watchOmarchyWithoutPortal(changes chan Appearance, fallback Appearance) (Appearance, <-chan Appearance, func()) {
	current, path, stamp := preferredAppearance(fallback)
	stopped := make(chan struct{})
	go func() {
		defer close(changes)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ticker.C:
				next, nextPath, nextStamp := preferredAppearance(fallback)
				if nextPath == path && nextStamp == stamp {
					continue
				}
				current, path, stamp = next, nextPath, nextStamp
				sendLatest(changes, current)
			}
		}
	}()
	var once sync.Once
	return current, changes, func() { once.Do(func() { close(stopped) }) }
}

func sendLatest(changes chan Appearance, appearance Appearance) {
	select {
	case changes <- appearance:
	default:
		<-changes
		changes <- appearance
	}
}

var omarchyThemePaths = func() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(state, "omarchy", "current", "theme", "colors.toml"),
		filepath.Join(config, "omarchy", "current", "theme", "colors.toml"),
	}
}

func preferredAppearance(fallback Appearance) (Appearance, string, string) {
	for _, path := range omarchyThemePaths() {
		appearance, stamp, ok := readOmarchyAppearance(path)
		if ok {
			return appearance, path, stamp
		}
	}
	return fallback, "", ""
}

func readOmarchyAppearance(path string) (Appearance, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Appearance{}, "", false
	}
	colors := make(map[string]color.NRGBA)
	mode := ""
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == 34 || value[0] == 39) {
			quote := value[0]
			if end := strings.IndexByte(value[1:], quote); end >= 0 {
				value = value[1 : end+1]
			}
		}
		if key == "mode" {
			mode = strings.ToLower(value)
			continue
		}
		if parsed, ok := parseHexColor(value); ok {
			colors[key] = parsed
		}
	}
	background, hasBackground := colors["background"]
	foreground, hasForeground := colors["foreground"]
	if scanner.Err() != nil || !hasBackground || !hasForeground {
		return Appearance{}, "", false
	}
	dark := mode != "light"
	if mode == "" {
		dark = relativeLuminance(background) < relativeLuminance(foreground)
	}
	appearance := Appearance{Dark: dark, Colors: colors}
	if accent, ok := colors["accent"]; ok {
		appearance.Accent, appearance.HasAccent = accent, true
	}
	info, _ := os.Stat(path)
	stamp := string(data)
	if info != nil {
		stamp = info.ModTime().UTC().String() + ":" + stamp
	}
	return appearance, stamp, true
}

func parseHexColor(value string) (color.NRGBA, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 && len(value) != 8 {
		return color.NRGBA{}, false
	}
	var components [4]uint8
	components[3] = 0xff
	for i := 0; i < len(value)/2; i++ {
		var v uint8
		for _, digit := range value[i*2 : i*2+2] {
			v <<= 4
			switch {
			case digit >= '0' && digit <= '9':
				v += uint8(digit - '0')
			case digit >= 'a' && digit <= 'f':
				v += uint8(digit-'a') + 10
			case digit >= 'A' && digit <= 'F':
				v += uint8(digit-'A') + 10
			default:
				return color.NRGBA{}, false
			}
		}
		components[i] = v
	}
	return color.NRGBA{R: components[0], G: components[1], B: components[2], A: components[3]}, true
}

func relativeLuminance(c color.NRGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

func readPortalAppearance(obj dbus.BusObject) Appearance {
	var out Appearance
	if value, ok := readPortalSetting(obj, "color-scheme"); ok {
		if scheme, ok := uintValue(value); ok {
			out.Dark = scheme == 1
		}
	}
	if value, ok := readPortalSetting(obj, "accent-color"); ok {
		if accent, ok := accentValue(value); ok {
			out.Accent, out.HasAccent = accent, true
		}
	}
	return out
}

func readPortalSetting(obj dbus.BusObject, key string) (any, bool) {
	var value dbus.Variant
	err := obj.Call(portalSettings+".ReadOne", 0, appearanceSpace, key).Store(&value)
	if err != nil {
		err = obj.Call(portalSettings+".Read", 0, appearanceSpace, key).Store(&value)
	}
	if err != nil {
		return nil, false
	}
	return unwrapVariant(value), true
}

func unwrapVariant(value any) any {
	for {
		variant, ok := value.(dbus.Variant)
		if !ok {
			return value
		}
		value = variant.Value()
	}
}

func uintValue(value any) (uint32, bool) {
	value = unwrapVariant(value)
	switch value := value.(type) {
	case uint32:
		return value, true
	case uint64:
		return uint32(value), true
	default:
		return 0, false
	}
}

func accentValue(value any) (color.NRGBA, bool) {
	value = unwrapVariant(value)
	components := [3]float64{}
	switch items := value.(type) {
	case []interface{}:
		if len(items) != 3 {
			return color.NRGBA{}, false
		}
		for i, item := range items {
			component, ok := item.(float64)
			if !ok {
				return color.NRGBA{}, false
			}
			components[i] = component
		}
	case []float64:
		if len(items) != 3 {
			return color.NRGBA{}, false
		}
		copy(components[:], items)
	case [3]float64:
		components = items
	default:
		return color.NRGBA{}, false
	}
	rgb := [3]uint8{}
	for i, component := range components {
		if component < 0 || component > 1 {
			return color.NRGBA{}, false
		}
		rgb[i] = uint8(math.Round(component * 255))
	}
	return color.NRGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 0xff}, true
}
