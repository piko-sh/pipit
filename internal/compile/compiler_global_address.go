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
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// markAddressTakenPackageVars records every package-level variable whose address the
// declarations take.
//
// Three forms take an address: explicit `&x`, a pointer-receiver method bound to the
// variable, and slicing an array variable.
//
// Takes decls ([]ast.Decl) which are scanned in full, function bodies included.
func (c *Compiler) markAddressTakenPackageVars(decls []ast.Decl) {
	if c.Info == nil {
		return
	}
	for _, declaration := range decls {
		ast.Inspect(declaration, c.markAddressTakenNode)
	}
}

// markAddressTakenNode is the ast.Inspect visitor behind markAddressTakenPackageVars: it
// marks the root package variable of an address-taking node and always descends.
//
// Takes node (ast.Node) which is the visited node.
//
// Returns bool which is always true so the walk continues.
func (c *Compiler) markAddressTakenNode(node ast.Node) bool {
	switch n := node.(type) {
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			c.markAddressTakenRoot(n.X)
		}
	case *ast.SelectorExpr:
		if c.selectorTakesReceiverAddress(n) {
			c.markAddressTakenRoot(n.X)
		}
	case *ast.SliceExpr:
		if isArrayValueType(c.Info.TypeOf(n.X)) {
			c.markAddressTakenRoot(n.X)
		}
	}
	return true
}

