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

//go:build ignore

// Command gen_opcode_tables emits the engine's opcode-keyed tables from the isa spec
// rows.
//
// The rows in internal/isa/spec_tier*.go are the one description of every operation: its
// Go handler, its assembly body and the jump table that body lives in, its tier-2 shim
// and its direct-exit stub. This program turns those columns into the engine declarations
// so adding an operation is: add the enum constant, add one row, write the handler,
// regenerate.
//
// Run via:
//
//	go run gen_opcode_tables.go
//
// from internal/engine (paths are relative), or via the //go:generate directive in
// vm_handler_flat_table.go, which runs it before gen_flat_switch.go because the flat
// switch is derived from the registration maps emitted here.
//
// With -validate nothing is written: the program regenerates in memory, compares against
// the committed files and exits 1 with a summary of every file that differs.
//
// Emitted files, each carrying the Go-standard "Code generated ... DO NOT EDIT." line:
//
//   - vm_handler_table_generated.go: the four registration maps gen_flat_switch.go reads.
//   - asm_jumptable_generated.go: buildStaticJumpTableEntries, the static jump-table
//     installs asmgen emits into initJumpTable.
//   - pathb_shim_registry_generated.go: pathBShimRegistry, the tier-2 assembly-call
//     shims.
//   - dispatch_direct_exits_generated.go: installPerOpDirectExits and
//     directExitHandlerAddresses. Both name assembly stubs, so the file carries the
//     assembly dispatch build constraint.
//   - direct_exit_registry_generated.go: directExitRegistrations, which asmgen reads to
//     emit the stub bodies and which every target builds.
//
// The enum constant for a (tier, code) pair is not stored in the rows, so it is recovered
// by parsing the four iota blocks in internal/isa/opcode.go.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/isa"
)

const (
	// opcodeSourcePath is the isa enum source, relative to internal/engine.
	opcodeSourcePath = "../isa/opcode.go"

	// tierCount is the number of encoding tiers.
	tierCount = 4

	// slotCount is the number of codes per tier.
	slotCount = 256

	// isaQualifier prefixes every enum constant reference in the emitted files.
	isaQualifier = "isa."

	// isaImportPath is the import path of the isa package.
	isaImportPath = "pipit.sh/pipit/internal/isa"

	// asmImportPath is the import path of the asm package.
	asmImportPath = "pipit.sh/pipit/internal/engine/asm"

	// assemblyDispatchConstraint is the build constraint of the assembly dispatch files.
	// vm_dispatch_direct_exits.go declares the exit stubs under it, so the file that takes
	// their addresses must carry the same one.
	assemblyDispatchConstraint = "//go:build !safe && !(js && wasm) && (amd64 || arm64)"

	// tier1TableConstant names the engine constant for the tier-1 jump table symbol.
	tier1TableConstant = "tier1JumpTableSymbol"

	// tier2TableConstant names the engine constant for the tier-2 jump table symbol.
	tier2TableConstant = "tier2JumpTableSymbol"

	// tier3TableConstant names the engine constant for the tier-3 jump table symbol.
	tier3TableConstant = "tier3JumpTableSymbol"

	// outputFileMode is the permission of every written file: source, readable like the rest
	// of the package.
	outputFileMode = 0o644

	// entryIndent is the indentation of one element inside a composite literal.
	entryIndent = "\t"
)

// generatedFileHeader opens every emitted file: the Apache 2.0 licence and the
// anti-fascism stand the rest of the codebase carries, then the Go-standard generated
// marker that golangci-lint and gopls look for.
const generatedFileHeader = `// Copyright 2026 PolitePixels Limited
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

// Code generated by go run gen_opcode_tables.go. DO NOT EDIT.

`

var (
	// enumTypeTiers maps each enum type in opcode.go to the tier it enumerates.
	enumTypeTiers = map[string]isa.Tier{
		"Opcode":         isa.TierMain,
		"SubOpcode":      isa.TierSub1,
		"SubOpcodeTier2": isa.TierSub2,
		"SubOpcodeTier3": isa.TierSub3,
	}

	// jumpTableConstants maps a row's AsmTable value to the engine constant naming that
	// table.
	jumpTableConstants = map[string]string{
		"tier1JumpTable": tier1TableConstant,
		"tier2JumpTable": tier2TableConstant,
		"tier3JumpTable": tier3TableConstant,
	}

	// tierJumpTables maps a sub-tier to the engine constant naming its jump table, which is
	// where that tier's direct-exit stubs are installed.
	tierJumpTables = map[isa.Tier]string{
		isa.TierSub1: tier1TableConstant,
		isa.TierSub2: tier2TableConstant,
		isa.TierSub3: tier3TableConstant,
	}
)

