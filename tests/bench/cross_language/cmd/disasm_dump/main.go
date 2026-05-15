// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

// Command disasm_dump compiles one pipit source file and prints the assembly disassembly
// of the resulting program to stdout.
//
// Usage:
//
//	go run ./tests/bench/cmd/disasm_dump <pipit_source.go>
package main

import (
	"context"
	"fmt"
	"os"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func main() {
	source, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	service := pipit.NewInterpreter(stdlib.WithStandardLibrary())
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": string(source)})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s", pipit.DisassembleAssembly(compiled))
}
