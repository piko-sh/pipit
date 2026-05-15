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
	"fmt"
	"strings"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

const (
	// pkasmSeparatorWidth is the width of the separator line in function headers.
	pkasmSeparatorWidth = 61

	// pkasmIndentUnit is the indentation string per nesting level.
	pkasmIndentUnit = "  "

	// pkasmConstantSeparator separates entries in constant pool dumps.
	pkasmConstantSeparator = "  "
)

// pkasmWriter writes human-readable bytecode assembly to a string builder. It tracks
// indentation depth for nested function output.
type pkasmWriter struct {
	// builder accumulates the assembly output.
	builder *strings.Builder

	// indentLevel tracks the current nesting depth.
	indentLevel int
}

// writeFunctionRecursive writes a function block and then recurses into its child
// functions with increased indentation.
//
// Takes compiledFunction (*CompiledFunction) which is the function to write.
func (w *pkasmWriter) writeFunctionRecursive(compiledFunction *program.CompiledFunction) {
	name := compiledFunction.Name
	if name == "" {
		name = "<anonymous>"
	}
	w.writeFunction(compiledFunction, name)

	w.indentLevel++
	for _, child := range compiledFunction.Functions {
		w.writeFunctionRecursive(child)
	}
	w.indentLevel--
}

// writeFunction writes a single function block: header, constant pools, and instruction
// listing.
//
// Takes compiledFunction (*CompiledFunction) which is the function to write.
// Takes name (string) which is the display name for the header.
func (w *pkasmWriter) writeFunction(compiledFunction *program.CompiledFunction, name string) {
	w.writeLine("")
	w.writeFunctionHeader(compiledFunction, name)
	w.writeConstantPools(compiledFunction)
	w.writeInstructions(compiledFunction)
}

// writeFunctionHeader writes the decorated function header showing name, register counts,
// parameter kinds, return kinds, and variadic flag.
//
// Takes compiledFunction (*CompiledFunction) which provides the metadata.
// Takes name (string) which is the display name for the header.
func (w *pkasmWriter) writeFunctionHeader(compiledFunction *program.CompiledFunction, name string) {
	separator := "; " + strings.Repeat("═", pkasmSeparatorWidth)
	w.writeLine(separator)
	w.writeLine(fmt.Sprintf("; function %s", name))

	if compiledFunction.SourceFile != "" {
		w.writeLine(fmt.Sprintf(";   source:    %s", compiledFunction.SourceFile))
	}

	regParts := formatRegisterCounts(compiledFunction)
	if len(regParts) > 0 {
		w.writeLine(fmt.Sprintf(";   registers: %s", strings.Join(regParts, " ")))
	}

	paramStr := formatKindList(compiledFunction.ParameterKinds)
	w.writeLine(fmt.Sprintf(";   params:    %s", paramStr))

	returnStr := formatKindList(compiledFunction.ResultKinds)
	w.writeLine(fmt.Sprintf(";   returns:   %s", returnStr))

	if compiledFunction.IsVariadic {
		w.writeLine(";   variadic:  true")
	}

	w.writeLine(separator)
}

// writeConstantPools dumps non-empty constant pools compactly.
//
// Takes compiledFunction (*CompiledFunction) which provides the constant pools.
func (w *pkasmWriter) writeConstantPools(compiledFunction *program.CompiledFunction) {
	hasAny := len(compiledFunction.IntConstants) > 0 || len(compiledFunction.FloatConstants) > 0 ||
		len(compiledFunction.StringConstants) > 0 || len(compiledFunction.BoolConstants) > 0 ||
		len(compiledFunction.UintConstants) > 0 || len(compiledFunction.ComplexConstants) > 0

	if !hasAny {
		return
	}

	w.writeLine("")
	w.writeLine("; constants:")

	if len(compiledFunction.IntConstants) > 0 {
		w.writeLine(fmt.Sprintf(";   ints:    %s", formatIntConstants(compiledFunction.IntConstants)))
	}
	if len(compiledFunction.FloatConstants) > 0 {
		w.writeLine(fmt.Sprintf(";   floats:  %s", formatFloatConstants(compiledFunction.FloatConstants)))
	}
	if len(compiledFunction.StringConstants) > 0 {
		w.writeLine(fmt.Sprintf(";   strings: %s", formatStringConstants(compiledFunction.StringConstants)))
	}
	if len(compiledFunction.BoolConstants) > 0 {
		w.writeLine(fmt.Sprintf(";   bools:   %s", formatBoolConstants(compiledFunction.BoolConstants)))
	}
	if len(compiledFunction.UintConstants) > 0 {
		w.writeLine(fmt.Sprintf(";   uints:   %s", formatUintConstants(compiledFunction.UintConstants)))
	}
	if len(compiledFunction.ComplexConstants) > 0 {
		w.writeLine(fmt.Sprintf(";   complex: %s", formatComplexConstants(compiledFunction.ComplexConstants)))
	}
}

