module kwakore/desktop

go 1.26.2

require (
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1
	github.com/rs/zerolog v1.35.1
	golang.org/x/sys v0.48.0 // indirect
)

require (
	github.com/abemedia/go-webview v0.0.0-20250327021345-7b06ad397f16
	github.com/ebitengine/purego v0.8.2
)

require kwakore/backend v0.0.0

replace kwakore/backend => ../backend

replace github.com/wizenheimer/blaze => github.com/fiatjaf/blaze v0.0.0-20260928142946-49abb01b1a4b
