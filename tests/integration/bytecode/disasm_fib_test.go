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

//go:build integration

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
)

func TestDumpFibUintBytecode(t *testing.T) {
	const source = `
package main

const fibTermsToCompute = 100000
const fibModulusBitmask = (1 << 64) - 1

func computeFib(n int) uint64 {
	previous := uint64(0)
	current := uint64(1)
	for index := 0; index < n; index++ {
		next := (previous + current) & fibModulusBitmask
		previous = current
		current = next
	}
	return current
}

func EntrypointRun() uint64 {
	return computeFib(fibTermsToCompute)
}
`
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{
		"main.go": source,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dump := debug.DisassembleAssembly(compiled)
	t.Logf("\n=== UINT64 fib bytecode ===\n%s", dump)
}

func TestDumpFibIntBytecode(t *testing.T) {
	const source = `
package main

const fibTermsToCompute = 100000

func computeFibInt(n int) int64 {
	previous := int64(0)
	current := int64(1)
	for index := 0; index < n; index++ {
		next := previous + current
		previous = current
		current = next
	}
	return current
}

func EntrypointRun() int64 {
	return computeFibInt(fibTermsToCompute)
}
`
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{
		"main.go": source,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dump := debug.DisassembleAssembly(compiled)
	t.Logf("\n=== INT64 fib bytecode ===\n%s", dump)
}

func TestDumpArithIntBytecode(t *testing.T) {
	const source = `
package main

func EntrypointRun() int64 {
	var sum int64
	for index := int64(0); index < 5000000; index++ {
		sum = sum + index
	}
	return sum
}
`
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{
		"main.go": source,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dump := debug.DisassembleAssembly(compiled)
	t.Logf("\n=== Pure int64 arithmetic bytecode ===\n%s", dump)
}
