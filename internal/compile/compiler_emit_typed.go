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

package compile

import (
	"context"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// EmitTyped emits a three-operand opcode, coercing any operand whose bank disagrees with
// the opcode's shape descriptor.
//
// Takes op (opcode) which is the opcode to emit.
// Takes a (VarLocation) which is the first operand.
// Takes b (VarLocation) which is the second operand.
// Takes c2 (VarLocation) which is the third operand.
func (c *Compiler) EmitTyped(ctx context.Context, op isa.Opcode, a, b, c2 program.VarLocation) {
	shape := isa.OperandShapeFor(op)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		program.Emit(c.Function, op, a.Register, b.Register, c2.Register)
		return
	}
	a = c.CoerceForOperand(ctx, shape.A, shape.Reads[0], a)
	b = c.CoerceForOperand(ctx, shape.B, shape.Reads[1], b)
	c2 = c.CoerceForOperand(ctx, shape.C, shape.Reads[2], c2)
	program.Emit(c.Function, op, a.Register, b.Register, c2.Register)
}

// CoerceForOperand applies coerceToKind to a single operand position when role resolves
// to a concrete register bank and the source bank disagrees.
//
// Takes role (isa.OperandRole) which is the operand role drawn from the shape descriptor.
// Takes reads (bool) which is true only for register-read positions; writes and
// non-register roles return location unchanged.
// Takes location (VarLocation) which is the candidate operand location.
//
// Returns the original location when no coercion is required, otherwise the coerced
// replacement.
func (c *Compiler) CoerceForOperand(ctx context.Context, role isa.OperandRole, reads bool, location program.VarLocation) program.VarLocation {
	if !reads {
		return location
	}
	expectedKind, ok := isa.KindForRole(role)
	if !ok {
		return location
	}
	if location.Kind == expectedKind {
		return location
	}
	return c.coerceToKind(ctx, location, expectedKind)
}

// rawOperand wraps a raw byte as a VarLocation so it can flow through EmitTyped.
//
// Takes value (uint8) which is the operand byte.
//
// Returns a VarLocation carrying value in the register field.
func rawOperand(value uint8) program.VarLocation {
	return program.VarLocation{Register: value, Kind: isa.RegisterGeneral}
}