// writeInstructions writes the instruction listing with source line annotations and
// enhanced call comments.
//
// Takes compiledFunction (*CompiledFunction) which provides the instruction body.
func (w *pkasmWriter) writeInstructions(compiledFunction *program.CompiledFunction) {
	if len(compiledFunction.Body) == 0 {
		return
	}

	w.writeLine("")

	var lastFile string
	var lastLine int

	for pc := range compiledFunction.Body {
		lastFile, lastLine = w.writeSourceAnnotation(compiledFunction, pc, lastFile, lastLine)
		w.writeInstruction(compiledFunction, pc)
	}
}

// writeSourceAnnotation emits a source line comment when the source position changes.
//
// Takes compiledFunction (*CompiledFunction) which provides the source map.
// Takes pc (int) which is the program counter to annotate.
// Takes lastFile (string) which is the previous source file.
// Takes lastLine (int) which is the previous source line.
//
// Returns the updated file and line trackers.
func (w *pkasmWriter) writeSourceAnnotation(compiledFunction *program.CompiledFunction, pc int, lastFile string, lastLine int) (string, int) {
	if compiledFunction.DebugSourceMap == nil || compiledFunction.DebugVarTable == nil {
		return lastFile, lastLine
	}
	file, line, _ := compiledFunction.DebugSourceMap.SourcePosition(pc)
	if line <= 0 || (file == lastFile && line == lastLine) {
		return lastFile, lastLine
	}
	if file != lastFile {
		w.writeLineRaw(w.indent() + fmt.Sprintf("%52s; %s:%d", "", file, line))
	} else {
		w.writeLineRaw(w.indent() + fmt.Sprintf("%52s; :%d", "", line))
	}
	return file, line
}

// writeInstruction writes a single instruction line with optional inline comment.
//
// Takes compiledFunction (*CompiledFunction) which provides the instruction body.
// Takes pc (int) which is the program counter of the instruction.
func (w *pkasmWriter) writeInstruction(compiledFunction *program.CompiledFunction, pc int) {
	instr := compiledFunction.Body[pc]
	label := isa.InstructionDisplayName(instr)
	comment := pkasmComment(compiledFunction, pc, instr)
	if comment != "" {
		w.writeLine(fmt.Sprintf("%04d  %-26s %3d %3d %3d    ; %s",
			pc, label, instr.A, instr.B, instr.C, comment))
	} else {
		w.writeLine(fmt.Sprintf("%04d  %-26s %3d %3d %3d",
			pc, label, instr.A, instr.B, instr.C))
	}
}

// writeLine writes an indented line to the builder.
//
// Takes line (string) which is the content to write.
func (w *pkasmWriter) writeLine(line string) {
	w.builder.WriteString(w.indent())
	w.builder.WriteString(line)
	w.builder.WriteByte('\n')
}

// writeLineRaw writes a line to the builder without indentation. The caller adds any
// prefix.
//
// Takes line (string) which is the content to write.
func (w *pkasmWriter) writeLineRaw(line string) {
	w.builder.WriteString(line)
	w.builder.WriteByte('\n')
}

// indent returns the indentation prefix for the current level (2 spaces per level).
//
// Returns string containing the indentation whitespace.
func (w *pkasmWriter) indent() string {
	if w.indentLevel <= 0 {
		return ""
	}
	return strings.Repeat(pkasmIndentUnit, w.indentLevel)
}

// DisassembleAssembly returns the complete human-readable bytecode assembly listing for
// the compiled file set. The output includes a file header, the root function body (if
// any), the variable init function (if any), and all child functions recursively.
//
// Takes cfs (*program.CompiledFileSet) which is the compiled file set to disassemble.
//
// Returns the assembly listing as a string.
func DisassembleAssembly(cfs *program.CompiledFileSet) string {
	w := &pkasmWriter{builder: &strings.Builder{}, indentLevel: 0}

	w.writeLine("; pkasm - pipit bytecode assembly")
	w.writeLine("")

	root := cfs.Root()
	if root == nil {
		return w.builder.String()
	}

	if len(root.Body) > 0 {
		w.writeFunction(root, "<root>")
	}

	if cfs.VariableInitFunction() != nil {
		w.writeFunction(cfs.VariableInitFunction(), "<varinit>")
	}

	for _, child := range root.Functions {
		w.writeFunctionRecursive(child)
	}

	return w.builder.String()
}

