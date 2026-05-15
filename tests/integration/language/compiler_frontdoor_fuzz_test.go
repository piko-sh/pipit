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

package language_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
)

func FuzzCompileFrontDoor(f *testing.F) {
	f.Add("package main\nfunc run() int { return 1 }\n")
	f.Add("package main\nfunc run() any { s := []any{1}; return s[0] }\n")
	f.Add("package main\ntype C float64\nfunc (c C) U() string { return \"c\" }\nfunc run() string { return C(1).U() }\n")
	f.Add("package main\nfunc run() int { var i int; for i = 0; i < 4; i++ {}; return i }\n")
	f.Add("package main\nfunc run() int { type T struct{ B struct{ X int } }; var t T; t.B.X++; return t.B.X }\n")
	f.Add("package main")
	f.Add("")
	f.Add("package main\nfunc run(")
	f.Add("package main\nimport \"fmt\"\nfunc run() { fmt.Println(1) }\n")

	f.Fuzz(func(t *testing.T, source string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("compiler front door panicked on input %q: %v", source, r)
			}
		}()

		service := app.NewService()
		_, _ = service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	})
}
