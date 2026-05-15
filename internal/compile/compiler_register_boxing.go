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
	"reflect"

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

var (
	// canonicalGeneralBoxType maps each typed bank to the reflect.Type the fast
	// move-to-general sub-op assumes, so boxNeedsPreciseType can detect mismatches.
	canonicalGeneralBoxType = [isa.NumRegisterKinds]reflect.Type{
		isa.RegisterInt:         typeFor[int](),
		isa.RegisterFloat:       typeFor[float64](),
		isa.RegisterString:      typeFor[string](),
		isa.RegisterBool:        typeFor[bool](),
		isa.RegisterUint:        typeFor[uint64](),
		isa.RegisterComplex:     typeFor[complex128](),
		isa.RegisterSliceInt:    typeFor[[]int](),
		isa.RegisterSliceFloat:  typeFor[[]float64](),
		isa.RegisterSliceString: typeFor[[]string](),
		isa.RegisterSliceBool:   typeFor[[]bool](),
		isa.RegisterSliceUint:   typeFor[[]uint64](),
		isa.RegisterSliceByte:   typeFor[[]byte](),
	}
)

// emitBoxToGeneral emits the instruction that boxes source into the general register,
// using a dedicated sub-op for int, float and string or isa.OpPackTyped when the source
// carries a non-canonical type.
//
// Takes destinationGeneralRegister (uint8) which receives the boxed value.
// Takes source (VarLocation) which is the value to box.
func (c *Compiler) emitBoxToGeneral(ctx context.Context, destinationGeneralRegister uint8, source program.VarLocation) {
	if c.boxNeedsPreciseType(source) && source.Kind != isa.RegisterGeneral {
		if aligned, ok := c.alignForTypedBox(ctx, source); ok && c.emitTypedBox(destinationGeneralRegister, aligned) {
			return
		}
	}
	switch source.Kind {
	case isa.RegisterInt:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveIntToGeneral), destinationGeneralRegister, source.Register)
	case isa.RegisterFloat:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveFloatToGeneral), destinationGeneralRegister, source.Register)
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveStringToGeneral), destinationGeneralRegister, source.Register)
	default:
		program.Emit(c.Function, isa.OpPackInterface, destinationGeneralRegister, source.Register, uint8(source.Kind))
	}
}

// alignForTypedBox moves source into the bank its recorded type's kind implies, which is
// what the precise isa.OpPackTyped path reads from. Values can legitimately sit
// elsewhere: comparison results and comma-ok flags have static type bool but live in the
// int bank.
//
// Takes source (VarLocation) which carries the recorded sourceType.
//
// Returns the aligned location and true, or false when no coercion to the natural bank
// exists and the canonical box must be used instead.
func (c *Compiler) alignForTypedBox(ctx context.Context, source program.VarLocation) (program.VarLocation, bool) {
	if isa.IsTypedSliceKind(source.Kind) {
		return source, source.SourceType.Kind() == reflect.Slice
	}
	natural := naturalBankForBoxKind(source.SourceType.Kind())
	if natural == source.Kind {
		return source, true
	}
	if natural == isa.RegisterGeneral {
		return source, false
	}
	coerced := c.coerceToKind(ctx, source, natural)
	if coerced.Kind != natural {
		return source, false
	}
	coerced.SourceType = source.SourceType
	return coerced, true
}

// emitBoxElementToGeneral boxes a composite-literal element, map key or value, or struct
// field store into the general bank.
//
// Delegates to emitBoxToGeneral; the name is kept at call sites that document why
// precision matters.
//
// Takes destinationGeneralRegister (uint8) which is the destination general register.
// Takes source (VarLocation) which is the value being boxed.
func (c *Compiler) emitBoxElementToGeneral(ctx context.Context, destinationGeneralRegister uint8, source program.VarLocation) {
	c.emitBoxToGeneral(ctx, destinationGeneralRegister, source)
}

// boxElementToGeneralTemp is the type-preserving sibling of boxToGeneralTemp for
// composite-literal elements: it boxes location into a fresh temporary general register
// via emitBoxElementToGeneral and updates location in place. No-op when already general.
//
// Takes location (*VarLocation) which is boxed in place into a temporary general
// register.
func (c *Compiler) boxElementToGeneralTemp(ctx context.Context, location *program.VarLocation) {
	if location.Kind == isa.RegisterGeneral {
		return
	}
	generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	c.emitBoxElementToGeneral(ctx, generalRegister, *location)
	*location = program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
}

// boxElementToGeneral boxes location into a persistent general register, preserving its
// source type, and updates location in place. No-op when already general.
//
// Takes location (*VarLocation) which is boxed in place.
func (c *Compiler) boxElementToGeneral(ctx context.Context, location *program.VarLocation) {
	if location.Kind == isa.RegisterGeneral {
		return
	}
	generalRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	c.emitBoxElementToGeneral(ctx, generalRegister, *location)
	*location = program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
}

// boxNeedsPreciseType reports whether boxing source must preserve its exact type via
// isa.OpPackTyped rather than the fast move-to-general sub-op.
//
// Takes source (VarLocation) whose sourceType and kind decide the boxing path.
//
// Returns true when the recorded sourceType differs from the bank's canonical type.
func (*Compiler) boxNeedsPreciseType(source program.VarLocation) bool {
	if source.SourceType == nil {
		return false
	}
	return source.SourceType != canonicalGeneralBoxType[source.Kind]
}

