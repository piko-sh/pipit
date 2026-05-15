module pipit.sh/pipit/cmd/pipit

go 1.27.0

require (
	charm.land/bubbles/v2 v2.1.0
	charm.land/bubbletea/v2 v2.0.6
	charm.land/glamour/v2 v2.0.0
	charm.land/lipgloss/v2 v2.0.3
	github.com/alecthomas/chroma/v2 v2.24.1
	github.com/dolmen-go/modfs v0.0.0-20250307075130-a5a095088a2f
	github.com/google/go-dap v0.12.0
	github.com/mattn/go-isatty v0.0.20
	github.com/muesli/cancelreader v0.2.2
	github.com/rogpeppe/go-internal v1.14.1
	github.com/stretchr/testify v1.12.1
	golang.org/x/mod v0.41.0
	golang.org/x/sys v0.48.0
	pipit.sh/pipit v0.1.0-alpha
	pipit.sh/pipit/sdk/extract v0.1.0-alpha
	pipit.sh/pipit/sdk/stdlib v0.1.0-alpha
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
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
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yuin/goldmark v1.7.8 // indirect
	github.com/yuin/goldmark-emoji v1.0.5 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	piko.sh/asmgen v0.2.0 // indirect
	piko.sh/vectormaths v0.2.0 // indirect
)

replace pipit.sh/pipit/sdk/extract => ../../sdk/extract