// DisassembleFunctionAssembly returns the human-readable bytecode assembly listing for
// the function and all its nested children.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to
// disassemble.
//
// Returns the assembly listing as a string.
func DisassembleFunctionAssembly(compiledFunction *program.CompiledFunction) string {
	w := &pkasmWriter{builder: &strings.Builder{}, indentLevel: 0}
	w.writeFunctionRecursive(compiledFunction)
	return w.builder.String()
}

// pkasmComment returns an inline comment for an instruction at pc, trying peephole
// provenance first, then call target resolution, then the generic shape comment.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// disassembled.
// Takes pc (int) which is the instruction's program counter.
// Takes instr (isa.Instruction) which is the instruction to comment.
//
// Returns string containing the comment, or empty if none applies.
func pkasmComment(compiledFunction *program.CompiledFunction, pc int, instr isa.Instruction) string {
	if comment := passes.FormatPeepholeAnnotation(passes.PeepholeAnnotationAt(compiledFunction, pc)); comment != "" {
		return comment
	}
	if comment := pkasmCallComment(compiledFunction, instr); comment != "" {
		return comment
	}
	return compiledFunction.DisassembleComment(instr)
}

// pkasmCallComment resolves call instructions to their target names.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// disassembled.
// Takes instr (isa.Instruction) which is the instruction to inspect.
//
// Returns string containing the resolved call comment, or empty if the instruction is not
// a call.
func pkasmCallComment(compiledFunction *program.CompiledFunction, instr isa.Instruction) string {
	if instr.Op == isa.OpDrillTier1 {
		return pkasmTier1CallComment(compiledFunction, instr)
	}
	switch instr.Op {
	case isa.OpMakeClosure:
		functionIndex := int(instr.WideIndex())
		if functionIndex < len(compiledFunction.Functions) {
			name := compiledFunction.Functions[functionIndex].Name
			if name == "" {
				name = "<anonymous>"
			}
			return fmt.Sprintf("closure %s (func %d)", name, functionIndex)
		}
		return fmt.Sprintf("closure (func %d)", functionIndex)
	default:
	}
	return ""
}

// resolveCallTarget resolves a CALL/TAIL_CALL/CALL_IIFE instruction to the target
// function name via callSites.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// disassembled.
// Takes instr (isa.Instruction) which is the call instruction.
// Takes label (string) which is the call type label for output.
//
// Returns string containing the resolved target description.
func resolveCallTarget(compiledFunction *program.CompiledFunction, instr isa.Instruction, label string) string {
	siteIndex := int(instr.WideIndex())
	if siteIndex >= len(compiledFunction.CallSites) {
		return fmt.Sprintf("%s (site %d)", label, siteIndex)
	}
	site := compiledFunction.CallSites[siteIndex]
	if site.IsClosure {
		return fmt.Sprintf("%s closure general[%d] (site %d)", label, site.ClosureRegister, siteIndex)
	}
	functionIndex := int(site.FunctionIndex)
	if functionIndex < len(compiledFunction.Functions) {
		name := compiledFunction.Functions[functionIndex].Name
		if name == "" {
			name = "<anonymous>"
		}
		return fmt.Sprintf("%s %s (site %d)", label, name, siteIndex)
	}
	return fmt.Sprintf("%s (site %d)", label, siteIndex)
}

// formatRegisterCounts returns a slice of "kind=N" strings for non-zero register banks.
//
// Takes compiledFunction (*CompiledFunction) which provides register counts.
//
// Returns []string containing one "kind=N" entry per non-zero register bank.
func formatRegisterCounts(compiledFunction *program.CompiledFunction) []string {
	var parts []string
	for i := range isa.NumRegisterKinds {
		count := compiledFunction.NumRegisters[i]
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", isa.RegisterKind(i).String(), count))
		}
	}
	return parts
}

// formatKindList formats a slice of register kinds as a parenthesised comma-separated
// list, e.g. "(int, string)" or "(none)".
//
// Takes kinds ([]isa.RegisterKind) which lists the kinds to format.
//
// Returns string containing the formatted parenthesised list.
func formatKindList(kinds []isa.RegisterKind) string {
	if len(kinds) == 0 {
		return "(none)"
	}
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.String()
	}
	return "(" + strings.Join(names, ", ") + ")"
}

