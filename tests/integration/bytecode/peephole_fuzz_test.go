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

//go:build integration && fuzz

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

func FuzzGvnDoesNotPanic(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4})
	f.Add([]byte{})
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, raw []byte) {
		body := decodeFuzzBody(raw)
		compiledFunction := &program.CompiledFunction{Body: body}
		_ = passes.RunFunctionGvn(context.Background(), compiledFunction)
		if len(compiledFunction.Body) != len(body) {
			t.Fatalf("GVN changed body length: got %d, want %d", len(compiledFunction.Body), len(body))
		}
	})
}

func FuzzBceDoesNotPanic(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4})
	f.Add([]byte{})
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, raw []byte) {
		body := decodeFuzzBody(raw)
		compiledFunction := &program.CompiledFunction{Body: body}
		passes.ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)
		if len(compiledFunction.Body) != len(body) {
			t.Fatalf("BCE changed body length: got %d, want %d", len(compiledFunction.Body), len(body))
		}
	})
}

func FuzzPointerAliasDoesNotPanic(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4})
	f.Add([]byte{})
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, raw []byte) {
		body := decodeFuzzBody(raw)
		compiledFunction := &program.CompiledFunction{Body: body}
		original := append([]isa.Instruction(nil), compiledFunction.Body...)
		_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
		if len(compiledFunction.Body) != len(original) {
			t.Fatalf("alias analysis mutated body length: got %d, want %d", len(compiledFunction.Body), len(original))
		}
		for i := range compiledFunction.Body {
			if compiledFunction.Body[i] != original[i] {
				t.Fatalf("alias analysis mutated body[%d]: got %#v, want %#v", i, compiledFunction.Body[i], original[i])
			}
		}
	})
}

func decodeFuzzBody(raw []byte) []isa.Instruction {
	const wordSize = 4
	const maxWords = 1024
	count := min(len(raw)/wordSize, maxWords)
	body := make([]isa.Instruction, count)
	for i := range count {
		base := i * wordSize
		body[i] = isa.Instruction{
			Op: isa.Opcode(raw[base]),
			A:  raw[base+1],
			B:  raw[base+2],
			C:  raw[base+3],
		}
	}
	return body
}