// emitTypedBox emits isa.OpPackTyped to box source into the general bank.
//
// Preserves source's exact source-level reflect.Type. When source carries no recorded
// type or when the type-table is exhausted the helper emits nothing and returns false,
// leaving the caller to fall back to the generic isa.OpPackInterface / move-to-general
// path.
//
// Takes destinationGeneralRegister (uint8) which receives the box.
// Takes source (VarLocation) whose sourceType drives the boxing type.
//
// Returns bool which is true when isa.OpPackTyped (plus its isa.OpExt type-index word)
// was emitted.
func (c *Compiler) emitTypedBox(destinationGeneralRegister uint8, source program.VarLocation) bool {
	if source.SourceType == nil {
		return false
	}
	typeIndex, err := program.AddTypeRef(c.Function, source.SourceType)
	if err != nil {
		c.recordStickyError(err)
		return false
	}
	program.Emit(c.Function, isa.OpPackTyped, destinationGeneralRegister, source.Register, uint8(source.Kind))
	program.EmitExtension(c.Function, typeIndex, 0)
	return true
}

// boxToGeneral boxes location into a freshly allocated persistent general register and
// updates location in place. No-op when location is already in the general bank.
//
// Takes location (*VarLocation) which names the value to box and is rewritten in place to
// its new general-bank slot.
func (c *Compiler) boxToGeneral(ctx context.Context, location *program.VarLocation) {
	if location.Kind == isa.RegisterGeneral {
		return
	}
	generalRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	c.emitBoxToGeneral(ctx, generalRegister, *location)
	*location = program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
}

// boxToGeneralTemp boxes location into a freshly allocated temporary general register and
// updates location in place. No-op when location is already in the general bank.
//
// Takes location (*VarLocation) which names the value to box and is rewritten in place to
// its new general-bank temporary slot.
func (c *Compiler) boxToGeneralTemp(ctx context.Context, location *program.VarLocation) {
	if location.Kind == isa.RegisterGeneral {
		return
	}
	generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	c.emitBoxToGeneral(ctx, generalRegister, *location)
	*location = program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
}

// coerceToKind returns a VarLocation in destinationKind, emitting the box or unbox
// instruction that bridges the banks. Returns location unchanged when they already match.
//
// Takes location (VarLocation) which is the value being coerced.
// Takes destinationKind (isa.RegisterKind) which is the target bank.
//
// Returns the coerced VarLocation.
func (c *Compiler) coerceToKind(ctx context.Context, location program.VarLocation, destinationKind isa.RegisterKind) program.VarLocation {
	if location.Kind == destinationKind {
		return location
	}
	if destinationKind == isa.RegisterGeneral {
		destination := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
		c.emitBoxToGeneral(ctx, destination, location)
		return program.VarLocation{Register: destination, Kind: isa.RegisterGeneral}
	}
	if location.Kind == isa.RegisterGeneral {
		destination := c.Scopes.Alloc.AllocTemp(destinationKind)
		switch destinationKind {
		case isa.RegisterInt:
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToInt), destination, location.Register)
		case isa.RegisterFloat:
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToFloat), destination, location.Register)
		case isa.RegisterString:
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToString), destination, location.Register)
		default:
			program.Emit(c.Function, isa.OpUnpackInterface, destination, location.Register, uint8(destinationKind))
		}
		return program.VarLocation{Register: destination, Kind: destinationKind}
	}
	if location.Kind == isa.RegisterInt && destinationKind == isa.RegisterBool {
		destination := c.Scopes.Alloc.AllocTemp(isa.RegisterBool)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpIntToBool), destination, location.Register)
		return program.VarLocation{Register: destination, Kind: isa.RegisterBool}
	}
	if location.Kind == isa.RegisterBool && destinationKind == isa.RegisterInt {
		destination := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpBoolToInt), destination, location.Register)
		return program.VarLocation{Register: destination, Kind: isa.RegisterInt}
	}
	return location
}

// emitUnboxFromGeneral emits the instruction that unboxes the value in general register
// sourceGeneralRegister into a freshly allocated register of destinationKind.
//
// Only the three banks with a dedicated unbox sub-op are listed. The default emits
// isa.OpUnpackInterface, which services every remaining bank.
//
// Takes sourceGeneralRegister (uint8) which is the source general register.
// Takes destinationKind (isa.RegisterKind) which is the destination bank.
//
// Returns program.VarLocation which is the freshly allocated destination location.
func (c *Compiler) emitUnboxFromGeneral(sourceGeneralRegister uint8, destinationKind isa.RegisterKind) program.VarLocation {
	destination := c.Scopes.Alloc.Alloc(destinationKind)
	switch destinationKind {
	case isa.RegisterInt:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToInt), destination, sourceGeneralRegister)
	case isa.RegisterFloat:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToFloat), destination, sourceGeneralRegister)
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToString), destination, sourceGeneralRegister)
	default:
		program.Emit(c.Function, isa.OpUnpackInterface, destination, sourceGeneralRegister, uint8(destinationKind))
	}
	return program.VarLocation{Register: destination, Kind: destinationKind}
}

// naturalBankForBoxKind maps a boxed type's reflect.Kind to the register bank
// handlePackTyped reads the source value from (boxTypedToGeneral and boxScalarToArenaBox
// select the bank by the boxed type's kind, not by the instruction's kind marker).
//
// Unlike registerKindForReflectKind this maps Complex64/Complex128 to the complex bank,
// matching boxTypedToGeneral's Complex arm.
//
// Takes kind (reflect.Kind) which is the boxed type's kind.
//
// Returns the isa.RegisterKind bank the runtime box reads, or isa.RegisterGeneral for
// compound kinds.
func naturalBankForBoxKind(kind reflect.Kind) isa.RegisterKind {
	if kind == reflect.Complex64 || kind == reflect.Complex128 {
		return isa.RegisterComplex
	}
	return isaselect.RegisterKindForReflectKind(kind)
}
