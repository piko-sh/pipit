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

package checker

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

var fixtureConfig = &checkerConfig{
	UnsafeAllowed: map[string]string{"unsafewrites.allowed": "fixture: reviewed"},
	Structs: map[string]structRule{
		"structfields.Closure": {Rule: "closure-fields", Why: "fixture", Fields: map[string]string{"cells": "cells", "function": "code"}},
		"structfields.Gone":    {Rule: "closure-fields", Why: "fixture", Fields: map[string]string{"kept": "scalar", "removed": "scalar"}},
		"structfields.Embeds":  {Rule: "closure-fields", Why: "fixture", Fields: map[string]string{"Base": "embedded", "Mutex": "scalar", "List": "embedded", "Pair": "embedded"}},
	},
	Exhaustive: exhaustiveRule{
		Types:           []string{"exh.Kind"},
		AllowedDefaults: map[string]string{"exh.justified": "fixture: the default refuses"},
	},
	Handlers: handlerRule{
		Packages: []string{"handlers"},
		Prefix:   "handle",
		Barriers: []string{"handlers.barrier"},
		Allowed: map[string]allowedHandler{
			"handlers.handleReviewed":     {Stores: 1, Why: "fixture"},
			"handlers.handleReviewedGrew": {Stores: 1, Why: "fixture: grew since review"},
		},
	},
}

func TestRulesReportTheirFixtures(t *testing.T) {
	testConfig = fixtureConfig
	t.Cleanup(func() { testConfig = nil })
	directory := analysistest.TestData()
	analysistest.Run(t, directory, unsafeAnalyser, "unsafewrites")
	analysistest.Run(t, directory, structAnalyser, "structfields")
	analysistest.Run(t, directory, exhaustiveAnalyser, "exh", "exhuser")
	analysistest.Run(t, directory, handlerAnalyser, "handlers", "plain")
}

func TestAnalysersListsEveryRule(t *testing.T) {
	names := make([]string, 0, 4)
	for _, analyser := range Analysers() {
		names = append(names, analyser.Name)
	}
	require.Equal(t, []string{unsafeAnalyser.Name, structAnalyser.Name, exhaustiveAnalyser.Name, handlerAnalyser.Name}, names)
}

func TestCarriesReferences(t *testing.T) {
	pointer := types.NewPointer(types.Typ[types.Int])
	field := func(name string, fieldType types.Type) *types.Var {
		return types.NewField(0, nil, name, fieldType, false)
	}
	deep := types.Type(types.Typ[types.Int])
	for range maxReferenceDepth + 2 {
		deep = types.NewArray(deep, 1)
	}
	cases := []struct {
		valueType types.Type
		name      string
		want      bool
	}{
		{name: "no type", valueType: nil, want: false},
		{name: "int", valueType: types.Typ[types.Int], want: false},
		{name: "string", valueType: types.Typ[types.String], want: true},
		{name: "unsafe pointer", valueType: types.Typ[types.UnsafePointer], want: true},
		{name: "pointer", valueType: pointer, want: true},
		{name: "array of ints", valueType: types.NewArray(types.Typ[types.Int], 2), want: false},
		{name: "array of pointers", valueType: types.NewArray(pointer, 2), want: true},
		{name: "struct of scalars", valueType: types.NewStruct([]*types.Var{field("a", types.Typ[types.Int])}, nil), want: false},
		{name: "struct holding a pointer", valueType: types.NewStruct([]*types.Var{field("a", types.Typ[types.Int]), field("b", pointer)}, nil), want: true},
		{name: "nesting past the depth bound counts as references", valueType: deep, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, carriesReferences(testCase.valueType, 0))
		})
	}
}
