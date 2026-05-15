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

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
)

// layoutTypeWordTable returns the per-typeTable-entry type words.
//
// Computes them on first use. The published slice is read through an atomic pointer so
// the common path is a single load.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the type table.
//
// Returns one pair per typeTable entry. Entries whose type is nil hold nil words.
func layoutTypeWordTable(compiledFunction *program.CompiledFunction) []program.LayoutTypeWordPair {
	if words := compiledFunction.LayoutTypeWords.Load(); words != nil {
		return *words
	}
	return buildLayoutTypeWordTable(compiledFunction)
}

// buildLayoutTypeWordTable computes and publishes the type-word cache exactly once.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the type table.
//
// Returns the published slice.
func buildLayoutTypeWordTable(compiledFunction *program.CompiledFunction) []program.LayoutTypeWordPair {
	compiledFunction.LayoutTypeWordsOnce.Do(func() {
		words := make(program.LayoutTypeWordSlice, len(compiledFunction.TypeTable))
		for index, entry := range compiledFunction.TypeTable {
			if entry == nil {
				continue
			}
			words[index] = program.LayoutTypeWordPair{
				Value:   reflectValueABIType(entry),
				Pointer: reflectValueABIType(reflect.PointerTo(entry)),
			}
		}
		compiledFunction.LayoutTypeWords.Store(&words)
	})
	return *compiledFunction.LayoutTypeWords.Load()
}
