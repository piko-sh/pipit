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

package selfhost_test

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine/program"
)

func (h *host) printListing(out io.Writer, name string) error {
	function := findFunction(h.compiled.Root(), name)
	if function == nil {
		return fmt.Errorf("no compiled function matches %q", name)
	}
	fmt.Fprintf(out, "%s (%s)\n", function.FunctionName(), function.RuntimeName)
	for line := range strings.SplitSeq(debug.DisassembleFunctionAssembly(function), "\n") {
		fmt.Fprintln(out, annotateListingLine(function, line))
	}
	return nil
}

func findFunction(root *program.CompiledFunction, name string) *program.CompiledFunction {
	if root.FunctionName() == name || root.RuntimeName == name || strings.HasSuffix(root.RuntimeName, "."+name) {
		return root
	}
	for _, child := range root.Functions {
		if found := findFunction(child, name); found != nil {
			return found
		}
	}
	return nil
}

func annotateListingLine(function *program.CompiledFunction, line string) string {
	const pcWidth = 4
	if len(line) <= pcWidth || line[pcWidth] != ' ' || function.DebugSourceMap == nil {
		return line
	}
	pc, err := strconv.Atoi(line[:pcWidth])
	if err != nil {
		return line
	}
	_, sourceLine, _ := function.DebugSourceMap.SourcePosition(pc)
	return fmt.Sprintf("L%-5d %s", sourceLine, line)
}
