module pipit.sh/pipit/cmd/wasm

go 1.27.0

require (
	github.com/stretchr/testify v1.12.1
	pipit.sh/pipit v0.0.0-alpha.10
	pipit.sh/pipit/sdk/stdlib v0.0.0-alpha.10
)

require (
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	piko.sh/asmgen v0.5.0 // indirect
	piko.sh/safeconv v0.1.0 // indirect
	piko.sh/safedisk v0.3.0 // indirect
	piko.sh/stablepool v0.3.0 // indirect
	piko.sh/vectormaths v0.4.0 // indirect
)

replace pipit.sh/pipit => ../..

replace pipit.sh/pipit/sdk/stdlib => ../../sdk/stdlib
