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
	"go/types"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// holdValue keeps a value for later use in a temporary at the bottom of the region that
// begins at mark, freeing every other temporary its evaluation left behind.
//
// Takes mark ([isa.NumRegisterKinds]uint32) which is the allocation state before the
// value was compiled.
// Takes location (program.VarLocation) which holds the value; a register, not a spill
// slot.
// Takes staticType (types.Type) which is the value's static type, used to pick the move's
// copy mode; nil is allowed.
//
// Returns program.VarLocation which is the temporary that now holds the value.
func (c *Compiler) holdValue(ctx context.Context, mark [isa.NumRegisterKinds]uint32, location program.VarLocation, staticType types.Type) program.VarLocation {
	c.Scopes.RestoreWatermark(mark)
	held := program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(location.Kind), Kind: location.Kind}
	if held.Register == location.Register {
		if location.Kind == isa.RegisterGeneral {
			c.emitSameKindMoveTyped(ctx, held, location, staticType)
		}
		return held
	}
	c.emitMoveTyped(ctx, held, location, staticType)
	return held
}

// reserveReturnSlots keeps the registers a multi-value return writes its results into out
// of the temporaries the return values are held in, so moving one result into place
// cannot overwrite a held value that a later move still reads.
func (c *Compiler) reserveReturnSlots() {
	var counts [isa.NumRegisterKinds]uint32
	for _, kind := range c.Function.ResultKinds {
		counts[kind]++
	}
	for kind, count := range counts {
		if count > 0 {
			c.Scopes.Alloc.ReserveLow(isa.RegisterKind(kind), count)
		}
	}
}

// compactArgument releases call argument temporaries when argument compaction is enabled.
//
// Takes mark ([isa.NumRegisterKinds]uint32) which is the allocation state before the
// argument was compiled.
// Takes location (program.VarLocation) which holds the argument's value.
// Takes staticType (types.Type) which is the argument's static type; nil is allowed.
//
// Returns program.VarLocation which holds the value afterwards.
func (c *Compiler) compactArgument(ctx context.Context, mark [isa.NumRegisterKinds]uint32, location program.VarLocation, staticType types.Type) program.VarLocation {
	if !c.argumentLeftTemporaries(mark, location) {
		return location
	}
	if !c.pressure.settings.compactArguments {
		c.pressure.compactionWouldHelp = true
		return location
	}
	c.Scopes.RestoreWatermark(mark)
	if uint32(location.Register) < c.Scopes.Alloc.Snapshot()[location.Kind] {
		return location
	}
	held := program.VarLocation{Register: c.Scopes.Alloc.Alloc(location.Kind), Kind: location.Kind, SourceType: location.SourceType}
	if held.Register != location.Register {
		c.emitSameKindMoveTyped(ctx, held, location, staticType)
	}
	return held
}

// argumentLeftTemporaries reports whether an argument's value is a temporary with other
// temporaries of its evaluation allocated below it or in other banks, which compaction
// could free.
//
// Takes mark ([isa.NumRegisterKinds]uint32) which is the allocation state before the
// argument was compiled.
// Takes location (program.VarLocation) which holds the argument's value.
//
// Returns bool which is true when compaction would free registers.
func (c *Compiler) argumentLeftTemporaries(mark [isa.NumRegisterKinds]uint32, location program.VarLocation) bool {
	if location.IsSpilled || location.IsIndirect || location.IsUpvalue || uint32(location.Register) < mark[location.Kind] {
		return false
	}
	current := c.Scopes.Alloc.Snapshot()
	for kind := range current {
		held := mark[kind]
		if isa.RegisterKind(kind) == location.Kind {
			held++
		}
		if current[kind] > held {
			return true
		}
	}
	return false
}
