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

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
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

// walkAndRelativise rewrites global operands in fn and its descendants.
//
// Takes fn (*CompiledFunction) which is the function to walk.
// Takes base (SlotAllocation) which is the per-kind base offset to subtract.
// Takes visited (map[*CompiledFunction]struct{}) which tracks already processed functions
// to break cycles.
//
// Returns error when any global operand rewrite fails.
func walkAndRelativise(fn *CompiledFunction, base SlotAllocation, visited map[*CompiledFunction]struct{}) error {
	if fn == nil {
		return nil
	}
	if _, seen := visited[fn]; seen {
		return nil
	}
	visited[fn] = struct{}{}

	for i := 0; i < len(fn.Body); i++ {
		skip, err := relativiseGlobalInstruction(fn, i, base)
		if err != nil {
			return err
		}
		i += skip
	}

	if err := walkAndRelativise(fn.VariableInitFunction, base, visited); err != nil {
		return err
	}
	for _, nested := range fn.Functions {
		if err := walkAndRelativise(nested, base, visited); err != nil {
			return err
		}
	}
	return nil
}

// relativiseGlobalInstruction rewrites the global-access instruction at index i to
// bundle-relative form and reports how many extra extension words it consumed.
//
// Takes fn (*CompiledFunction) which owns the instruction stream.
// Takes i (int) which is the instruction index to rewrite.
// Takes base (SlotAllocation) which is the per-kind base offset to subtract.
//
// Returns int which is the number of extra extension words consumed.
// Returns error when the operand precedes its kind's base or the wide form is missing an
// extension word.
func relativiseGlobalInstruction(fn *CompiledFunction, i int, base SlotAllocation) (int, error) {
	ins := fn.Body[i]
	switch ins.Op {
	case isa.OpGetGlobal, isa.OpSetGlobal:
		kind := isa.RegisterKind(ins.C)
		if int(kind) >= NumGlobalRegisterKinds {
			return 0, fmt.Errorf("engine: relativise: kind %d out of range in %s", kind, fn.Name)
		}
		b := base[kind]
		if uint16(ins.B) < b {
			return 0, fmt.Errorf("engine: relativise: %s references global %d but bundle base is %d (kind %d)", fn.Name, ins.B, b, kind)
		}
		fn.Body[i].B = safeconv.MustIntToUint8(int(uint16(ins.B) - b))
		return 0, nil
	case isa.OpDrillTier1:
		if !isa.InstrIsTier1SubOp(ins, isa.SubOpGetGlobalWide) &&
			!isa.InstrIsTier1SubOp(ins, isa.SubOpSetGlobalWide) {
			return 0, nil
		}
		if i+1 >= len(fn.Body) {
			return 0, fmt.Errorf("engine: relativise: wide op missing extension word at pc %d in %s", i, fn.Name)
		}
		ext := fn.Body[i+1]
		kind := isa.RegisterKind(ins.C)
		if int(kind) >= NumGlobalRegisterKinds {
			return 0, fmt.Errorf("engine: relativise: kind %d out of range in %s", kind, fn.Name)
		}
		b := base[kind]
		encoded := uint16(ext.A) | uint16(ext.B)<<isa.WideBitShift
		if encoded < b {
			return 0, fmt.Errorf("engine: relativise: %s references global %d (wide) but bundle base is %d (kind %d)", fn.Name, encoded, b, kind)
		}
		rel := encoded - b
		fn.Body[i+1].A = uint8(rel & 0xff)
		fn.Body[i+1].B = uint8(rel >> isa.WideBitShift)
		return 1, nil
	default:
		return 0, nil
	}
}

// walkAndSetBases stamps bases on fn and its descendants.
//
// Takes fn (*CompiledFunction) which is the function to stamp.
// Takes bases (*SlotAllocation) which is the per-kind base table.
// Takes visited (map[*CompiledFunction]struct{}) which tracks already processed functions
// to break cycles.
func walkAndSetBases(fn *CompiledFunction, bases *SlotAllocation, visited map[*CompiledFunction]struct{}) {
	if fn == nil {
		return
	}
	if _, seen := visited[fn]; seen {
		return
	}
	visited[fn] = struct{}{}
	fn.GlobalBases = bases
	walkAndSetBases(fn.VariableInitFunction, bases, visited)
	for _, nested := range fn.Functions {
		walkAndSetBases(nested, bases, visited)
	}
}