// handlerTable describes one registration map: the tier it covers, its Go name, the isa
// key type it is keyed by and the doc comment it is declared with.
type handlerTable struct {
	// doc is the complete doc comment, each line terminated.
	doc string

	// name is the Go variable name.
	name string

	// keyType is the isa enum type of the map key, unqualified.
	keyType string

	// tier selects the rows the map is built from.
	tier isa.Tier
}

// handlerTables describes the four registration maps in emission order. handlerTables
// drives one emitted map per tier.
//
// handlerRegistrations maps each tier-0 opcode to its Go handler; gen_flat_switch.go
// reads it to emit the flat dispatch switch. Opcodes with no entry fall through to
// handleInvalidOpcode, and the drill opcode dispatches through the tier tables instead.
// In each sub-tier map the drill marker (isa.SubOpDrillTier2, isa.SubOpTier2DrillTier3)
// descends a tier and never lands in the flat switch; unlisted tier-3 slots fall through
// to handleFlatUnknownTier3 via the generated switch's default arm.
var handlerTables = []handlerTable{
	{
		tier:    isa.TierMain,
		name:    "handlerRegistrations",
		keyType: "Opcode",
	},
	{
		tier:    isa.TierSub1,
		name:    "flatTier1Registrations",
		keyType: "SubOpcode",
	},
	{
		tier:    isa.TierSub2,
		name:    "flatTier2Registrations",
		keyType: "SubOpcodeTier2",
	},
	{
		tier:    isa.TierSub3,
		name:    "flatTier3Registrations",
		keyType: "SubOpcodeTier3",
	},
}

// outputFile is one emitted file and its formatted content.
type outputFile struct {
	// path is the output path, relative to internal/engine.
	path string

	// content is the gofmt-formatted file.
	content []byte
}

// generator holds the spec rows and the enum names they are keyed by.
type generator struct {
	// names holds the enum constant name at every (tier, code), or empty when the code is
	// not declared.
	names [tierCount][slotCount]string

	// rows are the spec rows in tier then code order.
	rows []isa.OpSpec
}

// constant returns the qualified enum constant of a row, for example isa.OpAddInt.
//
// Takes row (isa.OpSpec) which is the spec row.
//
// Returns string which is the qualified constant.
// Returns error when the row's code has no constant.
func (g *generator) constant(row isa.OpSpec) (string, error) {
	name := g.names[row.Tier][row.Code]
	if name == "" {
		return "", fmt.Errorf("spec row %s has no enum constant in %s", row, opcodeSourcePath)
	}
	return isaQualifier + name, nil
}

// rowLines renders the lines of every row that passes keep, in tier then code order.
//
// Takes keep (rowFilter) which selects the rows.
// Takes render (rowRenderer) which renders one row's lines.
//
// Returns []string which is the concatenated lines.
// Returns error when any row fails to render.
func (g *generator) rowLines(keep rowFilter, render rowRenderer) ([]string, error) {
	lines := make([]string, 0, len(g.rows))
	for index := range g.rows {
		row := g.rows[index]
		if !keep(row) {
			continue
		}
		constant, err := g.constant(row)
		if err != nil {
			return nil, err
		}
		rendered, err := render(row, constant)
		if err != nil {
			return nil, err
		}
		lines = append(lines, rendered...)
	}
	return lines, nil
}

// emitAll renders and formats every output file.
//
// Returns []outputFile which is every file in a fixed order.
// Returns error when an emitter or formatter fails.
func (g *generator) emitAll() ([]outputFile, error) {
	emitters := []struct {
		emit func() (string, error)
		path string
	}{
		{path: "vm_handler_table_generated.go", emit: g.emitHandlerTables},
		{path: "asm_jumptable_generated.go", emit: g.emitJumpTable},
		{path: "pathb_shim_registry_generated.go", emit: g.emitShimRegistry},
		{path: "dispatch_direct_exits_generated.go", emit: g.emitDirectExitInstalls},
		{path: "direct_exit_registry_generated.go", emit: g.emitDirectExitRegistry},
	}
	files := make([]outputFile, 0, len(emitters))
	for _, emitter := range emitters {
		text, err := emitter.emit()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", emitter.path, err)
		}
		formatted, err := format.Source([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("%s: format: %w\n%s", emitter.path, err, text)
		}
		files = append(files, outputFile{path: emitter.path, content: formatted})
	}
	return files, nil
}

