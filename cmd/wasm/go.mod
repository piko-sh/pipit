module pipit.sh/pipit/cmd/wasm

go 1.27.0

require (
	pipit.sh/pipit v0.0.0-alpha.1
	pipit.sh/pipit/sdk/stdlib v0.0.0-alpha.1
)

require (
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	piko.sh/asmgen v0.2.0 // indirect
	piko.sh/vectormaths v0.2.0 // indirect
)

replace pipit.sh/pipit => ../..

replace pipit.sh/pipit/sdk/stdlib => ../../sdk/stdlib
