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

//go:build pipit_opcode_census

package engine

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// opcodeCensusBuilt reports whether opcode census instrumentation is enabled.
	opcodeCensusBuilt = true

	// opcodeCensusSlots is the size of the flat dispatch space the counters are keyed by.
	opcodeCensusSlots = isa.FlatDispatchSlots

	// opcodeCensusSlotBits is the width of one flat slot inside a packed pair or triple key.
	opcodeCensusSlotBits = 10

	// opcodeCensusSlotMask extracts one flat slot from a packed key.
	opcodeCensusSlotMask = opcodeCensusSlots - 1

	// opcodeCensusNoSlot marks a missing predecessor, so no pair or triple is counted.
	opcodeCensusNoSlot = opcodeCensusSlots

	// opcodeCensusSequenceLimit caps how many pairs and triples a report lists.
	opcodeCensusSequenceLimit = 400

	// opcodeCensusFunctionLimit caps how many functions a report lists.
	opcodeCensusFunctionLimit = 60
)

// slowCallCensusCount records reflective calls by kind and signature.
type slowCallCensusCount struct {
	// Kind is slowCallNative, slowCallClosure or slowCallMethod.
	Kind string `json:"kind"`

	// Signature is the function type called.
	Signature string `json:"signature"`

	// Count is the number of calls.
	Count uint64 `json:"count"`
}

// slowCallKey names one counter of the slow-call census.
type slowCallKey struct {
	// functionType is the function type called.
	functionType reflect.Type

	// kind is the call's kind.
	kind string
}

var (
	// slowCalls counts the reflective calls by kind and signature.
	slowCalls = map[slowCallKey]uint64{}

	// slowCallsMu guards slowCalls.
	slowCallsMu sync.Mutex
)

// OpcodeCensusCount is one named counter in an opcode census report.
type OpcodeCensusCount struct {
	// Name is the operation's printed name or the names of a sequence.
	Name string `json:"name"`

	// Constants holds the isa constant name of each operation.
	Constants []string `json:"constants"`

	// Slots holds the flat dispatch slot of each operation in the sequence.
	Slots []int `json:"slots"`

	// Count is how many times the operation or sequence was dispatched.
	Count uint64 `json:"count"`
}

// OpcodeCensusFunction is one function's dispatch counts in an opcode census report.
type OpcodeCensusFunction struct {
	// Name is the compiled function's name.
	Name string `json:"name"`

	// Function is the compiled function the counts belong to, excluded from JSON.
	Function *program.CompiledFunction `json:"-"`

	// Counts holds the dispatch count of each body word, indexed by program counter.
	Counts []uint64 `json:"counts"`

	// Total is the sum of Counts.
	Total uint64 `json:"total"`

	// Length is the number of words in the function body.
	Length int `json:"len"`
}

// OpcodeCensusReport is the result of an opcode census.
type OpcodeCensusReport struct {
	// Operations lists every dispatched operation, most frequent first.
	Operations []OpcodeCensusCount `json:"ops"`

	// Pairs lists the most frequent pairs of operations run one after the other.
	Pairs []OpcodeCensusCount `json:"bigrams"`

	// Triples lists the most frequent runs of three operations.
	Triples []OpcodeCensusCount `json:"trigrams"`

	// Functions lists the functions that dispatched the most instructions.
	Functions []OpcodeCensusFunction `json:"functions"`

	// Total is the number of instructions dispatched.
	Total uint64 `json:"total"`
}

// opcodeCensusState holds the counters of the process-wide opcode census.
type opcodeCensusState struct {
	// functions maps each function that ran to its per-word dispatch counts.
	functions map[*program.CompiledFunction][]uint64

	// triples counts runs of three operations, keyed by their packed flat slots.
	triples map[int]uint64

	// lastFunction is the function of the previous record, whose counts lastCounts caches.
	lastFunction *program.CompiledFunction

	// lastCounts is the per-word count slice of lastFunction.
	lastCounts []uint64

	// pairs counts pairs of operations, indexed by their packed flat slots.
	pairs []uint64

	// operations counts each operation, indexed by flat slot.
	operations [opcodeCensusSlots]uint64

	// total is the number of instructions recorded.
	total uint64

	// nextPC is the program counter that falls through from the previous record.
	nextPC int

	// previous is the flat slot of the previous record.
	previous int

	// beforePrevious is the flat slot of the record before the previous one.
	beforePrevious int

	// mu guards every field.
	mu sync.Mutex
}

var (
	// opcodeCensus is the process-wide census.
	opcodeCensus = newOpcodeCensusState()

	// opcodeCensusConstantPrefixes holds each tier's isa constant prefix.
	opcodeCensusConstantPrefixes = [...]string{"Op", "SubOp", "SubOpTier2", "SubOpTier3"}
)

