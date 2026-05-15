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
	"go/ast"
	"go/types"
	"math"
	"reflect"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

// registerPackageLevelVar allocates a GlobalStore slot for each name declared in spec.
//
// Takes spec (*ast.ValueSpec) which declares one or more package-level variables.
func (c *Compiler) registerPackageLevelVar(_ context.Context, spec *ast.ValueSpec) {
	for _, name := range spec.Names {
		if name.Name == typemap.BlankIdentName {
			continue
		}
		typeObject := c.Info.Defs[name]
		if typeObject == nil {
			continue
		}
		kind := typemap.KindForType(typeObject.Type())
		if c.addressTakenGlobals[name.Name] {
			c.globalVariables[name.Name] = program.GlobalVariableInfo{Index: c.globals.AllocGeneral(reflect.Value{}), Kind: kind, IsIndirect: true}
			continue
		}
		var index int
		switch kind {
		case isa.RegisterInt:
			index = c.globals.AllocInt(0)
		case isa.RegisterFloat:
			index = c.globals.AllocFloat(0)
		case isa.RegisterString:
			index = c.globals.AllocString("")
		case isa.RegisterBool:
			index = c.globals.AllocBool(false)
		case isa.RegisterUint:
			index = c.globals.AllocUint(0)
		case isa.RegisterComplex:
			index = c.globals.AllocComplex(0)
		case isa.RegisterGeneral:
			index = c.globals.AllocGeneral(reflect.Value{})
		default:
		}
		c.globalVariables[name.Name] = program.GlobalVariableInfo{Index: index, Kind: kind, IsIndirect: false}
	}
}

// compilePackageLevelVarInit emits bytecode to initialise each package-level variable in
// spec.
//
// Vars with explicit initialisers compile the expression; zero-value vars Emit
// isa.SubOpLoadZero + isa.OpSetGlobal so the varinit function can be re-run to reset
// globals between Execute calls.
//
// Takes spec: the value specification holding one or more package-level variables.
//
// Returns nil on success.
//
// Returns the first compilation error encountered while emitting an initialiser.
func (c *Compiler) compilePackageLevelVarInit(ctx context.Context, spec *ast.ValueSpec) error {
	if multiValueSpec(spec) {
		return c.compilePackageLevelMultiValue(ctx, spec)
	}
	for i, name := range spec.Names {
		if name.Name == typemap.BlankIdentName {
			if i < len(spec.Values) {
				if _, err := c.compileExpression(ctx, spec.Values[i]); err != nil {
					return err
				}
			}
			continue
		}
		gv, ok := c.globalVariables[name.Name]
		if !ok {
			continue
		}
		if err := c.compilePackageLevelVar(ctx, spec, i, name, gv); err != nil {
			return err
		}
	}
	return nil
}

// compilePackageLevelMultiValue initialises `var a, b = f()` at package level: the values
// land in locals of the init function through compileMultiValueSpec, then each one is
// stored to its global.
//
// Takes spec (*ast.ValueSpec) which is the declaration.
//
// Returns any compilation error.
func (c *Compiler) compilePackageLevelMultiValue(ctx context.Context, spec *ast.ValueSpec) error {
	c.Scopes.PushScope()
	defer c.Scopes.PopScope()
	if _, err := c.compileMultiValueSpec(ctx, spec); err != nil {
		return err
	}
	for _, name := range spec.Names {
		if name.Name == typemap.BlankIdentName {
			continue
		}
		gv, isGlobal := c.globalVariables[name.Name]
		location, found := c.Scopes.LookupVar(name.Name)
		if !isGlobal || !found {
			continue
		}
		c.emitSetGlobal(ctx, gv, location)
	}
	return nil
}

// compilePackageLevelVar emits the initialiser for a single package-level variable within
// spec.
//
// Takes spec: the value specification holding the variable's source declaration.
// Takes i: the index of the variable within spec.Names.
// Takes name: the identifier being initialised.
// Takes gv: the global slot information for the variable.
//
// Returns nil on success.
//
// Returns the compilation error from the initialiser expression.
func (c *Compiler) compilePackageLevelVar(ctx context.Context, spec *ast.ValueSpec, i int, name *ast.Ident, gv program.GlobalVariableInfo) error {
	if i < len(spec.Values) {
		valueLocation, err := c.compileExpression(ctx, spec.Values[i])
		if err != nil {
			return err
		}
		c.emitSetGlobal(ctx, gv, valueLocation)
		return nil
	}

	if gv.IsIndirect {
		return nil
	}
	if gv.Kind == isa.RegisterGeneral {
		c.emitGlobalZeroGeneral(ctx, name, gv)
		return nil
	}

	register := c.Scopes.Alloc.AllocTemp(gv.Kind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), register, uint8(gv.Kind))
	c.emitSetGlobalOp(ctx, register, gv)
	c.Scopes.Alloc.FreeTemp(gv.Kind, register)
	return nil
}