// selectorTakesReceiverAddress reports whether selector binds a pointer-receiver method
// to an addressable value operand, which Go compiles as `(&x).M`. A method reached
// through an embedded pointer, or on a pointer operand, addresses other storage.
//
// Takes selector (*ast.SelectorExpr) which is the method selection to classify.
//
// Returns bool which is true when the operand's own address is taken.
func (c *Compiler) selectorTakesReceiverAddress(selector *ast.SelectorExpr) bool {
	selection := c.Info.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal || selection.Indirect() {
		return false
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok {
		return false
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	if _, pointerReceiver := types.Unalias(signature.Recv().Type()).(*types.Pointer); !pointerReceiver {
		return false
	}
	_, operandIsPointer := types.Unalias(selection.Recv()).(*types.Pointer)
	return !operandIsPointer
}

// markAddressTakenRoot marks the package variable, if any, whose storage operand's
// address refers to.
//
// Takes operand (ast.Expr) which is the operand of `&`, a receiver, or a sliced array.
func (c *Compiler) markAddressTakenRoot(operand ast.Expr) {
	identifier := c.rootAddressableIdent(operand)
	if identifier == nil || !c.isPackageLevelVar(identifier) {
		return
	}
	if c.addressTakenGlobals == nil {
		c.addressTakenGlobals = make(map[string]bool)
	}
	c.addressTakenGlobals[identifier.Name] = true
}

// rootAddressableIdent walks field selections on struct values and index expressions on
// array values down to the variable whose storage an address refers to. A pointer, slice
// or map hop means the address belongs to other storage, and a composite literal or call
// result is a temporary, so those yield nil.
//
// Takes operand (ast.Expr) which is the addressable expression to walk.
//
// Returns *ast.Ident which is the root variable, or nil.
func (c *Compiler) rootAddressableIdent(operand ast.Expr) *ast.Ident {
	for {
		switch e := operand.(type) {
		case *ast.Ident:
			return e
		case *ast.ParenExpr:
			operand = e.X
		case *ast.SelectorExpr:
			selection := c.Info.Selections[e]
			if selection == nil || selection.Kind() != types.FieldVal || selection.Indirect() {
				return nil
			}
			if _, isPointer := types.Unalias(selection.Recv()).(*types.Pointer); isPointer {
				return nil
			}
			operand = e.X
		case *ast.IndexExpr:
			if !isArrayValueType(c.Info.TypeOf(e.X)) {
				return nil
			}
			operand = e.X
		default:
			return nil
		}
	}
}

// isPackageLevelVar reports whether identifier denotes a package-scope variable.
//
// Takes identifier (*ast.Ident) which is resolved through the type information.
//
// Returns bool which is true for a variable declared at package scope.
func (c *Compiler) isPackageLevelVar(identifier *ast.Ident) bool {
	variable, ok := c.Info.ObjectOf(identifier).(*types.Var)
	if !ok || variable.IsField() || variable.Pkg() == nil {
		return false
	}
	return variable.Parent() == variable.Pkg().Scope()
}

// compileAddressOfGlobal yields `&x` for an indirect package variable: the pointer cell
// its slot holds. A package variable the pre-pass did not see (a later session submission
// taking the address of an earlier one) reports false and takes the snapshot fallback.
//
// Takes identifier (*ast.Ident) which names the package variable.
//
// Returns the cell pointer location and true, or false when x is not an indirect global.
func (c *Compiler) compileAddressOfGlobal(identifier *ast.Ident) (program.VarLocation, bool) {
	gv, ok := c.globalVariables[identifier.Name]
	if !ok || !gv.IsIndirect || c.Info == nil || !c.isPackageLevelVar(identifier) {
		return program.VarLocation{}, false
	}
	return c.emitGetGlobalSlot(gv), true
}

// emitPackageVarPrologue runs at the start of the variable-initialisation function,
// before any initialiser.
//
// It allocates the pointer cell of every indirect package variable and, when go/types
// init order is used, stores the typed zero of every uninitialised general-bank variable.
//
// Takes files ([]*ast.File) whose declarations name the variables to prepare.
func (c *Compiler) emitPackageVarPrologue(ctx context.Context, files []*ast.File) {
	zeroValueless := c.Info != nil && len(c.Info.InitOrder) > 0
	forEachPackageVarName(files, func(spec *ast.ValueSpec, name *ast.Ident) {
		c.emitPackageVarPrologueFor(ctx, spec, name, zeroValueless)
	})
}

// emitPackageVarPrologueFor prepares one package variable's slot (see
// emitPackageVarPrologue).
//
// Takes spec (*ast.ValueSpec) which declares the variable.
// Takes name (*ast.Ident) which is the variable's declaring identifier.
// Takes zeroValueless (bool) which requests a typed zero for a general-bank variable
// declared without an initialiser.
func (c *Compiler) emitPackageVarPrologueFor(ctx context.Context, spec *ast.ValueSpec, name *ast.Ident, zeroValueless bool) {
	if name.Name == typemap.BlankIdentName {
		return
	}
	gv, ok := c.globalVariables[name.Name]
	if !ok {
		return
	}
	if gv.IsIndirect {
		c.emitIndirectGlobalCell(ctx, name, gv)
		return
	}
	if zeroValueless && gv.Kind == isa.RegisterGeneral && len(spec.Values) == 0 {
		c.emitGlobalZeroGeneral(ctx, name, gv)
	}
}

// emitIndirectGlobalCell allocates the heap cell of an indirect package variable and
// stores its pointer in the general-bank slot.
//
// The cell starts at the typed zero value and is placed on the Go heap so it outlives the
// initialisation frame.
//
// Takes name (*ast.Ident) which is the variable's declaring identifier.
// Takes gv (program.GlobalVariableInfo) which names the slot that receives the cell.
func (c *Compiler) emitIndirectGlobalCell(ctx context.Context, name *ast.Ident, gv program.GlobalVariableInfo) {
	typeObject := c.Info.Defs[name]
	if typeObject == nil {
		return
	}
	typeIndex, err := program.AddTypeRef(c.Function, c.TypeToReflect(ctx, typeObject.Type()))
	if err != nil {
		c.recordStickyError(err)
		return
	}
	zero := c.Scopes.Alloc.AllocTemp(gv.Kind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), zero, uint8(gv.Kind))
	cell := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAllocIndirect, cell, zero, uint8(gv.Kind))
	program.EmitExtension(c.Function, typeIndex, isa.AllocIndirectHeapCell)
	c.emitSetGlobalOp(ctx, cell, gv)
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, cell)
	c.Scopes.Alloc.FreeTemp(gv.Kind, zero)
}

// isArrayValueType reports whether t is an array type (not a pointer to one).
//
// Takes t (types.Type) which may be nil.
//
// Returns bool which is true for array types.
func isArrayValueType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := types.Unalias(t).Underlying().(*types.Array)
	return ok
}

// forEachPackageVarName calls visit for every name declared by a package-level var spec
// in files, in source order.
//
// Takes files ([]*ast.File) whose declarations are scanned.
// Takes visit (func) which receives each spec and declared name.
func forEachPackageVarName(files []*ast.File, visit func(spec *ast.ValueSpec, name *ast.Ident)) {
	for _, file := range files {
		forEachVarSpec(file.Decls, func(spec *ast.ValueSpec) {
			for _, name := range spec.Names {
				visit(spec, name)
			}
		})
	}
}

// forEachVarSpec calls visit for every spec of every var declaration group in decls.
//
// Takes decls ([]ast.Decl) which are scanned in order.
// Takes visit (func) which receives each var spec.
func forEachVarSpec(decls []ast.Decl, visit func(spec *ast.ValueSpec)) {
	for _, declaration := range decls {
		genDecl, ok := declaration.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			if valueSpec, ok := spec.(*ast.ValueSpec); ok {
				visit(valueSpec)
			}
		}
	}
}
