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

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// emitSliceGet emits the read that plan describes: dest = collection[index].
//
// Takes plan (isaselect.SliceAccessPlan) which is the instruction PlanSliceGet chose.
// Takes dest (program.VarLocation) which receives the element.
// Takes collection (program.VarLocation) which holds the slice header.
// Takes index (program.VarLocation) which holds the int index.
func (c *Compiler) emitSliceGet(ctx context.Context, plan isaselect.SliceAccessPlan, dest, collection, index program.VarLocation) {
	if plan.UseTier1 {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(plan.Tier1), dest.Register, collection.Register)
		program.Emit(c.Function, isa.OpExt, index.Register, 0, 0)
		return
	}
	c.EmitTyped(ctx, plan.Op, dest, collection, index)
}

// emitSliceSet emits the write that plan describes: collection[index] = value.
//
// Takes plan (isaselect.SliceAccessPlan) which is the instruction PlanSliceSet chose.
// Takes collection (program.VarLocation) which holds the slice header.
// Takes index (program.VarLocation) which holds the int index.
// Takes value (program.VarLocation) which supplies the element.
func (c *Compiler) emitSliceSet(ctx context.Context, plan isaselect.SliceAccessPlan, collection, index, value program.VarLocation) {
	if plan.UseTier1 {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(plan.Tier1), collection.Register, index.Register)
		program.Emit(c.Function, isa.OpExt, value.Register, 0, 0)
		return
	}
	c.EmitTyped(ctx, plan.Op, collection, index, value)
}
