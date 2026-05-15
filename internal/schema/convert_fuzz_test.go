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

//go:build fuzz

package schema_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/schema"
)

func FuzzConvertBytecode(f *testing.F) {
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": "package main\n\nfunc helper(n int) int {\n\treturn n + 1\n}\n\nfunc main() {\n\tx := helper(1)\n\t_ = x\n}\n"})
	if err != nil {
		f.Fatalf("compiling seed program: %v", err)
	}
	seed, err := schema.Unpack(adapters.PackCompiledFileSetToBytes(compiled))
	if err != nil {
		f.Fatalf("unpacking seed payload: %v", err)
	}
	f.Add(seed)
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	for cut := 1; cut < len(seed); cut += 64 {
		f.Add(seed[:cut])
	}
	for position := 0; position < len(seed); position += 37 {
		flipped := append([]byte(nil), seed...)
		flipped[position] ^= 0x80
		f.Add(flipped)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("ConvertBytecode panicked: %v", recovered)
			}
		}()
		_, _ = schema.ConvertBytecode(payload)
	})
}