// emitGlobalZeroGeneral emits a zero-value initialiser for a isa.RegisterGeneral
// package-level variable.
//
// Prefers a named-type zero from the symbol registry before falling back to a composite
// (array/struct) zero reflect.Value.
//
// Takes name: the identifier being initialised.
// Takes gv: the global slot information for the variable.
func (c *Compiler) emitGlobalZeroGeneral(ctx context.Context, name *ast.Ident, gv program.GlobalVariableInfo) {
	typeObject := c.Info.Defs[name]
	if typeObject == nil {
		return
	}
	if c.symbols != nil {
		if zeroValue, ok := c.zeroValueForNamedType(ctx, typeObject.Type()); ok {
			if named, isNamed := types.Unalias(typeObject.Type()).(*types.Named); isNamed {
				c.emitGlobalGeneralConst(ctx, gv, zeroValue, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantNamedTypeZero,
					PackagePath: named.Obj().Pkg().Path(),
					SymbolName:  named.Obj().Name(), TypeDescriptor: descriptor.TypeDescriptor{}})
				return
			}
		}
	}
	if zeroValue, ok := c.zeroValueForCompositeType(ctx, typeObject.Type()); ok {
		c.emitGlobalGeneralConst(ctx, gv, zeroValue, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero,
			TypeDescriptor: descriptor.ReflectTypeToDescriptor(c.TypeToReflect(ctx, typeObject.Type())), PackagePath: "", SymbolName: ""})
	}
}

// emitGlobalGeneralConst adds value to the function's general constant pool and stores it
// into the supplied global slot.
//
// Takes gv (program.GlobalVariableInfo) which is the global slot for the destination
// variable.
// Takes value (reflect.Value) which is the value to register as a constant.
// Takes constantDescriptor (descriptor.GeneralConstantDescriptor) which classifies the
// constant for the runtime.
func (c *Compiler) emitGlobalGeneralConst(ctx context.Context, gv program.GlobalVariableInfo, value reflect.Value, constantDescriptor descriptor.GeneralConstantDescriptor) {
	register := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	constIndex, err := program.AddGeneralConstant(c.Function, value, constantDescriptor)
	if err != nil {
		c.recordStickyError(err)
		c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, register)
		return
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, register, constIndex)
	c.emitSetGlobalOp(ctx, register, gv)
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, register)
}

// emitGetGlobal loads the value of the global identified by gv into a freshly allocated
// register. An indirect global (gv.IsIndirect) is read through its pointer cell.
//
// Takes gv: the global slot information identifying the source global.
//
// Returns the VarLocation holding the loaded value, in the global's value bank.
func (c *Compiler) emitGetGlobal(_ context.Context, gv program.GlobalVariableInfo) program.VarLocation {
	if !gv.IsIndirect {
		return c.emitGetGlobalSlot(gv)
	}

	dest := c.Scopes.Alloc.Alloc(gv.Kind)
	cell := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	c.emitGetGlobalSlotInto(cell, gv)
	if gv.Kind == isa.RegisterGeneral {
		program.Emit(c.Function, isa.OpDeref, dest, cell, 0)
	} else {
		boxed := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpDeref, boxed, cell, 0)
		program.Emit(c.Function, isa.OpUnpackInterface, dest, boxed, uint8(gv.Kind))
		c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, boxed)
	}
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, cell)
	return program.VarLocation{Register: dest, Kind: gv.Kind}
}

// emitGetGlobalSlot loads the raw content of the slot identified by gv.
//
// Takes gv (GlobalVariableInfo) which identifies the source global.
//
// Returns the VarLocation holding the slot content, in the slot's bank.
func (c *Compiler) emitGetGlobalSlot(gv program.GlobalVariableInfo) program.VarLocation {
	bank := gv.SlotKind()
	dest := c.Scopes.Alloc.Alloc(bank)
	c.emitGetGlobalSlotInto(dest, gv)
	return program.VarLocation{Register: dest, Kind: bank}
}

