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

//go:build fuzz && (!js || !wasm)

package adapters

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/schema"

	"pipit.sh/pipit/internal/symtab"
)

func FuzzLoadCompiledFromBytes(f *testing.F) {
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": "package main\n\nfunc helper(n int) int {\n\treturn n + 1\n}\n\nfunc main() {\n\tx := helper(1)\n\t_ = x\n}\n"})
	if err != nil {
		f.Fatalf("compiling seed program: %v", err)
	}
	seed := PackCompiledFileSetToBytes(compiled)
	raw, err := schema.Unpack(seed)
	if err != nil {
		f.Fatalf("unpacking seed program: %v", err)
	}
	f.Add([]byte{})
	addBytecodeFuzzSeed(f, seed)
	addBytecodeFuzzSeed(f, raw)
	for _, source := range []string{
		"package main; func main() { values := [3]int{1, 2, 3}; println(values[1]) }",
		"package main; type item struct { Name string; Count int }; func main() { value := item{Name: \"pipit\", Count: 2}; println(value.Count) }",
		"package main; func main() { values := map[string]int{\"key\": 2}; println(values[\"key\"]) }",
		"package main; func main() { count := 2; next := func() int { count++; return count }; println(next()) }",
	} {
		compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		if err != nil {
			f.Fatalf("compiling descriptor seed: %v", err)
		}
		encoded := PackCompiledFileSetToBytes(compiled)
		payload, err := schema.Unpack(encoded)
		if err != nil {
			f.Fatalf("unpacking descriptor seed: %v", err)
		}
		f.Add(encoded)
		f.Add(payload)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("bytecode decode panicked: %v", recovered)
			}
		}()
		registry := symtab.NewSymbolRegistry(nil)
		loaded, err := LoadCompiledFromBytes(data, registry)
		if err != nil && loaded != nil {
			t.Fatal("versioned decode returned partial bytecode on failure")
		}
		loaded, err = decodeCompiledFileSet(context.Background(), data, registry)
		if err != nil && loaded != nil {
			t.Fatal("raw decode returned partial bytecode on failure")
		}
	})
}

func addBytecodeFuzzSeed(f *testing.F, seed []byte) {
	f.Helper()
	f.Add(seed)
	for cut := 1; cut < len(seed); cut += 64 {
		f.Add(seed[:cut])
	}
	for position := 0; position < len(seed); position += 37 {
		flipped := append([]byte(nil), seed...)
		flipped[position] ^= 0x80
		f.Add(flipped)
	}
}