// newOpcodeCensusState returns an empty census.
//
// Returns *opcodeCensusState which has no counts.
func newOpcodeCensusState() *opcodeCensusState {
	return &opcodeCensusState{
		functions:      map[*program.CompiledFunction][]uint64{},
		triples:        map[int]uint64{},
		lastFunction:   nil,
		lastCounts:     nil,
		pairs:          make([]uint64, opcodeCensusSlots<<opcodeCensusSlotBits),
		operations:     [opcodeCensusSlots]uint64{},
		total:          0,
		nextPC:         0,
		previous:       opcodeCensusNoSlot,
		beforePrevious: opcodeCensusNoSlot,
		mu:             sync.Mutex{},
	}
}

// countFunctionWord counts one dispatch of the word at pc in function.
//
// Takes function (*program.CompiledFunction) which is the running function.
// Takes length (int) which is the function's body length.
// Takes pc (int) which is the word's program counter.
func (census *opcodeCensusState) countFunctionWord(function *program.CompiledFunction, length int, pc int) {
	if function != census.lastFunction {
		counts, seen := census.functions[function]
		if !seen {
			counts = make([]uint64, length)
			census.functions[function] = counts
		}
		census.lastFunction = function
		census.lastCounts = counts
	}
	if pc < len(census.lastCounts) {
		census.lastCounts[pc]++
	}
}

// countSequences counts the pair and triple that slot completes, then makes slot the
// previous record.
//
// Takes slot (int) which is the flat slot just dispatched.
// Takes fallsThrough (bool) which is true when slot follows the previous record by
// fall-through in the same function.
func (census *opcodeCensusState) countSequences(slot int, fallsThrough bool) {
	if !fallsThrough || census.previous == opcodeCensusNoSlot {
		census.beforePrevious = opcodeCensusNoSlot
		census.previous = slot
		return
	}
	pair := census.previous<<opcodeCensusSlotBits | slot
	census.pairs[pair]++
	if census.beforePrevious != opcodeCensusNoSlot {
		census.triples[census.beforePrevious<<(2*opcodeCensusSlotBits)|pair]++
	}
	census.beforePrevious = census.previous
	census.previous = slot
}

// OpcodeCensusReset discards every count gathered so far.
//
// Safe for concurrent use.
func OpcodeCensusReset() {
	opcodeCensus.mu.Lock()
	defer opcodeCensus.mu.Unlock()
	clear(opcodeCensus.functions)
	clear(opcodeCensus.triples)
	clear(opcodeCensus.pairs)
	opcodeCensus.lastFunction = nil
	opcodeCensus.lastCounts = nil
	opcodeCensus.operations = [opcodeCensusSlots]uint64{}
	opcodeCensus.total = 0
	opcodeCensus.nextPC = 0
	opcodeCensus.previous = opcodeCensusNoSlot
	opcodeCensus.beforePrevious = opcodeCensusNoSlot
	slowCallsMu.Lock()
	clear(slowCalls)
	slowCallsMu.Unlock()
}

// opcodeCensusRecord counts an instruction and consecutive pairs and triples.
//
// Takes function (*program.CompiledFunction) which is the running function.
// Takes body ([]isa.Instruction) which is the function's body.
// Takes pc (int) which is the instruction's program counter.
//
// Safe for concurrent use.
func opcodeCensusRecord(function *program.CompiledFunction, body []isa.Instruction, pc int) {
	census := opcodeCensus
	census.mu.Lock()
	defer census.mu.Unlock()

	slot := isa.FlatIndexFor(body[pc])
	census.total++
	census.operations[slot]++
	fallsThrough := function == census.lastFunction && pc == census.nextPC
	census.countFunctionWord(function, len(body), pc)
	census.countSequences(slot, fallsThrough)

	next := pc + 1
	for next < len(body) && body[next].Op == isa.OpExtension {
		next++
	}
	census.nextPC = next
}

// OpcodeCensusSnapshot returns the counts gathered since the last reset.
//
// Returns OpcodeCensusReport which lists operations, sequences and functions, each most
// frequent first.
//
// Safe for concurrent use.
func OpcodeCensusSnapshot() OpcodeCensusReport {
	census := opcodeCensus
	census.mu.Lock()
	defer census.mu.Unlock()

	report := OpcodeCensusReport{
		Operations: nil,
		Pairs:      nil,
		Triples:    nil,
		Functions:  nil,
		Total:      census.total,
	}
	for slot, count := range &census.operations {
		if count != 0 {
			report.Operations = append(report.Operations, opcodeCensusSequence(count, slot))
		}
	}
	for pair, count := range census.pairs {
		if count != 0 {
			report.Pairs = append(report.Pairs,
				opcodeCensusSequence(count, pair>>opcodeCensusSlotBits, pair&opcodeCensusSlotMask))
		}
	}
	for key, count := range census.triples {
		report.Triples = append(report.Triples, opcodeCensusSequence(count,
			key>>(2*opcodeCensusSlotBits), key>>opcodeCensusSlotBits&opcodeCensusSlotMask, key&opcodeCensusSlotMask))
	}
	for function, counts := range census.functions {
		report.Functions = append(report.Functions, opcodeCensusFunctionEntry(function, counts))
	}
	report.Operations = opcodeCensusTopCounts(report.Operations, len(report.Operations))
	report.Pairs = opcodeCensusTopCounts(report.Pairs, opcodeCensusSequenceLimit)
	report.Triples = opcodeCensusTopCounts(report.Triples, opcodeCensusSequenceLimit)
	slices.SortFunc(report.Functions, func(left, right OpcodeCensusFunction) int {
		return cmp.Or(cmp.Compare(right.Total, left.Total), cmp.Compare(left.Name, right.Name))
	})
	report.Functions = report.Functions[:min(len(report.Functions), opcodeCensusFunctionLimit)]
	return report
}