// emitHandlerTables emits the four registration maps.
//
// Returns string which is the file text.
// Returns error when a row has no constant.
func (g *generator) emitHandlerTables() (string, error) {
	var out strings.Builder
	for _, table := range handlerTables {
		lines, err := g.rowLines(
			func(row isa.OpSpec) bool { return row.Tier == table.tier && row.Handler != "" },
			func(row isa.OpSpec, constant string) ([]string, error) {
				return []string{constant + ": " + row.Handler}, nil
			})
		if err != nil {
			return "", err
		}
		out.WriteString(table.doc)
		out.WriteString("var " + table.name + " = map[" + isaQualifier + table.keyType + "]opcodeHandler{\n")
		writeLiteralBody(&out, entryIndent, lines)
		out.WriteString("}\n\n")
	}
	return fileText("", [][]string{{isaImportPath}}, out.String()), nil
}

// emitJumpTable emits buildStaticJumpTableEntries.
//
// Returns string which is the file text.
// Returns error when a row names an unknown table.
func (g *generator) emitJumpTable() (string, error) {
	lines, err := g.rowLines(
		func(row isa.OpSpec) bool {
			return row.AsmBody != "" || (row.ExitStub != "" && row.Tier != isa.TierMain)
		},
		jumpTableLines)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("func buildStaticJumpTableEntries() []asm.AsmHandlerJumpTableEntry {\n" +
		"\treturn []asm.AsmHandlerJumpTableEntry{\n")
	writeLiteralBody(&out, entryIndent+entryIndent, lines)
	out.WriteString("\t}\n}\n")
	return fileText("", [][]string{{asmImportPath, isaImportPath}}, out.String()), nil
}

// emitShimRegistry emits pathBShimRegistry.
//
// Returns string which is the file text.
// Returns error when a shim sits on a sub-tier row.
func (g *generator) emitShimRegistry() (string, error) {
	lines, err := g.rowLines(func(row isa.OpSpec) bool { return row.Shim != "" }, shimLine)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(
		"var pathBShimRegistry = []shimRegistration{\n")
	writeLiteralBody(&out, entryIndent, lines)
	out.WriteString("}\n")
	return fileText("", [][]string{{isaImportPath}}, out.String()), nil
}

// emitDirectExitInstalls emits installPerOpDirectExits and directExitHandlerAddresses.
//
// Returns string which is the file text.
// Returns error when a row has no constant. The installer this emits must run after
// initJumpTable, which fills every slot with pathBFallback, and before
// installFlatJumpTableASM, which snapshots asmJumpTable into the runtime-hot
// flatJumpTable. Sub-tier stubs are installed by initJumpTable itself, from
// buildStaticJumpTableEntries. The address set it also emits deliberately omits slots
// owned by tier-2 shims: those stay in assembly via DISPATCH_NEXT, and listing them would
// force spurious tier-2 batching.
func (g *generator) emitDirectExitInstalls() (string, error) {
	installs, err := g.rowLines(
		func(row isa.OpSpec) bool { return hasExitStub(row) && row.Tier == isa.TierMain },
		func(row isa.OpSpec, constant string) ([]string, error) {
			return []string{"asmJumpTable[" + constant + "] = " + stubAddress(row)}, nil
		})
	if err != nil {
		return "", err
	}
	addresses, err := g.rowLines(hasExitStub, func(row isa.OpSpec, _ string) ([]string, error) {
		return []string{stubAddress(row)}, nil
	})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("func installPerOpDirectExits() {\n")
	for _, line := range installs {
		out.WriteString(entryIndent + line + "\n")
	}
	out.WriteString("}\n\n")
	out.WriteString(
		"var directExitHandlerAddresses = []uintptr{\n")
	writeLiteralBody(&out, entryIndent, addresses)
	out.WriteString("}\n")
	return fileText(assemblyDispatchConstraint, [][]string{{"reflect"}, {isaImportPath}}, out.String()), nil
}

