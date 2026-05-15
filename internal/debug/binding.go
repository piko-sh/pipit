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

package debug

import (
	"context"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"sort"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

// PackageBinding is the compile-time information about one package of a bound program
// that the evaluator needs: its type-checked package, function table and global slots.
type PackageBinding struct {
	// Package is the type-checked package.
	Package *types.Package

	// FunctionTable maps function names to their index in the program's function list.
	FunctionTable map[string]uint16

	// GlobalVariables maps package-level variable names to their store slots.
	GlobalVariables map[string]program.GlobalVariableInfo

	// Path is the import path, empty for the main package.
	Path string
}

// ProgramBinding is what a service hands the debugger after compiling a program, so that
// expressions can be type-checked and compiled against the paused program and breakpoint
// lines can be verified against its source maps.
type ProgramBinding struct {
	// FileSet is the token file set the program was parsed with.
	FileSet *token.FileSet

	// Compiled is the compiled program.
	Compiled *program.CompiledFileSet

	// Symbols is the symbol registry the program resolves host symbols through.
	Symbols *symtab.SymbolRegistry

	// Globals is the global store the program's package-level variables live in.
	Globals *engine.GlobalStore

	// lines caches, per source file, the sorted set of lines that carry an instruction.
	lines map[string][]int

	// evaluator compiles and runs expressions against this binding.
	evaluator *evaluator

	// Packages lists the program's packages.
	Packages []PackageBinding

	// Compiler is the compiler configuration template the program was compiled with; its
	// per-function fields are unset.
	Compiler compile.CompilerConfig

	// Limits are the service's limits, used for the scratch VM that evaluates expressions.
	Limits engine.VMLimits
}

// BindProgram records the program information the evaluator and breakpoint verifier need.
// A later binding replaces an earlier one.
//
// Takes binding (*ProgramBinding) which describes the compiled program; the debugger
// keeps its own copy.
//
// Concurrency: safe for concurrent use; acquires d.mu.
func (d *Debugger) BindProgram(binding *ProgramBinding) {
	stored := new(ProgramBinding)
	*stored = *binding
	stored.lines = collectSourceLines(binding.Compiled)
	stored.evaluator = newEvaluator(stored)
	d.session.SetConditionEvaluator(stored.evaluator.condition)
	d.mu.Lock()
	d.binding = stored
	d.mu.Unlock()
}

// resolveLine finds the first line at or after line in file that carries an instruction.
//
// Takes file (string) which is the source file.
// Takes line (int) which is the requested 1-based line.
//
// Returns int which is the resolved line.
// Returns bool which is false when the file is unknown or no later line carries code.
func (b *ProgramBinding) resolveLine(file string, line int) (int, bool) {
	lines, ok := b.lines[file]
	if !ok {
		return line, true
	}
	index := sort.SearchInts(lines, line)
	if index >= len(lines) {
		return line, false
	}
	return lines[index], true
}

// packageAt returns the package binding the frame's function was compiled in.
//
// Takes frame (engine.DebugFrame) which locates the function.
//
// Returns *PackageBinding which is nil when the position is not in a bound package.
func (b *ProgramBinding) packageAt(frame engine.DebugFrame) *PackageBinding {
	if frame.Function == nil || b.FileSet == nil {
		return nil
	}
	position := b.positionOf(frame.File, frame.Line, frame.Column)
	for index := range b.Packages {
		pkg := &b.Packages[index]
		if pkg.Package == nil {
			continue
		}
		if position.IsValid() && scopeContains(pkg.Package.Scope(), position) {
			return pkg
		}
	}
	if len(b.Packages) == 1 {
		return &b.Packages[0]
	}
	return nil
}

// positionOf converts a file, line and column into a token.Pos of the binding's file set.
//
// Takes file (string) which is the source file.
// Takes line (int) which is the 1-based line.
// Takes column (int) which is the 1-based column; zero means the start of the line.
//
// Returns token.Pos which is token.NoPos when the file or line is unknown.
func (b *ProgramBinding) positionOf(file string, line int, column int) token.Pos {
	var found *token.File
	b.FileSet.Iterate(func(candidate *token.File) bool {
		if candidate.Name() == file {
			found = candidate
			return false
		}
		return true
	})
	if found == nil || line <= 0 || line > found.LineCount() {
		return token.NoPos
	}
	position := found.LineStart(line)
	if column > 1 {
		position += token.Pos(column - 1)
	}
	return position
}

// scopeContains reports whether any file scope of a package scope contains pos.
//
// Takes scope (*types.Scope) which is the package scope.
// Takes pos (token.Pos) which is the position.
//
// Returns bool which is true when a file scope contains pos.
func scopeContains(scope *types.Scope, pos token.Pos) bool {
	for child := range scope.Children() {
		if child.Contains(pos) {
			return true
		}
	}
	return false
}

// collectSourceLines gathers, per file, the lines that carry an instruction in any
// function of the program.
//
// Takes compiled (*program.CompiledFileSet) which may be nil.
//
// Returns map[string][]int which maps a file to its sorted code lines.
func collectSourceLines(compiled *program.CompiledFileSet) map[string][]int {
	if compiled == nil || compiled.Root() == nil {
		return nil
	}
	lines := make(map[string]map[int]struct{})
	for _, function := range compiled.Root().Functions {
		addFunctionLines(lines, function)
	}
	sorted := make(map[string][]int, len(lines))
	for file, set := range lines {
		list := make([]int, 0, len(set))
		for line := range set {
			list = append(list, line)
		}
		slices.Sort(list)
		sorted[file] = list
	}
	return sorted
}

// addFunctionLines records the lines of one function that carry an instruction.
//
// Takes lines (map[string]map[int]struct{}) which collects lines per file.
// Takes function (*program.CompiledFunction) which may be nil or lack a source map.
func addFunctionLines(lines map[string]map[int]struct{}, function *program.CompiledFunction) {
	if function == nil || function.DebugSourceMap == nil {
		return
	}
	for pc := range function.Body {
		file, line, _ := function.DebugSourceMap.SourcePosition(pc)
		if line == 0 {
			continue
		}
		if lines[file] == nil {
			lines[file] = make(map[int]struct{})
		}
		lines[file][line] = struct{}{}
	}
}

// globalVariables reads the package-level variables of the frame's package.
//
// Takes thread (*engine.DebugThread) which is paused.
// Takes frame (engine.DebugFrame) which locates the package.
//
// Returns []VariableInfo which is sorted by name.
// Returns error which is fault.ErrNoProgramBinding without a binding for the frame.
func (d *Debugger) globalVariables(thread *engine.DebugThread, frame engine.DebugFrame) ([]VariableInfo, error) {
	binding := d.currentBinding()
	if binding == nil {
		return nil, fault.ErrNoProgramBinding
	}
	pkg := binding.packageAt(frame)
	if pkg == nil {
		return nil, fault.ErrNoProgramBinding
	}
	names := make([]string, 0, len(pkg.GlobalVariables))
	for name := range pkg.GlobalVariables {
		names = append(names, name)
	}
	slices.Sort(names)
	globals := thread.VM().Globals
	variables := make([]VariableInfo, 0, len(names))
	for _, name := range names {
		slot := pkg.GlobalVariables[name]
		value, ok := globals.SnapshotVar(slot)
		var boxed any
		if ok && value.IsValid() {
			boxed = engine.MaterialiseArenaValue(thread.VM().Arena, value).Interface()
		}
		variables = append(variables, VariableInfo{Name: name, Value: boxed, Kind: slot.Kind.String(), Type: globalTypeName(pkg, name)})
	}
	return variables, nil
}

// globalTypeName returns the declared type of a package-level variable.
//
// Takes pkg (*PackageBinding) which is the package binding.
// Takes name (string) which is the variable name.
//
// Returns string which is the type as source text, empty when unknown.
func globalTypeName(pkg *PackageBinding, name string) string {
	if pkg.Package == nil {
		return ""
	}
	object := pkg.Package.Scope().Lookup(name)
	if object == nil {
		return ""
	}
	return types.TypeString(object.Type(), types.RelativeTo(pkg.Package))
}

// typeNameFor returns the declared type of a local visible at the frame's position, or
// the dynamic type of its value when the program is not bound.
//
// Takes frame (engine.DebugFrame) which locates the position.
// Takes name (string) which is the variable name.
// Takes value (reflect.Value) which is the variable's current value.
//
// Returns string which is the type as source text, empty when unknown.
func (d *Debugger) typeNameFor(frame engine.DebugFrame, name string, value reflect.Value) string {
	if binding := d.currentBinding(); binding != nil {
		if typeName := binding.localTypeName(frame, name); typeName != "" {
			return typeName
		}
	}
	if value.IsValid() {
		return value.Type().String()
	}
	return ""
}

// localTypeName looks a local up in the innermost scope at the frame's position.
//
// Takes frame (engine.DebugFrame) which locates the position.
// Takes name (string) which is the variable name.
//
// Returns string which is the type as source text, empty when not found.
func (b *ProgramBinding) localTypeName(frame engine.DebugFrame, name string) string {
	pkg := b.packageAt(frame)
	if pkg == nil || pkg.Package == nil {
		return ""
	}
	position := b.positionOf(frame.File, frame.Line, frame.Column)
	if !position.IsValid() {
		return ""
	}
	inner := pkg.Package.Scope().Innermost(position)
	if inner == nil {
		return ""
	}
	_, object := inner.LookupParent(name, position)
	if object == nil {
		return ""
	}
	return types.TypeString(object.Type(), types.RelativeTo(pkg.Package))
}

// evaluate runs an expression against a paused frame through the binding's evaluator.
//
// Takes vm (*engine.VM) which is the paused VM.
// Takes frame (engine.DebugFrame) which is the frame's source position.
// Takes callFrame (*engine.CallFrame) which holds the frame's variables.
// Takes expression (string) which is the Go expression.
// Takes opts (EvalOptions) which tune the evaluation.
//
// Returns EvalResult which is the value and its type.
// Returns error when the expression fails to compile or run.
func (b *ProgramBinding) evaluate(ctx context.Context, vm *engine.VM, frame engine.DebugFrame, callFrame *engine.CallFrame, expression string, opts EvalOptions) (EvalResult, error) {
	return b.evaluator.evaluate(ctx, vm, frame, callFrame, expression, opts)
}
