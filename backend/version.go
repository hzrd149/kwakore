package backend

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Version is the release this build is, stamped by release builds with
// -ldflags "-X kwakore/backend.Version=v1.2.3". Empty for development builds.
var Version = ""

// SourceURL is where Verdana's source, releases and issues live.
const SourceURL = "https://github.com/hzrd149/verdana"

// aboutInfo is what the settings window's About page shows.
type aboutInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Modified bool   `json:"modified"`
	Platform string `json:"platform"`
	Go       string `json:"go"`
	Source   string `json:"source"`
}

func aboutVersion() aboutInfo {
	info := aboutInfo{
		Version:  strings.TrimSpace(Version),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Go:       runtime.Version(),
		Source:   SourceURL,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = s.Value
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
		// go install module@version stamps the main module's version
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
	}
	return info
}
