module github.com/nmcginn/mtga-draft-assistant

// go-gui requires 1.26; this is also the minimum toolchain (decision 2).
go 1.26.0

// The exact toolchain `make check` and CI build, test and lint with, so a
// result on a laptop is a result in CI (decision 2). `go` downloads it on demand.
toolchain go1.26.9

require (
	github.com/go-gui-org/go-gui v0.86.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/dlclark/regexp2/v2 v2.2.2 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-gui-org/go-glyph v1.26.2 // indirect
	github.com/go-pdf/fpdf v0.9.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jezek/xgb v1.3.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tdewolff/parse/v2 v2.8.16 // indirect
	github.com/yuin/goldmark v1.8.5 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
