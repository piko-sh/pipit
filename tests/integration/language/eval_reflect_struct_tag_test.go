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

package language_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

func reflectingService(t *testing.T) *app.Service {
	t.Helper()
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"reflect": {
			"TypeOf":      reflect.ValueOf(reflect.TypeOf),
			"Type":        reflect.ValueOf((*reflect.Type)(nil)),
			"StructTag":   reflect.ValueOf((*reflect.StructTag)(nil)),
			"StructField": reflect.ValueOf((*reflect.StructField)(nil)),
		},
	})
	symbols.SynthesiseAll()
	service := app.NewService()
	service.UseSymbols(symbols)
	return service
}

func TestEvalReflectStructTagOnASelfReferentialType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		expect any
	}{
		{

			name: "a self-referential field reports no tag",
			source: `package main

import "reflect"

type node struct {
	N    int
	Next *node
}

func run() string {
	t := reflect.TypeOf(node{})
	out := ""
	for i := 0; i < t.NumField(); i++ {
		out += t.Field(i).Name + "=[" + string(t.Field(i).Tag) + "]"
	}
	return out
}

func main() {}
`,
			expect: "N=[]Next=[]",
		},
		{
			name: "a self-referential field keeps the tag the program wrote",
			source: `package main

import "reflect"

type node struct {
	N    int    ` + "`json:\"n\"`" + `
	Next *node  ` + "`json:\"next,omitempty\"`" + `
}

func run() string {
	t := reflect.TypeOf(node{})
	f, _ := t.FieldByName("Next")
	return f.Tag.Get("json") + "|" + string(f.Tag)
}

func main() {}
`,
			expect: "next,omitempty|json:\"next,omitempty\"",
		},
		{
			name: "a mutually recursive pair reports no tags either",
			source: `package main

import "reflect"

type left struct {
	R *right
}

type right struct {
	L *left
}

func run() string {
	lt := reflect.TypeOf(left{})
	rt := reflect.TypeOf(right{})
	return "[" + string(lt.Field(0).Tag) + "][" + string(rt.Field(0).Tag) + "]"
}

func main() {}
`,
			expect: "[][]",
		},
		{
			name: "a type with no cycle is unaffected",
			source: `package main

import "reflect"

type plain struct {
	A int ` + "`json:\"a\"`" + `
	B string
}

func run() string {
	t := reflect.TypeOf(plain{})
	return "[" + string(t.Field(0).Tag) + "][" + string(t.Field(1).Tag) + "]"
}

func main() {}
`,
			expect: "[json:\"a\"][]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := reflectingService(t).EvalFile(context.Background(), tt.source, "run")
			require.NoError(t, err, "source:\n%s", tt.source)
			require.Equal(t, tt.expect, result,
				"a program must see the struct tags it declared, and only those")
		})
	}
}