// emitDirectExitRegistry emits directExitRegistrations.
//
// Returns string which is the file text.
// Returns error when a stub has no exit reason.
func (g *generator) emitDirectExitRegistry() (string, error) {
	lines, err := g.rowLines(hasExitStub, func(row isa.OpSpec, _ string) ([]string, error) {
		if row.ExitReason == "" {
			return nil, fmt.Errorf("spec row %s has an exit stub but no exit reason", row)
		}
		return []string{"{Name: " + strconv.Quote(row.ExitStub) + ", ExitReason: " + row.ExitReason + "}"}, nil
	})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("var directExitRegistrations = []asm.DirectExitHandlerSpec{\n")
	writeLiteralBody(&out, entryIndent, lines)
	out.WriteString("}\n")
	return fileText("", [][]string{{asmImportPath}}, out.String()), nil
}

// rowFilter selects the rows an emitter renders.
type rowFilter func(row isa.OpSpec) bool

// rowRenderer renders the lines one row contributes, given its qualified enum constant.
type rowRenderer func(row isa.OpSpec, constant string) ([]string, error)

// main parses the flags and runs the generator, exiting 1 on any failure.
func main() {
	validate := flag.Bool("validate", false,
		"compare the committed tables against the generated ones instead of writing them")
	flag.Parse()

	if err := run(*validate); err != nil {
		fmt.Fprintf(os.Stderr, "gen_opcode_tables: %v\n", err)
		os.Exit(1)
	}
}

// run generates every file in memory and then either validates or writes them.
//
// Takes validate (bool) which selects comparison against the committed files over
// writing.
//
// Returns error which is the first failure, or the validation verdict.
func run(validate bool) error {
	names, err := loadEnumNames(opcodeSourcePath)
	if err != nil {
		return err
	}
	gen := &generator{names: names, rows: isa.AllSpecs()}
	files, err := gen.emitAll()
	if err != nil {
		return err
	}
	if validate {
		return validateFiles(files)
	}
	return writeFiles(files)
}

// loadEnumNames parses the four iota blocks in opcode.go and returns the constant name at
// every (tier, code).
//
// Takes path (string) which is the opcode.go source path.
//
// Returns [tierCount][slotCount]string which is the name table.
// Returns error when the source does not parse or does not hold exactly one block per
// tier.
func loadEnumNames(path string) ([tierCount][slotCount]string, error) {
	var names [tierCount][slotCount]string
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return names, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := 0
	for _, decl := range file.Decls {
		block, ok := decl.(*ast.GenDecl)
		if !ok || block.Tok != token.CONST {
			continue
		}
		tier, ok := enumBlockTier(block)
		if !ok {
			continue
		}
		if err := collectEnumBlock(block, &names[tier]); err != nil {
			return names, fmt.Errorf("%s: %w", path, err)
		}
		seen++
	}
	if seen != tierCount {
		return names, fmt.Errorf("%s: found %d enum blocks, want %d", path, seen, tierCount)
	}
	return names, nil
}

// enumBlockTier reports which tier a const block enumerates. A block qualifies when its
// first member is typed as one of the four enum types and starts at iota.
//
// Takes block (*ast.GenDecl) which is a const declaration.
//
// Returns isa.Tier which is the enumerated tier.
// Returns bool which is false when the block is not an enum.
func enumBlockTier(block *ast.GenDecl) (isa.Tier, bool) {
	if len(block.Specs) == 0 {
		return isa.TierMain, false
	}
	first, ok := block.Specs[0].(*ast.ValueSpec)
	if !ok || first.Type == nil || len(first.Values) != 1 {
		return isa.TierMain, false
	}
	typeName, ok := first.Type.(*ast.Ident)
	if !ok {
		return isa.TierMain, false
	}
	tier, ok := enumTypeTiers[typeName.Name]
	if !ok {
		return isa.TierMain, false
	}
	value, ok := first.Values[0].(*ast.Ident)
	if !ok || value.Name != "iota" {
		return isa.TierMain, false
	}
	return tier, true
}

