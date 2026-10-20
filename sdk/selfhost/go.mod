module pipit.sh/pipit/sdk/selfhost

go 1.27.0

require (
	golang.org/x/tools v0.50.0
	piko.sh/safeconv v0.1.0
	pipit.sh/pipit v0.0.0-alpha.8
	pipit.sh/pipit/sdk/stdlib v0.0.0-alpha.8
)

require (
	golang.org/x/sys v0.48.0 // indirect
	piko.sh/asmgen v0.2.0 // indirect
	piko.sh/vectormaths v0.2.0 // indirect
)

replace pipit.sh/pipit => ../..

replace pipit.sh/pipit/sdk/stdlib => ../stdlib
