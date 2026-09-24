module pipit.sh/pipit/sdk/stdlib

go 1.27.0

require (
	github.com/stretchr/testify v1.12.1
	golang.org/x/tools v0.50.0
	pipit.sh/pipit v0.0.0-alpha.1
)

require (
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.48.0 // indirect
	piko.sh/asmgen v0.2.0 // indirect
	piko.sh/vectormaths v0.2.0 // indirect
)

replace pipit.sh/pipit => ../..