// collectEnumBlock records the member names of one enum block in declaration order. Every
// member after the first must be a bare name, so that its value is the previous one plus
// one; an explicit type or value would break the name-to-code correspondence.
//
// Takes block (*ast.GenDecl) which is the enum block.
// Takes names (*[slotCount]string) which receives the name at each code.
//
// Returns error when a member does not follow the iota sequence or the block overflows a
// byte.
func collectEnumBlock(block *ast.GenDecl, names *[slotCount]string) error {
	for index, spec := range block.Specs {
		member, ok := spec.(*ast.ValueSpec)
		if !ok || len(member.Names) != 1 {
			return fmt.Errorf("enum member %d: want exactly one name per spec", index)
		}
		name := member.Names[0].Name
		if index > 0 && (member.Type != nil || len(member.Values) != 0) {
			return fmt.Errorf("enum member %s: an explicit type or value breaks the iota sequence", name)
		}
		if index >= slotCount {
			return fmt.Errorf("enum member %s: more than %d members in one tier", name, slotCount)
		}
		names[index] = name
	}
	return nil
}

// fileText assembles one output file: licence and generated marker, optional build
// constraint, package clause, import groups and body.
//
// Takes constraint (string) which is the //go:build line, or empty for none.
// Takes importGroups ([][]string) which are the import paths, one blank-separated group
// per element.
// Takes body (string) which is the declarations.
//
// Returns string which is the unformatted file text.
func fileText(constraint string, importGroups [][]string, body string) string {
	var out strings.Builder
	out.WriteString(generatedFileHeader)
	if constraint != "" {
		out.WriteString(constraint + "\n\n")
	}
	out.WriteString("package engine\n\n")
	if len(importGroups) > 0 {
		out.WriteString("import (\n")
		for index, group := range importGroups {
			if index > 0 {
				out.WriteString("\n")
			}
			for _, path := range group {
				out.WriteString(entryIndent + strconv.Quote(path) + "\n")
			}
		}
		out.WriteString(")\n\n")
	}
	out.WriteString(body)
	return out.String()
}

// writeLiteralBody writes the indented, comma-terminated elements of a composite literal.
//
// Takes out (*strings.Builder) which receives the text.
// Takes indent (string) which precedes every element.
// Takes lines ([]string) which are the elements without their trailing comma.
func writeLiteralBody(out *strings.Builder, indent string, lines []string) {
	for _, line := range lines {
		out.WriteString(indent + line + ",\n")
	}
}

// jumpTableLines renders the jump-table entries of one row: its assembly body, then its
// sub-tier direct-exit stub. Tier-0 stubs are patched in by installPerOpDirectExits
// instead, after initJumpTable has run.
//
// Takes row (isa.OpSpec) which is the spec row.
// Takes constant (string) which is the row's qualified enum constant.
//
// Returns []string which is zero, one or two entry literals.
// Returns error when the row names an unknown jump table.
func jumpTableLines(row isa.OpSpec, constant string) ([]string, error) {
	offset := "int(" + constant + ") * bytesPerJumpTableSlot"
	lines := make([]string, 0, 2)
	if row.AsmBody != "" {
		table, err := jumpTableConstant(row.AsmTable)
		if err != nil {
			return nil, fmt.Errorf("spec row %s: %w", row, err)
		}
		lines = append(lines, jumpTableEntry(row.AsmBody, table, offset))
	}
	if row.ExitStub != "" && row.Tier != isa.TierMain {
		lines = append(lines, jumpTableEntry(row.ExitStub, tierJumpTables[row.Tier], offset))
	}
	return lines, nil
}

// jumpTableEntry renders one asm.AsmHandlerJumpTableEntry literal.
//
// Takes name (string) which is the assembly symbol.
// Takes table (string) which is the Go expression naming the jump table.
// Takes offset (string) which is the Go expression computing the byte offset.
//
// Returns string which is the literal without its trailing comma.
func jumpTableEntry(name, table, offset string) string {
	return "{Name: " + strconv.Quote(name) + ", TableSymbol: " + table + ", Offset: " + offset + "}"
}

// jumpTableConstant maps a row's AsmTable to the Go expression naming that table. The
// empty value is the main table, spelled as an empty string literal.
//
// Takes table (string) which is the row's AsmTable value.
//
// Returns string which is the Go expression.
// Returns error when the table is unknown.
func jumpTableConstant(table string) (string, error) {
	if table == "" {
		return `""`, nil
	}
	if constant, ok := jumpTableConstants[table]; ok {
		return constant, nil
	}
	return "", fmt.Errorf("unknown jump table %q", table)
}

