package backend

import (
	"encoding/json"
	"unicode"
	"unicode/utf8"
)

// The envelope head: what napEnqueue reads from an envelope before it decides
// whether to queue it (D-09, D-10, D-11). Go reads it itself and trusts
// nothing the host page checked, because a bypassed host page reaches Go
// with whatever it likes.

// napHead is an envelope's top level as napEnqueue needs it.
type napHead struct {
	// typ is the exact "type" key's value
	typ string
	// id is the raw correlation id when there is a valid one, else nil
	id json.RawMessage
	// badID says an id was present but is not a JSON string or number of
	// at most napMaxIDBytes bytes; such an envelope is dropped (D-11)
	badID bool
	// subID is the exact "subId" key's value when it is a valid string
	subID string
	// collide says two top-level keys are the same key to encoding/json's
	// case-insensitive struct decoding (D-10)
	collide bool
}

// parseNapHead reads an envelope's head. It fails (the envelope is dropped
// silently) when raw is not a JSON object, has more than napMaxTopKeys keys,
// or has no exact "type" key holding a non-empty JSON string.
//
// It decodes into a map because encoding/json keeps a map's keys exact: two
// keys that only a case-insensitive struct decode would merge (type and TYPE)
// show up as two entries, so the collision is visible. An exactly duplicated
// key collapses to its last value, both here and in the handler's struct
// decode, so the two never disagree on it. The handlers' struct decodes do
// fold case, which is why type is read from the exact key only and why any
// fold collision is refused: otherwise {"type":"storage.keys","TYPE":
// "upload.upload"} could be one type to this check and another to a handler.
func parseNapHead(raw []byte) (napHead, bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil || len(top) > napMaxTopKeys {
		return napHead{}, false
	}
	var h napHead
	seen := make(map[string]struct{}, len(top))
	for k := range top {
		f := foldKey(k)
		if _, dup := seen[f]; dup {
			h.collide = true
		}
		seen[f] = struct{}{}
	}
	t, ok := top["type"]
	if !ok || len(t) == 0 || t[0] != '"' || json.Unmarshal(t, &h.typ) != nil || h.typ == "" {
		return napHead{}, false
	}
	if raw, present := top["id"]; present {
		// present always means a value: even "id":null is not absent
		id, ok := napValidID(raw)
		h.id, h.badID = id, !ok || len(raw) == 0
	}
	if raw, present := top["subId"]; present {
		h.subID, _ = napValidSubID(raw)
	}
	return h, true
}

// foldKey is the key two JSON object keys share exactly when encoding/json's
// struct decoding treats them as the same field: ASCII letters upper-cased,
// every other rune replaced by the smallest rune of its unicode.SimpleFold
// orbit. strings.ToLower or ToUpper would not do: the Kelvin sign (U+212A)
// folds to K and the long s (U+017F) to S.
//
// Source: GOROOT/src/encoding/json/fold.go (appendFoldedName, foldRune).
func foldKey(k string) string {
	out := make([]byte, 0, len(k))
	for i := 0; i < len(k); {
		if c := k[i]; c < utf8.RuneSelf {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
			out = append(out, c)
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(k[i:])
		for {
			r2 := unicode.SimpleFold(r)
			if r2 <= r {
				r = r2
				break
			}
			r = r2
		}
		out = utf8.AppendRune(out, r)
		i += n
	}
	return string(out)
}

// napValidID checks a correlation id (D-11). An absent id is fine (nil,
// true): fire-and-forget types carry none. A JSON string whose unescaped
// value is at most napMaxIDBytes bytes, or a JSON number whose raw token is,
// comes back as its raw token, which replies echo verbatim. Anything else
// (object, array, bool, null, too long) is (nil, false).
func napValidID(raw json.RawMessage) (json.RawMessage, bool) {
	if raw == nil {
		return nil, true
	}
	if len(raw) == 0 {
		return nil, false
	}
	switch c := raw[0]; {
	case c == '"':
		var s string
		if json.Unmarshal(raw, &s) != nil || len(s) > napMaxIDBytes {
			return nil, false
		}
		return raw, true
	case c == '-' || ('0' <= c && c <= '9'):
		var n json.Number
		if len(raw) > napMaxIDBytes || json.Unmarshal(raw, &n) != nil {
			return nil, false
		}
		return raw, true
	}
	return nil, false
}

// napValidSubID checks a subscription id: a JSON string of 1 to
// napMaxIDBytes bytes after unescaping.
func napValidSubID(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" || len(s) > napMaxIDBytes {
		return "", false
	}
	return s, true
}
