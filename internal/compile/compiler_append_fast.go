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

// tryEmitStructFastAppend emits isa.OpAppendStructFast into a fresh general register when
// the slice's element type is a pointer-free struct or array, so the handler can copy the
// element bytes directly.
//
// Takes sliceLocation (VarLocation) which is the general-bank source slice.
// Takes location (VarLocation) which is the general-bank element.
// Takes sliceType (types.Type) which is the static slice type, nil when unknown.
//
// Returns the destination location and true when the fast opcode was emitted.
func (c *Compiler) tryEmitStructFastAppend(ctx context.Context, sliceLocation, location program.VarLocation, sliceType types.Type) (program.VarLocation, bool) {
	typeIndex, ok := c.structFastAppendTypeIndex(ctx, location, sliceType)
	if !ok {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAppendStructFast, dest, sliceLocation.Register, location.Register)
	program.EmitExtension(c.Function, typeIndex, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// emitStructFastAppendInto is the same-register form used by the in-place append path:
// the result lands back in dest, which is the source slice's own register, so the escape
// pass can authorise header reuse at the site.
//
// Takes dest (uint8) which is the destination and source general register.
// Takes sliceLocation (VarLocation) which is the general-bank source slice.
// Takes location (VarLocation) which is the general-bank element.
// Takes sliceType (types.Type) which is the static slice type, nil when unknown.
//
// Returns true when the fast opcode was emitted.
func (c *Compiler) emitStructFastAppendInto(ctx context.Context, dest uint8, sliceLocation, location program.VarLocation, sliceType types.Type) bool {
	typeIndex, ok := c.structFastAppendTypeIndex(ctx, location, sliceType)
	if !ok {
		return false
	}
	program.Emit(c.Function, isa.OpAppendStructFast, dest, sliceLocation.Register, location.Register)
	program.EmitExtension(c.Function, typeIndex, 0)
	return true
}

// structFastAppendTypeIndex resolves the type-table index for the element type of a
// pointer-free struct or array slice append.
//
// Takes location (VarLocation) which is the element; it must sit in the general bank.
// Takes sliceType (types.Type) which is the static slice type, nil when unknown.
//
// Returns the type-table index and true when the fast opcode applies.
func (c *Compiler) structFastAppendTypeIndex(ctx context.Context, location program.VarLocation, sliceType types.Type) (uint16, bool) {
	if sliceType == nil || location.Kind != isa.RegisterGeneral {
		return 0, false
	}
	slice, ok := sliceType.Underlying().(*types.Slice)
	if !ok {
		return 0, false
	}
	elementType := slice.Elem()
	switch elementType.Underlying().(type) {
	case *types.Struct, *types.Array:
	default:
		return 0, false
	}
	if !typesTypeIsPointerFree(elementType) {
		return 0, false
	}
	reflectType := c.TypeToReflect(ctx, elementType)
	if reflectType == nil {
		return 0, false
	}
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return 0, false
	}
	return typeIndex, true
}

// fastAppendOpcodeFor maps a tier-1 typed append sub-op to its top-level direct-exit
// opcode.
//
// Takes sub (isa.SubOpcode) which is the typed append variant chosen for the slice.
//
// Returns the promoted opcode and true for the int, float and string variants, or zero
// and false for the variants that keep their tier-1 encoding.
func fastAppendOpcodeFor(sub isa.SubOpcode) (isa.Opcode, bool) {
	switch sub {
	case isa.SubOpAppendInt:
		return isa.OpAppendIntFast, true
	case isa.SubOpAppendFloat:
		return isa.OpAppendFloatFast, true
	case isa.SubOpAppendString:
		return isa.OpAppendStringFast, true
	default:
		return 0, false
	}
}

// typesTypeIsPointerFree reports whether values of t contain no pointers, mirroring the
// runtime typeIsPointerFree check on the static type.
//
// Takes t (types.Type) which is the static type to classify.
//
// Returns true for numeric and boolean basics and for arrays and structs composed solely
// of pointer-free types.
func typesTypeIsPointerFree(t types.Type) bool {
	switch underlying := t.Underlying().(type) {
	case *types.Basic:
		info := underlying.Info()
		return info&(types.IsNumeric|types.IsBoolean) != 0 && info&types.IsUntyped == 0 && underlying.Kind() != types.UnsafePointer
	case *types.Array:
		return typesTypeIsPointerFree(underlying.Elem())
	case *types.Struct:
		for field := range underlying.Fields() {
			if !typesTypeIsPointerFree(field.Type()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
