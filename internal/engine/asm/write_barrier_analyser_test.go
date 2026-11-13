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

package asm

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
)

type wbArchitecture struct {
	name            string
	contextRegister string
	call            string
	unconditional   string
	wordMoves       map[string]bool
}

var wbArchitectures = []wbArchitecture{
	{name: "amd64", contextRegister: "R15", call: "CALL", unconditional: "JMP", wordMoves: map[string]bool{"MOVQ": true}},
	{name: "arm64", contextRegister: "R19", call: "BL", unconditional: "B", wordMoves: map[string]bool{"MOVD": true}},
}

type wbInstruction struct {
	text     string
	op       string
	operands []string
	labels   []string
}

type wbOperand struct {
	register  string
	base      string
	index     string
	symbol    string
	memory    bool
	immediate bool
}

type wbState struct {
	tainted          map[string]bool
	barrierAddress   map[string]bool
	barrierFlag      map[string]bool
	flagsTestBarrier bool
	clear            bool
}

type wbViolation struct {
	handler string
	text    string
}

var (
	wbTextLine      = regexp.MustCompile(`^TEXT\s+·(\w+)\(SB\)`)
	wbLabelLine     = regexp.MustCompile(`^(\w+):$`)
	wbMacroLine     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*\(\)$`)
	wbRegister      = regexp.MustCompile(`^(?:[A-Z]{2}|R\d+|X\d+|F\d+|V\d+|RSP|ZR|SB|FP)$`)
	wbParenGroup    = regexp.MustCompile(`\(([^()]*)\)`)
	wbIndexRegister = regexp.MustCompile(`^([A-Z]{2}|R\d+)`)
)

var wbExitMacros = map[string]bool{"DISPATCH_NEXT()": true, "DIV_BY_ZERO_EXIT()": true}

type wbEdge struct {
	target     int
	barrierOff bool
}

func (v wbViolation) String() string {
	return v.handler + ": " + v.text
}

func parseHandlers(content string) (map[string][]wbInstruction, error) {
	handlers := map[string][]wbInstruction{}
	var name string
	var pending []string
	for raw := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(raw)
		if comment := strings.Index(line, "//"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if match := wbTextLine.FindStringSubmatch(line); match != nil {
			name = match[1]
			pending = nil
			handlers[name] = nil
			continue
		}
		if name == "" {
			continue
		}
		if match := wbLabelLine.FindStringSubmatch(line); match != nil {
			pending = append(pending, match[1])
			continue
		}
		for part := range strings.SplitSeq(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" || part == "NO_LOCAL_POINTERS" {
				continue
			}
			if wbMacroLine.MatchString(part) && !wbExitMacros[part] {
				return nil, fmt.Errorf("%s: unknown macro %s", name, part)
			}
			op, rest, _ := strings.Cut(part, " ")
			handlers[name] = append(handlers[name], wbInstruction{
				text:     part,
				op:       op,
				operands: splitOperands(strings.TrimSpace(rest)),
				labels:   pending,
			})
			pending = nil
		}
	}
	return handlers, nil
}

func splitOperands(list string) []string {
	if list == "" {
		return nil
	}
	var operands []string
	depth := 0
	start := 0
	for index, character := range list {
		switch character {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				operands = append(operands, strings.TrimSpace(list[start:index]))
				start = index + 1
			}
		}
	}
	return append(operands, strings.TrimSpace(list[start:]))
}

func parseOperand(text string) wbOperand {
	if symbol, immediate := strings.CutPrefix(text, "$"); immediate {
		return wbOperand{immediate: true, symbol: symbol}
	}
	if wbRegister.MatchString(text) {
		return wbOperand{register: text}
	}
	groups := wbParenGroup.FindAllStringSubmatch(text, -1)
	if len(groups) == 0 {
		return wbOperand{symbol: text}
	}
	operand := wbOperand{memory: true, symbol: text[:strings.Index(text, "(")]}
	first := groups[0][1]
	if wbRegister.MatchString(first) {
		operand.base = first
		if len(groups) > 1 {
			operand.index = wbIndexRegister.FindString(groups[1][1])
		}
		return operand
	}
	operand.symbol = first
	if len(groups) > 1 {
		operand.base = groups[1][1]
	}
	return operand
}

func copyState(state wbState) wbState {
	return wbState{
		tainted:          maps.Clone(state.tainted),
		barrierAddress:   maps.Clone(state.barrierAddress),
		barrierFlag:      maps.Clone(state.barrierFlag),
		flagsTestBarrier: state.flagsTestBarrier,
		clear:            state.clear,
	}
}

func mergeState(existing, incoming wbState) (wbState, bool) {
	merged := copyState(existing)
	for register := range incoming.tainted {
		merged.tainted[register] = true
	}
	for register := range merged.barrierAddress {
		if !incoming.barrierAddress[register] {
			delete(merged.barrierAddress, register)
		}
	}
	for register := range merged.barrierFlag {
		if !incoming.barrierFlag[register] {
			delete(merged.barrierFlag, register)
		}
	}
	merged.flagsTestBarrier = merged.flagsTestBarrier && incoming.flagsTestBarrier
	merged.clear = merged.clear && incoming.clear
	changed := len(merged.tainted) != len(existing.tainted) ||
		len(merged.barrierAddress) != len(existing.barrierAddress) ||
		len(merged.barrierFlag) != len(existing.barrierFlag) ||
		merged.flagsTestBarrier != existing.flagsTestBarrier || merged.clear != existing.clear
	return merged, changed
}

func analyseHandler(arch wbArchitecture, bankBases map[string]bool, name string, instructions []wbInstruction) ([]wbViolation, int, error) {
	labels := map[string]int{}
	for index, instruction := range instructions {
		for _, label := range instruction.labels {
			labels[label] = index
		}
	}
	states := make([]*wbState, len(instructions))
	if len(instructions) == 0 {
		return nil, 0, nil
	}
	entry := wbState{tainted: map[string]bool{}, barrierAddress: map[string]bool{}, barrierFlag: map[string]bool{}}
	states[0] = &entry
	worklist := []int{0}
	for len(worklist) > 0 {
		index := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		out := transfer(arch, bankBases, instructions[index], copyState(*states[index]))
		edges, err := successors(arch, labels, instructions, index, out)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", name, err)
		}
		for _, edge := range edges {
			incoming := copyState(out)
			if edge.barrierOff {
				incoming.clear = true
			}
			if states[edge.target] == nil {
				states[edge.target] = &incoming
				worklist = append(worklist, edge.target)
				continue
			}
			if merged, changed := mergeState(*states[edge.target], incoming); changed {
				states[edge.target] = &merged
				worklist = append(worklist, edge.target)
			}
		}
	}
	var violations []wbViolation
	stores := 0
	for index, instruction := range instructions {
		state := states[index]
		if state == nil || !isStore(instruction) {
			continue
		}
		destination := wbOperand{memory: true, base: "DI"}
		if instruction.op != "MOVSQ" && instruction.op != "MOVSB" {
			destination = parseOperand(instruction.operands[len(instruction.operands)-1])
		}
		if !state.tainted[destination.base] && !state.tainted[destination.index] {
			continue
		}
		stores++
		if !state.clear {
			violations = append(violations, wbViolation{handler: name, text: instruction.text})
		}
	}
	return violations, stores, nil
}

func isStore(instruction wbInstruction) bool {
	if instruction.op == "MOVSQ" || instruction.op == "MOVSB" {
		return true
	}
	if len(instruction.operands) == 0 || isComparison(instruction.op) || isBranch(instruction.op) {
		return false
	}
	return parseOperand(instruction.operands[len(instruction.operands)-1]).memory
}

func isComparison(op string) bool {
	for _, prefix := range []string{"CMP", "TEST", "UCOMI", "FCMP", "CMN", "TST"} {
		if strings.HasPrefix(op, prefix) {
			return true
		}
	}
	return false
}

func isBranch(op string) bool {
	if op == "RET" || op == "BL" || op == "CALL" || wbExitMacros[op] {
		return true
	}
	if strings.HasPrefix(op, "J") || op == "CBZ" || op == "CBNZ" || op == "TBZ" || op == "TBNZ" {
		return true
	}
	return op == "B" || (strings.HasPrefix(op, "B") && len(op) == 3 && op != "BIC")
}

func successors(arch wbArchitecture, labels map[string]int, instructions []wbInstruction, index int, state wbState) ([]wbEdge, error) {
	instruction := instructions[index]
	next := []wbEdge{}
	if index+1 < len(instructions) {
		next = append(next, wbEdge{target: index + 1})
	}
	op := instruction.op
	switch {
	case op == "RET" || wbExitMacros[op]:
		return nil, nil
	case op == arch.call:
		return next, nil
	case !isBranch(op):
		return next, nil
	}
	target := instruction.operands[len(instruction.operands)-1]
	targetIndex, local := labels[target]
	if !local && !strings.Contains(target, "(") && !strings.HasPrefix(target, "·") {
		return nil, fmt.Errorf("branch to unknown label %q in %q", target, instruction.text)
	}
	if op == arch.unconditional {
		if !local {
			return nil, nil
		}
		return []wbEdge{{target: targetIndex}}, nil
	}
	takenOff, fallthroughOff := barrierBranch(instruction, state)
	edges := make([]wbEdge, 0, len(next)+1)
	for _, edge := range next {
		edges = append(edges, wbEdge{target: edge.target, barrierOff: fallthroughOff})
	}
	if local {
		edges = append(edges, wbEdge{target: targetIndex, barrierOff: takenOff})
	}
	return edges, nil
}

func barrierBranch(instruction wbInstruction, state wbState) (takenOff, fallthroughOff bool) {
	tests := state.flagsTestBarrier
	if instruction.op == "CBZ" || instruction.op == "CBNZ" {
		tests = state.barrierFlag[instruction.operands[0]]
	}
	if !tests {
		return false, false
	}
	switch instruction.op {
	case "JE", "JEQ", "JZ", "CBZ", "BEQ":
		return true, false
	case "JNE", "JNZ", "CBNZ", "BNE":
		return false, true
	default:
		return false, false
	}
}

func transfer(arch wbArchitecture, bankBases map[string]bool, instruction wbInstruction, state wbState) wbState {
	op := instruction.op
	if op == arch.call {
		return wbState{tainted: map[string]bool{}, barrierAddress: map[string]bool{}, barrierFlag: map[string]bool{}}
	}
	if isBranch(op) {
		return state
	}
	operands := make([]wbOperand, len(instruction.operands))
	for index, text := range instruction.operands {
		operands[index] = parseOperand(text)
	}
	if isComparison(op) {
		state.flagsTestBarrier = testsBarrierFlag(operands, state)
		return state
	}
	destinations := destinationRegisters(instruction, operands)
	if len(destinations) == 0 {
		state.flagsTestBarrier = state.flagsTestBarrier && isMove(op)
		return state
	}
	taint := resultTaint(arch, bankBases, op, operands, state)
	address, flag := barrierRegisters(op, operands, state)
	for _, register := range destinations {
		state.tainted[register] = taint
		if !taint {
			delete(state.tainted, register)
		}
		setFact(state.barrierAddress, register, address)
		setFact(state.barrierFlag, register, flag)
	}
	state.flagsTestBarrier = state.flagsTestBarrier && isMove(op)
	return state
}

func setFact(facts map[string]bool, register string, holds bool) {
	if holds {
		facts[register] = true
		return
	}
	delete(facts, register)
}

func isMove(op string) bool {
	return strings.HasPrefix(op, "MOV") || strings.HasPrefix(op, "LEA") || op == "LDP" || op == "STP"
}

func destinationRegisters(instruction wbInstruction, operands []wbOperand) []string {
	if len(operands) == 0 {
		return nil
	}
	if instruction.op == "LDP" {
		return wbParenRegisters(instruction.operands[len(instruction.operands)-1])
	}
	last := operands[len(operands)-1]
	if last.register == "" || last.memory {
		return nil
	}
	return []string{last.register}
}

func wbParenRegisters(text string) []string {
	var registers []string
	for part := range strings.SplitSeq(strings.Trim(text, "()"), ",") {
		registers = append(registers, strings.TrimSpace(part))
	}
	return registers
}

func resultTaint(arch wbArchitecture, bankBases map[string]bool, op string, operands []wbOperand, state wbState) bool {
	sources := operands[:len(operands)-1]
	destination := operands[len(operands)-1].register
	sourceTaint := func(operand wbOperand) bool {
		if operand.memory {
			return operand.base == arch.contextRegister && bankBases[operand.symbol]
		}
		return state.tainted[operand.register]
	}
	switch {
	case arch.wordMoves[op] && len(sources) == 1:
		return sourceTaint(sources[0])
	case strings.HasPrefix(op, "LEA"):
		return len(sources) == 1 && (state.tainted[sources[0].base] || state.tainted[sources[0].index])
	case strings.HasPrefix(op, "ADD") || strings.HasPrefix(op, "SUB") || strings.HasPrefix(op, "INC") || strings.HasPrefix(op, "DEC") ||
		strings.HasPrefix(op, "CMOV") || op == "CSEL":
		taint := len(sources) < 2 && state.tainted[destination]
		for _, source := range sources {
			taint = taint || sourceTaint(source)
		}
		return taint
	default:
		return false
	}
}

func barrierRegisters(op string, operands []wbOperand, state wbState) (address, flag bool) {
	if len(operands) != 2 {
		return false, false
	}
	source := operands[0]
	if source.immediate && strings.HasPrefix(source.symbol, "runtime·writeBarrier(SB)") {
		return true, false
	}
	if !strings.HasPrefix(op, "MOVB") && !strings.HasPrefix(op, "MOVW") && op != "MOVL" {
		return false, false
	}
	if source.memory && (strings.HasPrefix(source.symbol, "runtime·writeBarrier") || state.barrierAddress[source.base]) {
		return false, true
	}
	return false, false
}

func testsBarrierFlag(operands []wbOperand, state wbState) bool {
	for _, operand := range operands {
		if operand.memory && strings.HasPrefix(operand.symbol, "runtime·writeBarrier") {
			return true
		}
		if operand.register != "" && state.barrierFlag[operand.register] {
			return true
		}
	}
	return false
}
