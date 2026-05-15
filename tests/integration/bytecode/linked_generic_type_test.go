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
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/link"
)

func TestLinkedGenericTypeInstantiatesAgainstUserDefinedType(t *testing.T) {
	t.Parallel()

	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"example.com/synth": {
			"Container": reflect.ValueOf(link.WrapType("Container", 1, []link.GenericField{
				{
					Name:     "Item",
					Exported: true,
					FieldType: link.GenericFieldType{
						Kind:         link.FieldKindTypeArg,
						TypeArgIndex: 0,
					},
				},
				{
					Name:     "Count",
					Exported: true,
					FieldType: link.GenericFieldType{
						Kind:      link.FieldKindBasic,
						BasicKind: reflect.Int,
					},
				},
			})),
		},
	})
	symbols.SynthesiseAll()

	service := app.NewService()
	service.UseSymbols(symbols)

	source := `package main

import (
	"example.com/synth"
)

type Doc struct {
	Title string
}

func run() string {
	c := synth.Container[Doc]{
		Item:  Doc{Title: "hello"},
		Count: 42,
	}
	return c.Item.Title
}

func main() {}
`

	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, "hello", result)
}

func TestLinkedGenericTypeAsSliceElement(t *testing.T) {
	t.Parallel()

	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"example.com/synth": {
			"Result": reflect.ValueOf(link.WrapType("Result", 1, []link.GenericField{
				{
					Name:     "Item",
					Exported: true,
					FieldType: link.GenericFieldType{
						Kind:         link.FieldKindTypeArg,
						TypeArgIndex: 0,
					},
				},
				{
					Name:     "Score",
					Exported: true,
					FieldType: link.GenericFieldType{
						Kind:      link.FieldKindBasic,
						BasicKind: reflect.Float64,
					},
				},
			})),
		},
	})
	symbols.SynthesiseAll()

	service := app.NewService()
	service.UseSymbols(symbols)

	source := `package main

import (
	"example.com/synth"
)

type Doc struct {
	Title string
}

type Response struct {
	Results []synth.Result[Doc]
}

func run() string {
	resp := Response{
		Results: []synth.Result[Doc]{
			{Item: Doc{Title: "first"}, Score: 0.9},
			{Item: Doc{Title: "second"}, Score: 0.5},
		},
	}
	return resp.Results[0].Item.Title + "/" + resp.Results[1].Item.Title
}

func main() {}
`

	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, "first/second", result)
}