// shimLine renders one shimRegistration literal.
//
// Shims bind operations at any tier: the registration carries the tier and the code
// within it, so the shim installs into that tier's jump table at that slot.
//
// Takes row (isa.OpSpec) which is the spec row.
// Takes constant (string) which is the row's qualified enum constant.
//
// Returns []string which is the one literal.
// Returns error which is always nil; present so the signature matches the other line
// emitters.
func shimLine(row isa.OpSpec, constant string) ([]string, error) {
	line := fmt.Sprintf("{HandlerName: %q, ShimSuffix: %q, Tier: %s, Code: uint8(%s), Narrow: %t, InstallSuppressed: %t}",
		row.ShimHandler, row.Shim, tierConstant(row.Tier), constant,
		row.Has(isa.SpecShimNarrow), row.Has(isa.SpecShimSuppressed))
	return []string{line}, nil
}

// tierConstant renders the qualified isa.Tier constant for a tier.
//
// Takes tier (isa.Tier) which is the encoding depth.
//
// Returns string which is the Go expression naming it.
func tierConstant(tier isa.Tier) string {
	switch tier {
	case isa.TierSub1:
		return "isa.TierSub1"
	case isa.TierSub2:
		return "isa.TierSub2"
	case isa.TierSub3:
		return "isa.TierSub3"
	case isa.TierMain:
		return "isa.TierMain"
	default:
		return "isa.TierMain"
	}
}

// hasExitStub selects the rows bound to a direct-exit stub.
//
// Takes row (isa.OpSpec) which is the spec row.
//
// Returns bool which is true when the row has a stub.
func hasExitStub(row isa.OpSpec) bool { return row.ExitStub != "" }

// stubAddress renders the address of a row's exit stub.
//
// Takes row (isa.OpSpec) which is the spec row.
//
// Returns string which is the reflect expression yielding the stub's address.
func stubAddress(row isa.OpSpec) string {
	return "reflect.ValueOf(" + row.ExitStub + ").Pointer()"
}

// validateFiles compares each generated file against the committed one.
//
// Takes files ([]outputFile) which are the freshly generated files.
//
// Returns error naming how many files differ, or nil when every file matches. A file that
// cannot be read counts as differing.
func validateFiles(files []outputFile) error {
	stale := 0
	for _, file := range files {
		committed, err := os.ReadFile(file.path)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "%s: cannot read the committed file: %v\n", file.path, err)
			stale++
		case !bytes.Equal(committed, file.content):
			fmt.Fprintf(os.Stderr, "%s is stale: first difference at line %d (committed %d bytes, generated %d bytes)\n",
				file.path, firstDifferingLine(committed, file.content), len(committed), len(file.content))
			stale++
		default:
			fmt.Fprintf(os.Stderr, "// %s matches the spec rows\n", file.path)
		}
	}
	if stale > 0 {
		return fmt.Errorf("%d of %d generated files no longer match the spec rows; "+
			"regenerate with: make generate-engine", stale, len(files))
	}
	return nil
}

// firstDifferingLine returns the 1-based line at which two byte slices first differ, or
// the line after the shorter one ends when one is a prefix of the other.
//
// Takes committed ([]byte) which is the file on disk.
// Takes generated ([]byte) which is the freshly generated file.
//
// Returns int which is the line number.
func firstDifferingLine(committed, generated []byte) int {
	line := 1
	for index := range min(len(committed), len(generated)) {
		if committed[index] != generated[index] {
			return line
		}
		if committed[index] == '\n' {
			line++
		}
	}
	return line
}

// writeFiles writes every generated file to disk.
//
// Takes files ([]outputFile) which are the files to write.
//
// Returns error when a write fails.
func writeFiles(files []outputFile) error {
	for _, file := range files {
		if err := os.WriteFile(file.path, file.content, outputFileMode); err != nil {
			return fmt.Errorf("write %s: %w", file.path, err)
		}
		fmt.Fprintf(os.Stderr, "// wrote %s (%d bytes)\n", file.path, len(file.content))
	}
	return nil
}