// emitGetGlobalSlotInto loads the raw content of the slot identified by gv into dest, a
// register of the slot's bank (gv.SlotKind()).
//
// Takes dest (uint8) which receives the slot content.
// Takes gv: the global slot information identifying the source global.
func (c *Compiler) emitGetGlobalSlotInto(dest uint8, gv program.GlobalVariableInfo) {
	bank := gv.SlotKind()
	if gv.Index <= math.MaxUint8 {
		program.Emit(c.Function, isa.OpGetGlobal, dest, safeconv.MustIntToUint8(gv.Index), uint8(bank))
		return
	}
	program.EmitTier1(c.Function, isa.SubOpGetGlobalWide, dest, uint8(bank))
	program.EmitExtension(c.Function, safeconv.MustIntToUint16(gv.Index), 0)
}

// emitSetGlobal stores source into the global identified by gv.
//
// Inserts a bank coercion through a temporary register when source.Kind differs from
// gv.Kind. An indirect global (gv.IsIndirect) is written through its pointer cell.
//
// Takes gv: the global slot information identifying the destination global.
// Takes source: the source location holding the value to store.
func (c *Compiler) emitSetGlobal(ctx context.Context, gv program.GlobalVariableInfo, source program.VarLocation) {
	if gv.IsIndirect {
		c.emitSetIndirectGlobal(ctx, gv, source)
		return
	}
	if source.Kind != gv.Kind {
		temp := c.Scopes.Alloc.AllocTemp(gv.Kind)
		c.emitMove(ctx, program.VarLocation{Register: temp, Kind: gv.Kind}, source)
		c.emitSetGlobalOp(ctx, temp, gv)
		c.Scopes.Alloc.FreeTemp(gv.Kind, temp)
		return
	}
	c.emitSetGlobalOp(ctx, source.Register, gv)
}

// emitSetIndirectGlobal writes source through the pointer cell of an indirect global.
//
// The value is first moved into the global's own bank, then boxed and stored through the
// cell.
//
// Takes gv: the indirect global to write.
// Takes source: the source location holding the value to store.
func (c *Compiler) emitSetIndirectGlobal(ctx context.Context, gv program.GlobalVariableInfo, source program.VarLocation) {
	cell := c.emitGetGlobalSlot(gv)
	if source.Kind != gv.Kind {
		temp := c.Scopes.Alloc.AllocTemp(gv.Kind)
		c.emitMove(ctx, program.VarLocation{Register: temp, Kind: gv.Kind}, source)
		c.emitIndirectWrite(ctx, cell, program.VarLocation{Register: temp, Kind: gv.Kind, SourceType: source.SourceType})
		c.Scopes.Alloc.FreeTemp(gv.Kind, temp)
	} else {
		c.emitIndirectWrite(ctx, cell, source)
	}
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, cell.Register)
}

// emitSetGlobalOp emits the narrow or wide isa.OpSetGlobal instruction, writing the slot
// itself: the value of a plain global, the pointer cell of an indirect one.
//
// Selects the wide form when the global index does not fit in a uint8.
//
// Takes sourceRegister: the register supplying the value to store.
// Takes gv: the global slot information identifying the destination global.
func (c *Compiler) emitSetGlobalOp(_ context.Context, sourceRegister uint8, gv program.GlobalVariableInfo) {
	bank := gv.SlotKind()
	if gv.Index <= math.MaxUint8 {
		program.Emit(c.Function, isa.OpSetGlobal, sourceRegister, safeconv.MustIntToUint8(gv.Index), uint8(bank))
	} else {
		program.EmitTier1(c.Function, isa.SubOpSetGlobalWide, sourceRegister, uint8(bank))
		program.EmitExtension(c.Function, safeconv.MustIntToUint16(gv.Index), 0)
	}
}

// multiValueSpec reports whether spec declares several names from one multi-valued
// expression: `var a, b = f()`, `var v, ok = m[k]`, `var v, ok = x.(T)`, `var v, ok =
// <-ch`.
//
// Takes spec (*ast.ValueSpec) which is the declaration.
//
// Returns true for the one-value, many-names shape.
func multiValueSpec(spec *ast.ValueSpec) bool {
	return len(spec.Values) == 1 && len(spec.Names) > 1
}
