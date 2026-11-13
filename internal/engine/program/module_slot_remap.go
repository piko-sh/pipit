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

package program

import (
	"fmt"
	"math"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// RelativiseGlobalOperands rewrites global operands as bundle-relative by subtracting the
// per-kind base from each global-access instruction in every reachable function.
//
// Takes root (*CompiledFunction) which is the root of the function graph to walk.
// Takes base (SlotAllocation) which is the per-kind base offset to subtract.
//
// Returns error when an operand precedes its kind's base or a wide instruction is missing
// its extension word.
func RelativiseGlobalOperands(root *CompiledFunction, base SlotAllocation) error {
	if root == nil {
		return nil
	}
	visited := make(map[*CompiledFunction]struct{})
	return walkAndRelativise(root, base, visited)
}

// SetGlobalBases assigns bases to every reachable CompiledFunction so the VM offsets
// global operands at dispatch time.
//
// Takes root (*CompiledFunction) which is the root of the function graph to walk.
// Takes bases (*SlotAllocation) which is the per-kind base table to install.
func SetGlobalBases(root *CompiledFunction, bases *SlotAllocation) {
	if root == nil || bases == nil {
		return
	}
	visited := make(map[*CompiledFunction]struct{})
	walkAndSetBases(root, bases, visited)
}

// GlobalOperandExtent finds, per register kind, the lowest global slot the program's
// global-access instructions name and the number of slots from it to the highest.
//
// A program compiled from source names the slots its compiling service allocated, so its
// operands are absolute in that service's GlobalStore. A service that loads the program
// from bytecode reserves the extent in its own store and gives every function bases that
// move the operands into it.
//
// Takes roots (...*CompiledFunction) which are the function graphs to walk (the root and
// the variable initialiser).
//
// Returns low (SlotAllocation) which is the lowest slot named per kind, zero for a kind
// with none.
// Returns count (SlotAllocation) which is the number of slots from low to the highest
// slot named per kind.
// Returns error when an instruction names a kind out of range, a wide instruction lacks
// its extension word, or a kind names slot 65535 or spans more slots than a slot count
// can hold (wrapping fault.ErrBytecodeVerification and fault.ErrGlobalSlotsExhausted).
func GlobalOperandExtent(roots ...*CompiledFunction) (low, count SlotAllocation, err error) {
	var high SlotAllocation
	var seen [NumGlobalRegisterKinds]bool
	visited := make(map[*CompiledFunction]struct{})
	note := func(kind isa.RegisterKind, slot uint16) {
		if !seen[kind] || slot < low[kind] {
			low[kind] = slot
		}
		if !seen[kind] || slot > high[kind] {
			high[kind] = slot
		}
		seen[kind] = true
	}
	for _, root := range roots {
		if err := walkGlobalOperands(root, visited, note); err != nil {
			return SlotAllocation{}, SlotAllocation{}, err
		}
	}
	for kind := range NumGlobalRegisterKinds {
		if !seen[kind] {
			continue
		}
		extent := int(high[kind]) - int(low[kind]) + 1
		if extent > math.MaxUint16 || high[kind] == math.MaxUint16 {
			return SlotAllocation{}, SlotAllocation{}, fmt.Errorf("%w: %w: kind %d names slots %d to %d", fault.ErrBytecodeVerification, fault.ErrGlobalSlotsExhausted, kind, low[kind], high[kind])
		}
		count[kind] = safeconv.IntToUint16(extent)
	}
	return low, count, nil
}

// walkGlobalOperands calls note with the kind and slot of every global-access instruction
// in function and its descendants.
//
// Takes function (*CompiledFunction) which is the function to walk.
// Takes visited (map[*CompiledFunction]struct{}) which tracks already walked functions.
// Takes note (func(isa.RegisterKind, uint16)) which receives each operand.
//
// Returns error when an instruction names a kind out of range or a wide instruction lacks
// its extension word.
func walkGlobalOperands(function *CompiledFunction, visited map[*CompiledFunction]struct{}, note func(isa.RegisterKind, uint16)) error {
	if function == nil {
		return nil
	}
	if _, seen := visited[function]; seen {
		return nil
	}
	visited[function] = struct{}{}
	for i := 0; i < len(function.Body); i++ {
		skip, err := noteGlobalOperand(function, i, note)
		if err != nil {
			return err
		}
		i += skip
	}
	if err := walkGlobalOperands(function.VariableInitFunction, visited, note); err != nil {
		return err
	}
	for _, nested := range function.Functions {
		if err := walkGlobalOperands(nested, visited, note); err != nil {
			return err
		}
	}
	return nil
}

// noteGlobalOperand calls note with the kind and slot of the instruction at index i when
// it accesses a global, and reports how many extension words it consumed.
//
// Takes function (*CompiledFunction) which owns the instruction stream.
// Takes i (int) which is the instruction index.
// Takes note (func(isa.RegisterKind, uint16)) which receives the operand.
//
// Returns int which is the number of extension words consumed.
// Returns error when the instruction names a kind out of range or a wide instruction
// lacks its extension word.
func noteGlobalOperand(function *CompiledFunction, i int, note func(isa.RegisterKind, uint16)) (int, error) {
	instruction := function.Body[i]
	wide := isa.InstrIsTier1SubOp(instruction, isa.SubOpGetGlobalWide) || isa.InstrIsTier1SubOp(instruction, isa.SubOpSetGlobalWide)
	if instruction.Op != isa.OpGetGlobal && instruction.Op != isa.OpSetGlobal && !wide {
		return 0, nil
	}
	kind := isa.RegisterKind(instruction.C)
	if int(kind) >= NumGlobalRegisterKinds {
		return 0, fmt.Errorf("engine: global extent: kind %d out of range in %s", kind, function.Name)
	}
	if !wide {
		note(kind, uint16(instruction.B))
		return 0, nil
	}
	if i+1 >= len(function.Body) {
		return 0, fmt.Errorf("engine: global extent: wide op missing extension word at pc %d in %s", i, function.Name)
	}
	extension := function.Body[i+1]
	note(kind, uint16(extension.A)|uint16(extension.B)<<isa.WideBitShift)
	return 1, nil
}

// walkAndRelativise rewrites global operands in function and its descendants.
//
// Takes function (*CompiledFunction) which is the function to walk.
// Takes base (SlotAllocation) which is the per-kind base offset to subtract.
// Takes visited (map[*CompiledFunction]struct{}) which tracks already processed functions
// to break cycles.
//
// Returns error when any global operand rewrite fails.
func walkAndRelativise(function *CompiledFunction, base SlotAllocation, visited map[*CompiledFunction]struct{}) error {
	if function == nil {
		return nil
	}
	if _, seen := visited[function]; seen {
		return nil
	}
	visited[function] = struct{}{}

	for i := 0; i < len(function.Body); i++ {
		skip, err := relativiseGlobalInstruction(function, i, base)
		if err != nil {
			return err
		}
		i += skip
	}

	if err := walkAndRelativise(function.VariableInitFunction, base, visited); err != nil {
		return err
	}
	for _, nested := range function.Functions {
		if err := walkAndRelativise(nested, base, visited); err != nil {
			return err
		}
	}
	return nil
}

// relativiseGlobalInstruction rewrites the global-access instruction at index i to
// bundle-relative form and reports how many extra extension words it consumed.
//
// Takes function (*CompiledFunction) which owns the instruction stream.
// Takes i (int) which is the instruction index to rewrite.
// Takes base (SlotAllocation) which is the per-kind base offset to subtract.
//
// Returns int which is the number of extra extension words consumed.
// Returns error when the operand precedes its kind's base or the wide form is missing an
// extension word.
func relativiseGlobalInstruction(function *CompiledFunction, i int, base SlotAllocation) (int, error) {
	instruction := function.Body[i]
	switch instruction.Op {
	case isa.OpGetGlobal, isa.OpSetGlobal:
		kind := isa.RegisterKind(instruction.C)
		if int(kind) >= NumGlobalRegisterKinds {
			return 0, fmt.Errorf("engine: relativise: kind %d out of range in %s", kind, function.Name)
		}
		b := base[kind]
		if uint16(instruction.B) < b {
			return 0, fmt.Errorf("engine: relativise: %s references global %d but bundle base is %d (kind %d)", function.Name, instruction.B, b, kind)
		}
		function.Body[i].B = safeconv.MustIntToUint8(int(uint16(instruction.B) - b))
		return 0, nil
	case isa.OpDrillTier1:
		if !isa.InstrIsTier1SubOp(instruction, isa.SubOpGetGlobalWide) &&
			!isa.InstrIsTier1SubOp(instruction, isa.SubOpSetGlobalWide) {
			return 0, nil
		}
		if i+1 >= len(function.Body) {
			return 0, fmt.Errorf("engine: relativise: wide op missing extension word at pc %d in %s", i, function.Name)
		}
		ext := function.Body[i+1]
		kind := isa.RegisterKind(instruction.C)
		if int(kind) >= NumGlobalRegisterKinds {
			return 0, fmt.Errorf("engine: relativise: kind %d out of range in %s", kind, function.Name)
		}
		b := base[kind]
		encoded := uint16(ext.A) | uint16(ext.B)<<isa.WideBitShift
		if encoded < b {
			return 0, fmt.Errorf("engine: relativise: %s references global %d (wide) but bundle base is %d (kind %d)", function.Name, encoded, b, kind)
		}
		rel := encoded - b
		function.Body[i+1].A = uint8(rel & 0xff)
		function.Body[i+1].B = uint8(rel >> isa.WideBitShift)
		return 1, nil
	default:
		return 0, nil
	}
}

// walkAndSetBases stamps bases on function and its descendants.
//
// Takes function (*CompiledFunction) which is the function to stamp.
// Takes bases (*SlotAllocation) which is the per-kind base table.
// Takes visited (map[*CompiledFunction]struct{}) which tracks already processed functions
// to break cycles.
func walkAndSetBases(function *CompiledFunction, bases *SlotAllocation, visited map[*CompiledFunction]struct{}) {
	if function == nil {
		return
	}
	if _, seen := visited[function]; seen {
		return
	}
	visited[function] = struct{}{}
	function.GlobalBases = bases
	walkAndSetBases(function.VariableInitFunction, bases, visited)
	for _, nested := range function.Functions {
		walkAndSetBases(nested, bases, visited)
	}
}
