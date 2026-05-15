module pipit.sh/pipit/examples/webserver

go 1.27.0

require (
	golang.org/x/tools v0.50.0
	pipit.sh/pipit/cmd/pipit v0.0.0
)

require (
	charm.land/bubbles/v2 v2.1.0 // indirect
	charm.land/bubbletea/v2 v2.0.6 // indirect
	charm.land/glamour/v2 v2.0.0 // indirect
	charm.land/lipgloss/v2 v2.0.3 // indirect
	github.com/alecthomas/chroma/v2 v2.24.1 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260416155717-489999b90468 // indirect
	github.com/charmbracelet/x/ansi v0.11.7 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dolmen-go/modfs v0.0.0-20250307075130-a5a095088a2f // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-dap v0.12.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yuin/goldmark v1.7.8 // indirect
	github.com/yuin/goldmark-emoji v1.0.5 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	piko.sh/asmgen v0.2.0 // indirect
	piko.sh/goastutil v0.1.0 // indirect
	piko.sh/vectormaths v0.2.0 // indirect
	pipit.sh/pipit v0.1.0-alpha // indirect
	pipit.sh/pipit/sdk/extract v0.1.0-alpha // indirect
	pipit.sh/pipit/sdk/stdlib v0.1.0-alpha // indirect
)

replace (
	pipit.sh/pipit => ../..
	pipit.sh/pipit/cmd/pipit => ../../cmd/pipit
	pipit.sh/pipit/sdk/extract => ../../sdk/extract
	pipit.sh/pipit/sdk/stdlib => ../../sdk/stdlib
)