// formatIntConstants formats an int constant pool compactly.
//
// Takes constants ([]int64) which is the pool to format.
//
// Returns string containing the formatted constant entries.
func formatIntConstants(constants []int64) string {
	parts := make([]string, len(constants))
	for i, v := range constants {
		parts[i] = fmt.Sprintf("[%d]=%d", i, v)
	}
	return strings.Join(parts, pkasmConstantSeparator)
}

// formatFloatConstants formats a float constant pool compactly.
//
// Takes constants ([]float64) which is the pool to format.
//
// Returns string containing the formatted constant entries.
func formatFloatConstants(constants []float64) string {
	parts := make([]string, len(constants))
	for i, v := range constants {
		parts[i] = fmt.Sprintf("[%d]=%g", i, v)
	}
	return strings.Join(parts, pkasmConstantSeparator)
}

// formatStringConstants formats a string constant pool compactly.
//
// Takes constants ([]string) which is the pool to format.
//
// Returns string containing the formatted constant entries.
func formatStringConstants(constants []string) string {
	parts := make([]string, len(constants))
	for i, v := range constants {
		s := v
		if len(s) > program.MaxDisassembleStringLen {
			s = s[:program.TruncatedDisassembleStringLen] + "..."
		}
		parts[i] = fmt.Sprintf("[%d]=%q", i, s)
	}
	return strings.Join(parts, pkasmConstantSeparator)
}

// formatBoolConstants formats a bool constant pool compactly.
//
// Takes constants ([]bool) which is the pool to format.
//
// Returns string containing the formatted constant entries.
func formatBoolConstants(constants []bool) string {
	parts := make([]string, len(constants))
	for i, v := range constants {
		parts[i] = fmt.Sprintf("[%d]=%v", i, v)
	}
	return strings.Join(parts, pkasmConstantSeparator)
}

// formatUintConstants formats a uint constant pool compactly.
//
// Takes constants ([]uint64) which is the pool to format.
//
// Returns string containing the formatted constant entries.
func formatUintConstants(constants []uint64) string {
	parts := make([]string, len(constants))
	for i, v := range constants {
		parts[i] = fmt.Sprintf("[%d]=%d", i, v)
	}
	return strings.Join(parts, pkasmConstantSeparator)
}

// formatComplexConstants formats a complex constant pool compactly.
//
// Takes constants ([]complex128) which is the pool to format.
//
// Returns string containing the formatted constant entries.
func formatComplexConstants(constants []complex128) string {
	parts := make([]string, len(constants))
	for i, v := range constants {
		parts[i] = fmt.Sprintf("[%d]=%v", i, v)
	}
	return strings.Join(parts, pkasmConstantSeparator)
}

// pkasmTier1CallComment resolves the call comment for the tier-1 call sub-ops.
//
// The call operations live at tier 1, so they share isa.OpDrillTier1 as their op byte and
// must be told apart by the sub-opcode in operand A.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes instr (instruction) which is the instruction to inspect.
//
// Returns string containing the resolved call comment, or empty when the sub-op is not a
// call.
func pkasmTier1CallComment(compiledFunction *program.CompiledFunction, instr isa.Instruction) string {
	switch isa.SubOpcode(instr.A) {
	case isa.SubOpCall:
		return resolveCallTarget(compiledFunction, instr, "call")
	case isa.SubOpTailCall:
		return resolveCallTarget(compiledFunction, instr, "tail call")
	case isa.SubOpCallIIFE:
		return resolveCallTarget(compiledFunction, instr, "iife")
	case isa.SubOpCallNative:
		siteIndex := int(instr.WideIndex())
		if siteIndex < len(compiledFunction.CallSites) {
			site := compiledFunction.CallSites[siteIndex]
			if site.IsClosure {
				return fmt.Sprintf("call closure general[%d] (site %d)", site.ClosureRegister, siteIndex)
			}
			if site.IsMethod {
				return fmt.Sprintf("call native method general[%d] (site %d)", site.NativeRegister, siteIndex)
			}
			return fmt.Sprintf("call native general[%d] (site %d)", site.NativeRegister, siteIndex)
		}
		return "call native"
	case isa.SubOpCallMethod:
		siteIndex := int(instr.WideIndex())
		return fmt.Sprintf("call method (site %d)", siteIndex)
	case isa.SubOpCallMethodInlineable:
		siteIndex := int(instr.WideIndex())
		return fmt.Sprintf("call method inlineable (site %d)", siteIndex)
	case isa.SubOpCallBuiltin:
		return "call builtin"
	default:
	}
	return ""
}
