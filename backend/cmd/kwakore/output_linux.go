//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// writeCLIResult preserves the protocol result in JSON mode. The default
// presentation names fields and keeps nested values legible without changing
// the daemon's response or exposing fields it did not return.
func writeCLIResult(w io.Writer, method string, raw json.RawMessage, jsonOutput bool) error {
	if jsonOutput {
		_, err := w.Write(append(raw, '\n'))
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("invalid daemon response")
	}
	title := strings.ReplaceAll(method, ".", " ")
	if _, err := fmt.Fprintln(w, strings.ToUpper(title[:1])+title[1:]); err != nil {
		return err
	}
	return printValue(w, value, "")
}

func printValue(w io.Writer, value any, indent string) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			_, err := fmt.Fprintln(w, indent+"(none)")
			return err
		}
		for _, key := range keys {
			label := strings.ReplaceAll(key, "_", " ")
			switch child := v[key].(type) {
			case map[string]any, []any:
				if _, err := fmt.Fprintf(w, "%s%s:\n", indent, label); err != nil {
					return err
				}
				if err := printValue(w, child, indent+"  "); err != nil {
					return err
				}
			default:
				if _, err := fmt.Fprintf(w, "%s%s: %s\n", indent, label, displayValue(child)); err != nil {
					return err
				}
			}
		}
	case []any:
		if len(v) == 0 {
			_, err := fmt.Fprintln(w, indent+"(none)")
			return err
		}
		for _, item := range v {
			if object, ok := item.(map[string]any); ok {
				if _, err := fmt.Fprintln(w, indent+"-"); err != nil {
					return err
				}
				if err := printValue(w, object, indent+"  "); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(w, "%s- %s\n", indent, displayValue(item)); err != nil {
				return err
			}
		}
	default:
		_, err := fmt.Fprintln(w, indent+displayValue(v))
		return err
	}
	return nil
}

func displayValue(value any) string {
	if value == nil {
		return "none"
	}
	if text, ok := value.(string); ok {
		if strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) }) >= 0 {
			return strconv.Quote(text)
		}
		return text
	}
	return fmt.Sprint(value)
}