// WriteOpcodeCensus writes the census gathered since the last reset to w as JSON.
//
// Takes w (io.Writer) which receives the report.
//
// Returns error when encoding or writing fails.
func WriteOpcodeCensus(w io.Writer) error {
	if err := json.NewEncoder(w).Encode(OpcodeCensusSnapshot()); err != nil {
		return fmt.Errorf("writing opcode census: %w", err)
	}
	return nil
}

// opcodeCensusSequence builds the report entry for a run of one or more flat slots.
//
// Takes count (uint64) which is how often the run was dispatched.
// Takes slots (...int) which are the flat slots of the run, in execution order.
//
// Returns OpcodeCensusCount which names the run.
func opcodeCensusSequence(count uint64, slots ...int) OpcodeCensusCount {
	names := make([]string, len(slots))
	constants := make([]string, len(slots))
	for index, slot := range slots {
		names[index], constants[index] = opcodeCensusName(slot)
	}
	return OpcodeCensusCount{Name: strings.Join(names, " ; "), Constants: constants, Slots: slots, Count: count}
}

// opcodeCensusName returns the names of the operation in a flat slot.
//
// Takes slot (int) which is the flat dispatch slot.
//
// Returns name (string) which is the disassembler's name with its tier prefix, or the
// slot number when no row exists.
// Returns constant (string) which is the operation's isa constant name, or empty when no
// row exists.
func opcodeCensusName(slot int) (name string, constant string) {
	tier := safeconv.MustIntToUint8(slot / isa.SlotsPerTier)
	row, found := isa.SpecAt(isa.Tier(tier), safeconv.MustIntToUint8(slot%isa.SlotsPerTier))
	if !found {
		return "slot " + strconv.Itoa(slot), ""
	}
	prefix := opcodeCensusConstantPrefixes[tier]
	return strconv.Itoa(int(tier)) + ":" + row.Name, prefix + strings.TrimPrefix(row.Handler, "handle")
}

// opcodeCensusFunctionEntry builds the report entry for one function.
//
// Takes function (*program.CompiledFunction) which is the function.
// Takes counts ([]uint64) which holds its per-word dispatch counts.
//
// Returns OpcodeCensusFunction which carries the function, a copy of the counts and their
// sum.
func opcodeCensusFunctionEntry(function *program.CompiledFunction, counts []uint64) OpcodeCensusFunction {
	total := uint64(0)
	for _, count := range counts {
		total += count
	}
	return OpcodeCensusFunction{
		Name:     function.Name,
		Function: function,
		Counts:   slices.Clone(counts),
		Total:    total,
		Length:   len(counts),
	}
}

// opcodeCensusTopCounts sorts entries most frequent first and keeps at most limit.
//
// Takes entries ([]OpcodeCensusCount) which is sorted in place.
// Takes limit (int) which is the most entries to keep.
//
// Returns []OpcodeCensusCount which is the leading part of entries.
func opcodeCensusTopCounts(entries []OpcodeCensusCount, limit int) []OpcodeCensusCount {
	slices.SortFunc(entries, func(left, right OpcodeCensusCount) int {
		return cmp.Or(cmp.Compare(right.Count, left.Count), cmp.Compare(left.Name, right.Name))
	})
	return entries[:min(len(entries), limit)]
}

// slowCallCensusRecord counts one call that used reflection.
//
// Takes kind (string) which is slowCallNative, slowCallClosure or slowCallMethod.
// Takes functionType (reflect.Type) which is the function type called.
//
// Safe for concurrent use.
func slowCallCensusRecord(kind string, functionType reflect.Type) {
	slowCallsMu.Lock()
	slowCalls[slowCallKey{functionType: functionType, kind: kind}]++
	slowCallsMu.Unlock()
}

// slowCallCensusSnapshot returns the reflective calls counted since the last reset.
//
// Returns []slowCallCensusCount which lists them, most frequent first.
//
// Safe for concurrent use.
func slowCallCensusSnapshot() []slowCallCensusCount {
	slowCallsMu.Lock()
	defer slowCallsMu.Unlock()
	counts := make([]slowCallCensusCount, 0, len(slowCalls))
	for key, count := range slowCalls {
		counts = append(counts, slowCallCensusCount{Kind: key.kind, Signature: key.functionType.String(), Count: count})
	}
	slices.SortFunc(counts, func(left, right slowCallCensusCount) int {
		return cmp.Or(cmp.Compare(right.Count, left.Count), cmp.Compare(left.Kind, right.Kind), cmp.Compare(left.Signature, right.Signature))
	})
	return counts
}
