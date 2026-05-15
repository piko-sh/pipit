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

package cli

import (
	"slices"

	"pipit.sh/pipit"
)

// functionEntry pairs an entrypoint name with its CompiledFunction.
type functionEntry struct {
	// compiled is the compiled bytecode representation.
	compiled *pipit.CompiledFunction

	// name is the function's declared name.
	name string
}

// eachFunction returns every named CompiledFunction inside cfs, sorted by name.
//
// Takes cfs (*CompiledFileSet) which supplies the compiled functions.
//
// Returns []functionEntry which pairs each name with its compiled function.
func eachFunction(cfs *pipit.CompiledFileSet) []functionEntry {
	names := cfs.FunctionNames()
	slices.Sort(names)
	entries := make([]functionEntry, 0, len(names))
	for _, name := range names {
		function, err := cfs.FindFunction(name)
		if err != nil {
			continue
		}
		entries = append(entries, functionEntry{name: name, compiled: function})
	}
	return entries
}

// compiledFunctions returns just the function names, sorted.
//
// Takes cfs (*CompiledFileSet) which supplies the compiled functions.
//
// Returns []string which holds the sorted function names.
func compiledFunctions(cfs *pipit.CompiledFileSet) []string {
	names := cfs.FunctionNames()
	slices.Sort(names)
	return names
}
