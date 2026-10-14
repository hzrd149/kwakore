package backend

import (
	"net/url"
	"strconv"
	"strings"
)

type blossomURI struct {
	Hash, Extension  string
	Servers, Authors []string
	Size             int64
}

// parseBlossomURI implements BUD-10: blossom:<sha256>.<ext>?xs=...&as=...&sz=...
// Hints are discovery data; they never override fetch policy or hash checks.
func parseBlossomURI(raw string) (blossomURI, error) {
	var out blossomURI
	invalid := func() (blossomURI, error) { return out, rerr("invalid-request", "invalid BUD-10 Blossom URI") }
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "blossom" || u.Host != "" || u.Fragment != "" {
		return invalid()
	}
	hash, ext, found := strings.Cut(u.Opaque, ".")
	if !found || !hex64.MatchString(hash) || len(ext) == 0 || len(ext) > 32 {
		return invalid()
	}
	for _, r := range ext {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return invalid()
		}
	}
	out.Hash, out.Extension = hash, ext
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return invalid()
	}
	if len(q["xs"]) > resourceMaxServers || len(q["as"]) > 8 {
		return invalid()
	}
	for _, hint := range q["xs"] {
		if !strings.Contains(hint, "://") {
			hint = "https://" + hint
		}
		server, err := url.Parse(hint)
		if err != nil || (server.Scheme != "https" && server.Scheme != "http") || server.Host == "" || server.User != nil || server.Path != "" && server.Path != "/" || server.RawQuery != "" || server.Fragment != "" {
			return invalid()
		}
		out.Servers = append(out.Servers, server.Scheme+"://"+server.Host)
	}
	for _, author := range q["as"] {
		if !hex64.MatchString(author) {
			return invalid()
		}
		out.Authors = append(out.Authors, author)
	}
	if sizes, ok := q["sz"]; ok {
		if len(sizes) != 1 {
			return invalid()
		}
		out.Size, err = strconv.ParseInt(sizes[0], 10, 64)
		if err != nil || out.Size <= 0 {
			return invalid()
		}
	}
	return out, nil
}
