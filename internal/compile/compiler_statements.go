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
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"reflect"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

const (

	// compileFunctionWrapFormat is the format string used to wrap errors that surface while
	// compiling a named function declaration.
	compileFunctionWrapFormat = "compiling %s: %w"
)

// deferrableBuiltinIDs maps the builtins the spec allows as defer and go statements to
// the identifiers the engine dispatches on. The others (len, append, make, new, complex,
// real, imag, min, max, unsafe.*) are rejected by go/types before compilation.
var deferrableBuiltinIDs = map[string]uint8{
	"close":   isa.BuiltinClose,
	"copy":    isa.BuiltinCopy,
	"delete":  isa.BuiltinDelete,
	"panic":   isa.BuiltinPanic,
	"print":   isa.BuiltinPrint,
	"println": isa.BuiltinPrintln,
	"recover": isa.BuiltinRecover,
	"clear":   isa.BuiltinClear,
}

// compileFunctionDecl compiles a function declaration into a CompiledFunction and
// registers it in the function table.
//
// Takes declaration (*ast.FuncDecl) which is the AST function declaration to compile and
// register.
//
// Returns an error if registration or body compilation fails.
func (c *Compiler) compileFunctionDecl(ctx context.Context, declaration *ast.FuncDecl) error {
	compiledFunction, err := c.registerFunctionDecl(ctx, declaration)
	if err != nil {
		return fmt.Errorf("compiling function declaration: %w", err)
	}
	if err := c.compileFunctionBody(ctx, declaration, compiledFunction); err != nil {
		return fmt.Errorf("compiling function declaration: %w", err)
	}
	return nil
}

// registerFunctionDecl pre-registers a function declaration in the function table without
// compiling its body, ensuring all functions are visible before any bodies are compiled.
//
// Takes declaration (*ast.FuncDecl) which is the AST function declaration to register.
//
// Returns the stub CompiledFunction for later body compilation, or an error if the
// declaration cannot be resolved.
func (c *Compiler) registerFunctionDecl(ctx context.Context, declaration *ast.FuncDecl) (*program.CompiledFunction, error) {
	fnObj := c.Info.Defs[declaration.Name]
	if fnObj == nil {
		return nil, fmt.Errorf("undefined function: %s at %s", declaration.Name.Name, c.positionString(declaration.Name.Pos()))
	}
	signature, ok := fnObj.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("not a function: %s (type %T) at %s", declaration.Name.Name, fnObj.Type(), c.positionString(declaration.Name.Pos()))
	}

	tableName := methodTableName(declaration)
	compiledFunction := buildFunctionDeclStub(ctx, c, declaration, signature, tableName)
	root := c.RootFunction
	index := safeconv.MustIntToUint16(len(root.Functions))
	root.Functions = append(root.Functions, compiledFunction)
	c.recordFunctionDeclIndex(declaration, tableName, index)

	if signature.Recv() != nil {
		c.registerMethodReceiver(ctx, signature, tableName, index)
	}
	return compiledFunction, nil
}

// recordFunctionDeclIndex routes a registered function index into a table.
//
// The destination is initFunctionIndices for package-level `init()` only or the
// per-package functionTable otherwise. The receiver guard prevents `init()` methods on
// user types from being mis-classified as package-init entries, which would otherwise
// derail subsequent pointer-receiver method dispatch by invoking them with no receiver
// (see yaml.v3 parser.init).
//
// Takes declaration (*ast.FuncDecl) which is the parsed declaration.
// Takes tableName (string) which is the function-table key.
// Takes index (uint16) which is the rootFunction.functions index.
func (c *Compiler) recordFunctionDeclIndex(declaration *ast.FuncDecl, tableName string, index uint16) {
	if declaration.Name.Name == InitFunctionName && declaration.Recv == nil {
		c.initFunctionIndices = append(c.initFunctionIndices, index)
		return
	}
	if c.functionTable == nil {
		c.functionTable = make(map[string]uint16)
	}
	c.functionTable[tableName] = index
	if declaration.Recv == nil && c.functionDeclarations != nil {
		c.functionDeclarations[tableName] = declaration
	}
}

// registerMethodReceiver registers a method in the root function's method table and
// records the receiver's reflect type name.
//
// Takes signature (*types.Signature) which is the method signature containing the
// receiver type.
// Takes tableName (string) which is the method table key in "ReceiverType.MethodName"
// format.
// Takes index (uint16) which is the position of the compiled function in the root
// function table.
func (c *Compiler) registerMethodReceiver(ctx context.Context, signature *types.Signature, tableName string, index uint16) {
	root := c.RootFunction
	if err := program.RegisterMethod(root, tableName, index); err != nil {
		c.recordStickyError(err)
		return
	}
	if root.MethodSignatures == nil {
		root.MethodSignatures = make(map[string]string)
	}
	root.MethodSignatures[tableName] = program.SignatureShapeString(signature)

	receiverType := signature.Recv().Type()
	if pointer, ok := receiverType.(*types.Pointer); ok {
		receiverType = pointer.Elem()
	}
	named, ok := receiverType.(*types.Named)
	if !ok {
		return
	}

	reflectType := c.TypeToReflect(ctx, named)
	if root.TypeNames == nil {
		root.TypeNames = make(map[reflect.Type]string)
	}
	if previous, exists := root.TypeNames[reflectType]; exists && previous != named.Obj().Name() {
		logging.LoggerFrom(ctx).Debug("named types share a runtime type; interface dispatch cannot tell them apart",
			slog.String("type", named.Obj().Name()),
			slog.String("previous", previous),
			slog.String("runtimeType", reflectType.String()))
	}
	root.TypeNames[reflectType] = named.Obj().Name()
	if _, isBasic := named.Underlying().(*types.Basic); isBasic {
		if poolType := c.namedScalarBoxType(named); poolType != nil {
			root.TypeNames[poolType] = named.Obj().Name()
		}
	}
}

// compileFunctionBody compiles the body of a registered function declaration into the
// given CompiledFunction.
//
// Takes declaration (*ast.FuncDecl) which is the AST function declaration whose body is
// compiled.
// Takes compiledFunction (*CompiledFunction) which is the target CompiledFunction to Emit
// bytecode into.
//
// Returns an error if the body compilation fails.
func (c *Compiler) compileFunctionBody(ctx context.Context, declaration *ast.FuncDecl, compiledFunction *program.CompiledFunction) error {
	sub := newFunctionCompiler(ctx, c.programContext, compiledFunction, functionOptions{
		scopeName:         declaration.Name.Name,
		upvalues:          nil,
		substitutions:     nil,
		substitutionCache: nil,
		rangeOverFunction: nil,
	})
	sub.prepareBody(bodySpec{body: declaration.Body, params: declaration.Type.Params, resultTypes: resolveResultTypes(c.Info, declaration)})

	c.compileFunctionParams(ctx, sub, declaration)
	sub.declareNamedResults(ctx, declaration.Type.Results, compiledFunction)

	if _, err := sub.compileStmtList(ctx, declaration.Body.List); err != nil {
		return fmt.Errorf(compileFunctionWrapFormat, declaration.Name.Name, err)
	}
	if err := sub.finishBody(ctx, compiledFunction, declaration.Body, finishFlags{tailCall: true, finaliseDefer: true, classifyEscapes: true}); err != nil {
		return fmt.Errorf(compileFunctionWrapFormat, declaration.Name.Name, err)
	}

	return nil
}

// compileSpecialisedBody populates a pre-registered specialisation stub with a
// monomorphic body for a generic function.
//
// Takes genericCF (*CompiledFunction) which is the original generic function.
// Takes subs (map[*types.TypeParam]types.Type) which is the type substitution map for
// this specialisation.
// Takes specFunctionIndex (uint16) which is the index of the pre-allocated specialised
// CompiledFunction in rootFunction.functions.
//
// Returns nil on success or an error describing the body compilation failure.
func (c *Compiler) compileSpecialisedBody(
	ctx context.Context,
	genericCF *program.CompiledFunction,
	subs map[*types.TypeParam]types.Type,
	specFunctionIndex uint16,
) error {
	declaration := genericCF.GenericDeclaration
	if declaration == nil {
		return fmt.Errorf("generic function %s has no AST declaration retained", genericCF.Name)
	}

	specCF := c.RootFunction.Functions[specFunctionIndex]
	cache := map[types.Type]types.Type{}
	c.populateSpecialisationStub(ctx, specCF, genericCF, subs, cache)
	sub := c.buildSpecialisationSubCompiler(ctx, specCF, declaration, subs, cache)

	escape.ApplySpecialisationTypedSliceSurvivors(sub.EscapeContext(), declaration, specCF)

	c.compileFunctionParams(ctx, sub, declaration)
	sub.declareNamedResults(ctx, declaration.Type.Results, specCF)

	if _, err := sub.compileStmtList(ctx, declaration.Body.List); err != nil {
		return fmt.Errorf("compiling specialisation %s: %w", specCF.Name, err)
	}
	if err := sub.finishBody(ctx, specCF, declaration.Body, finishFlags{tailCall: true, finaliseDefer: true, classifyEscapes: true}); err != nil {
		return fmt.Errorf("compiling specialisation %s: %w", specCF.Name, err)
	}

	return nil
}

// populateSpecialisationStub fills in metadata, parameter and result kind tables on a
// specialised CompiledFunction stub by walking the generic callee's parameterTypeRefs /
// resultTypeRefs through the active substitution map. The cache is shared with the
// sub-Compiler so type substitution memoises across the body emission too.
//
// Takes specCF (*CompiledFunction) which is the stub to populate.
// Takes genericCF (*CompiledFunction) which is the generic origin.
// Takes subs (map[*types.TypeParam]types.Type) which is the type-args substitution map.
// Takes cache (map[types.Type]types.Type) which memoises walks.
func (c *Compiler) populateSpecialisationStub(ctx context.Context, specCF, genericCF *program.CompiledFunction, subs map[*types.TypeParam]types.Type, cache map[types.Type]types.Type) {
	specCF.SpecialisationOrigin = genericCF
	specCF.IsVariadic = genericCF.IsVariadic
	specCF.VariadicSliceType = genericCF.VariadicSliceType
	specCF.IsPointerReceiver = genericCF.IsPointerReceiver
	specCF.HasReceiver = genericCF.HasReceiver
	specCF.Name = genericCF.Name + "[spec:" + specialisationSuffix(subs, genericCF.GenericTypeParams) + "]"

	promotion := &typemap.KindPromotionContext{
		Substitutions:     subs,
		SubstitutionCache: cache,

		Disqualified:          nil,
		BindingName:           "",
		CalleeParamPromotions: nil,
		CalleeParamIndex:      0,
	}
	for index, t := range genericCF.ParameterTypeRefs {
		substituted := typemap.SubstituteType(t, subs, cache)
		paramKind, _ := typemap.KindForPromotedSlot(substituted, promotion)
		if index == 0 && genericCF.HasReceiver && index < len(genericCF.ParameterKinds) {
			paramKind = genericCF.ParameterKinds[index]
		}
		specCF.ParameterKinds = append(specCF.ParameterKinds, paramKind)
		specCF.ParameterIsGeneric = append(specCF.ParameterIsGeneric, false)
		specCF.ParameterTypeRefs = append(specCF.ParameterTypeRefs, substituted)

		specCF.ParameterTypedSlicePromoted = append(specCF.ParameterTypedSlicePromoted, isa.IsTypedSliceKind(paramKind))
	}
	for _, t := range genericCF.ResultTypeRefs {
		substituted := typemap.SubstituteType(t, subs, cache)
		resultKind, _ := typemap.KindForPromotedSlot(substituted, promotion)
		specCF.ResultKinds = append(specCF.ResultKinds, resultKind)
		specCF.ResultTypeRefs = append(specCF.ResultTypeRefs, substituted)
		specCF.ResultReflectTypes = append(specCF.ResultReflectTypes, c.exactReflectTypeForBoxing(substituted))
	}
	specCF.SignatureReflectType = c.specialisedSignatureReflectType(ctx, specCF)
}

// specialisedSignatureReflectType builds a specialisation's static func type from its
// substituted parameter and result type refs, skipping the receiver slot.
//
// Takes specCF (*program.CompiledFunction) which is the populated specialisation stub.
//
// Returns reflect.Type which is the func type, or nil when a type is not convertible.
func (c *Compiler) specialisedSignatureReflectType(ctx context.Context, specCF *program.CompiledFunction) reflect.Type {
	parameterRefs := specCF.ParameterTypeRefs
	if specCF.HasReceiver && len(parameterRefs) > 0 {
		parameterRefs = parameterRefs[1:]
	}
	in := make([]reflect.Type, 0, len(parameterRefs))
	for _, ref := range parameterRefs {
		if ref == nil || typemap.ContainsTypeParameter(ref) {
			return nil
		}
		in = append(in, c.TypeToReflect(ctx, ref))
	}
	out := make([]reflect.Type, 0, len(specCF.ResultTypeRefs))
	for _, ref := range specCF.ResultTypeRefs {
		if ref == nil || typemap.ContainsTypeParameter(ref) {
			return nil
		}
		out = append(out, c.TypeToReflect(ctx, ref))
	}
	if specCF.IsVariadic && (len(in) == 0 || in[len(in)-1].Kind() != reflect.Slice) {
		return nil
	}
	return reflect.FuncOf(in, out, specCF.IsVariadic)
}

// buildSpecialisationSubCompiler constructs a sub-Compiler primed with the active
// substitution map, debug propagation hook, and closure-capture analysis for emitting a
// specialised body.
//
// Takes specCF (*CompiledFunction) which is the stub being filled.
// Takes declaration (*ast.FuncDecl) which is the generic declaration whose body the
// sub-Compiler will walk.
// Takes subs (map[*types.TypeParam]types.Type) which is the substitution map for
// type-args.
// Takes cache (map[types.Type]types.Type) which memoises substitution across the body's
// expressions.
//
// Returns a fully primed sub-Compiler ready for compileStmtList.
func (c *Compiler) buildSpecialisationSubCompiler(
	ctx context.Context,
	specCF *program.CompiledFunction,
	declaration *ast.FuncDecl,
	subs map[*types.TypeParam]types.Type,
	cache map[types.Type]types.Type,
) *Compiler {
	sub := newFunctionCompiler(ctx, c.programContext, specCF, functionOptions{
		scopeName:         specCF.Name,
		upvalues:          nil,
		substitutions:     subs,
		substitutionCache: cache,
		rangeOverFunction: nil,
	})
	sub.prepareBody(bodySpec{body: declaration.Body, params: declaration.Type.Params, resultTypes: resolveResultTypes(c.Info, declaration)})
	return sub
}

// compileFunctionParams declares receiver and parameter variables in the sub-Compiler's
// scope.
//
// Takes sub (*Compiler) which is the sub-Compiler whose scope receives the variable
// declarations.
// Takes declaration (*ast.FuncDecl) which is the function declaration containing the
// parameter list.
func (c *Compiler) compileFunctionParams(ctx context.Context, sub *Compiler, declaration *ast.FuncDecl) {
	recordParam := func(location program.VarLocation) {
		if sub.Function == nil {
			return
		}
		sub.Function.ParameterRegisters = append(sub.Function.ParameterRegisters, location.Register)
	}

	c.declareFunctionReceiver(ctx, sub, declaration, recordParam)
	if declaration.Type.Params == nil {
		return
	}
	parameterPosition := 0
	if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
		parameterPosition = 1
	}
	for _, field := range declaration.Type.Params.List {
		if len(field.Names) == 0 {
			parameterPosition = c.declareAnonymousParameter(sub, field.Type, parameterPosition, recordParam)
			continue
		}
		for _, name := range field.Names {
			parameterPosition = c.declareFunctionParameter(ctx, sub, name, parameterPosition, recordParam)
		}
	}
}

// declareAnonymousParameter reserves the register of an unnamed parameter (`func f(int,
// bool)`): the caller still copies an argument into the slot, so the position must be
// taken even though the body cannot refer to it.
//
// Takes sub (*Compiler) which compiles the function body.
// Takes fieldType (ast.Expr) which is the parameter's type expression.
// Takes parameterPosition (int) which is the parameter's index.
// Takes recordParam (func(VarLocation)) which records the register.
//
// Returns the next parameter position.
func (c *Compiler) declareAnonymousParameter(sub *Compiler, fieldType ast.Expr, parameterPosition int, recordParam func(program.VarLocation)) int {
	kind := isa.RegisterGeneral
	if sub.Function != nil && parameterPosition < len(sub.Function.ParameterKinds) {
		kind = sub.Function.ParameterKinds[parameterPosition]
	} else if paramType := c.Info.TypeOf(fieldType); paramType != nil {
		kind = sub.kindFor(paramType)
	}
	register := sub.Scopes.Alloc.Alloc(kind)
	recordParam(program.VarLocation{Register: register, Kind: kind})
	return parameterPosition + 1
}

// declareFunctionReceiver declares the receiver variable for a method.
//
// A named receiver is added to the scope with a general-bank slot and is candidate for
// heap promotion when captured; a blank receiver still reserves the slot so the call ABI
// keeps its parameter indices stable.
//
// Takes sub (*Compiler) which is the body-compiling sub-Compiler.
// Takes declaration (*ast.FuncDecl) which is the parsed declaration.
// Takes recordParam (func(VarLocation)) which captures each slot for the call ABI.
func (*Compiler) declareFunctionReceiver(ctx context.Context, sub *Compiler, declaration *ast.FuncDecl, recordParam func(program.VarLocation)) {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return
	}
	field := declaration.Recv.List[0]
	if len(field.Names) > 0 {
		location := sub.Scopes.DeclareVar(field.Names[0].Name, isa.RegisterGeneral)
		recordParam(location)
		sub.tryHeapPromoteCapturedLocal(ctx, field.Names[0].Name, field.Names[0])
		return
	}
	register := sub.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	recordParam(program.VarLocation{Register: register, Kind: isa.RegisterGeneral})
}

// declareFunctionParameter declares one named parameter.
//
// Records the register location and returns the next parameter position. The recorded
// location is captured BEFORE tryHeapPromoteCapturedLocal so the prologue's
// isa.OpAllocIndirect reads from the caller-visible slot. See BuildCallArgCopyProgram,
// which looks parameters up by this index rather than restarting per-bank counters from
// 0.
//
// Takes sub (*Compiler) which is the body-compiling sub-Compiler.
// Takes name (*ast.Ident) which names the parameter.
// Takes parameterPosition (int) which is the slot index entering the call.
// Takes recordParam (func(VarLocation)) which captures the slot for the call ABI.
//
// Returns int which is the slot index after declaring the parameter.
func (c *Compiler) declareFunctionParameter(ctx context.Context, sub *Compiler, name *ast.Ident, parameterPosition int, recordParam func(program.VarLocation)) int {
	typeObject := c.Info.Defs[name]
	if typeObject == nil {
		return parameterPosition + 1
	}
	kind := sub.kindFor(typeObject.Type())
	if sub.Function != nil && parameterPosition < len(sub.Function.ParameterKinds) {
		kind = sub.Function.ParameterKinds[parameterPosition]
	}
	location := sub.Scopes.DeclareVar(name.Name, kind)
	recordParam(location)
	sub.tryHeapPromoteCapturedLocal(ctx, name.Name, name)
	return parameterPosition + 1
}

// declareNamedResults declares named return values as zero-initialised locals.
//
// Takes results (*ast.FieldList) which is the function's result list, or nil.
// Takes compiledFunction (*CompiledFunction) which receives the named-result locations.
func (c *Compiler) declareNamedResults(ctx context.Context, results *ast.FieldList, compiledFunction *program.CompiledFunction) {
	if results == nil || compiledFunction == nil {
		return
	}
	if resultsHaveNames(results) {
		c.reserveCanonicalResultSlots(compiledFunction)
	}
	position := 0
	for _, field := range results.List {
		for _, name := range field.Names {
			c.declareNamedResult(ctx, compiledFunction, name, position)
			position++
		}
	}
	if len(compiledFunction.NamedResultLocations) == 0 {
		c.declareSyntheticResults(ctx, compiledFunction)
	}
}

// reserveCanonicalResultSlots keeps each bank's first registers clear of named and
// synthetic result variables.
//
// This ensures result i of a bank never lives in another result's canonical return slot.
//
// Takes compiledFunction (*CompiledFunction) whose ResultKinds give the slot counts.
func (c *Compiler) reserveCanonicalResultSlots(compiledFunction *program.CompiledFunction) {
	var counts [isa.NumRegisterKinds]uint32
	for _, kind := range compiledFunction.ResultKinds {
		counts[kind]++
	}
	for kind, count := range counts {
		if count > 0 {
			c.Scopes.Alloc.ReserveLow(isa.RegisterKind(kind), count)
		}
	}
}

// declareNamedResult declares one named result. A blank name still occupies its position
// (the named-result protocol indexes results by position), so it receives a synthetic
// local like the ones a defer-containing function declares for unnamed results.
//
// Takes compiledFunction (*CompiledFunction) which receives the location and name.
// Takes name (*ast.Ident) which is the result's identifier, possibly blank.
// Takes position (int) which is the result's index in the signature.
func (c *Compiler) declareNamedResult(ctx context.Context, compiledFunction *program.CompiledFunction, name *ast.Ident, position int) {
	if name.Name == "" || name.Name == typemap.BlankIdentName {
		if position < len(compiledFunction.ResultKinds) {
			c.declareSyntheticResult(ctx, compiledFunction, position, compiledFunction.ResultKinds[position])
		}
		return
	}
	typeObject := c.Info.Defs[name]
	if typeObject == nil {
		return
	}
	kind := c.kindForCallSlot(typeObject.Type())
	location := c.Scopes.DeclareVar(name.Name, kind)
	compiledFunction.NamedResultLocations = append(compiledFunction.NamedResultLocations, location)
	compiledFunction.NamedResultNames = append(compiledFunction.NamedResultNames, name.Name)
	c.emitNamedResultZero(ctx, compiledFunction, location, typeObject)
	if c.closureCapturedNames[name.Name] {
		if c.heapPromotedNames == nil {
			c.heapPromotedNames = map[string]bool{}
		}
		c.heapPromotedNames[name.Name] = true
	}
	c.tryHeapPromoteCapturedLocal(ctx, name.Name, name)
}

// declareSyntheticResults gives a defer-containing function with unnamed results a hidden
// named result per slot.
//
// The named-result protocol (zero at entry, store at return, syncNamedResults after the
// deferred calls, deliverRecoveredReturn after a recover) carries the values. Without
// them a recovered panic would deliver whatever scratch the return slots held. Functions
// without defers keep the cheaper unnamed protocol.
//
// Takes compiledFunction (*CompiledFunction) which receives the synthetic locations and
// names.
func (c *Compiler) declareSyntheticResults(ctx context.Context, compiledFunction *program.CompiledFunction) {
	if !c.hasDefers || len(compiledFunction.ResultKinds) == 0 {
		return
	}
	c.reserveCanonicalResultSlots(compiledFunction)
	for index, kind := range compiledFunction.ResultKinds {
		c.declareSyntheticResult(ctx, compiledFunction, index, kind)
	}
}

// declareSyntheticResult declares the hidden named result for result position index.
//
// Takes compiledFunction (*CompiledFunction) which receives the location and name.
// Takes index (int) which is the result's position in the signature.
// Takes kind (isa.RegisterKind) which is the result's register bank.
func (c *Compiler) declareSyntheticResult(ctx context.Context, compiledFunction *program.CompiledFunction, index int, kind isa.RegisterKind) {
	name := scope.SyntheticNamePrefix + "r" + strconv.Itoa(index)
	location := c.Scopes.DeclareVar(name, kind)
	compiledFunction.NamedResultLocations = append(compiledFunction.NamedResultLocations, location)
	compiledFunction.NamedResultNames = append(compiledFunction.NamedResultNames, name)
	var typeObject types.Object
	if index < len(c.currentResultTypes) && c.currentResultTypes[index] != nil {
		typeObject = types.NewVar(token.NoPos, nil, name, c.currentResultTypes[index])
	}
	c.emitNamedResultZero(ctx, compiledFunction, location, typeObject)
}

// emitNamedResultZero zero-initialises a named result at function entry, through a
// scratch register when the result lives in a spill slot. A general-bank result takes the
// typed zero of its declared type (an array or struct result must be a real value the
// body can slice or index), which emitLocalZeroValue selects.
//
// Takes compiledFunction (*CompiledFunction) which receives the instructions.
// Takes location (VarLocation) which is the named result's location.
// Takes typeObject (types.Object) which carries the result's type; nil falls back to the
// bank's untyped zero.
func (c *Compiler) emitNamedResultZero(ctx context.Context, compiledFunction *program.CompiledFunction, location program.VarLocation, typeObject types.Object) {
	kind := location.Kind
	if kind == isa.RegisterGeneral && typeObject != nil && !location.IsSpilled {
		c.emitLocalZeroValue(ctx, typeObject, location)
		return
	}
	if !location.IsSpilled {
		program.Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), location.Register, uint8(kind))
		return
	}
	scratch := c.Scopes.Alloc.AllocTemp(kind)
	program.Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), scratch, uint8(kind))
	program.Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpSpill), scratch, uint8(kind))
	program.EmitExtension(compiledFunction, location.SpillSlot, 0)
	c.Scopes.Alloc.FreeTemp(kind, scratch)
}

// compileIndexAssign compiles an index assignment: a[i] = v.
//
// Takes target (*ast.IndexExpr) which is the index expression on the left-hand side of
// the assignment.
// Takes valueLocation (VarLocation) which is the register location holding the value to
// assign.
//
// Returns an error if the collection or index expressions fail to compile.
func (c *Compiler) compileIndexAssign(ctx context.Context, target *ast.IndexExpr, valueLocation program.VarLocation) error {
	if _, ok := c.tryCompileFusedArrayFieldIndex(ctx, target, true, valueLocation); ok {
		return nil
	}
	if _, ok := c.tryCompileDerefSliceIndex(ctx, target, true, valueLocation); ok {
		return nil
	}
	collectionLocation, err := c.compileExpression(ctx, target.X)
	if err != nil {
		return fmt.Errorf("compiling index assignment: %w", err)
	}
	indexLocation, err := c.compileExpression(ctx, target.Index)
	if err != nil {
		return fmt.Errorf("compiling index assignment: %w", err)
	}
	return c.compileIndexAssignFrom(ctx, target, collectionLocation, indexLocation, valueLocation)
}

// compileIndexAssignFrom stores valueLocation into target with its collection and index
// already compiled, which lets a prepared target evaluate them before the value.
//
// Takes target (*ast.IndexExpr) which is the index expression being assigned.
// Takes collectionLocation (program.VarLocation) which holds the collection.
// Takes indexLocation (program.VarLocation) which holds the index or key.
// Takes valueLocation (program.VarLocation) which holds the value.
//
// Returns error when the target's type is unknown.
func (c *Compiler) compileIndexAssignFrom(ctx context.Context, target *ast.IndexExpr, collectionLocation, indexLocation, valueLocation program.VarLocation) error {
	c.setDebugPosition(ctx, target.Lbrack)
	collectionType, ok := c.underlyingTypeOf(target.X)
	if _, isMap := collectionType.(*types.Map); ok && !isMap {
		c.ensureIntRegister(ctx, &indexLocation)
	}
	if !ok {
		return fmt.Errorf("%w: missing type information for index assignment target at %s", fault.ErrCompilation, c.positionString(target.X.Pos()))
	}
	if mapType, isMap := collectionType.(*types.Map); isMap {
		c.compileIndexAssignMap(ctx, mapType, collectionLocation, indexLocation, valueLocation)
		return nil
	}
	if c.tryCompileIndexAssignTypedSlice(ctx, collectionType, collectionLocation, indexLocation, valueLocation) {
		return nil
	}
	if isa.IsTypedSliceKind(collectionLocation.Kind) {
		c.boxToGeneralTemp(ctx, &collectionLocation)
	}

	c.boxElementToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpIndexSet, collectionLocation.Register, indexLocation.Register, valueLocation.Register)
	return nil
}

// compileIndexAssignMap emits the map-set path for `m[k] = v`. Picks the typed-map
// fast-path opcode when the key/value/element register kinds match an exact-typed map
// opcode; otherwise boxes to general and emits the generic isa.OpMapSet.
//
// Takes mapType (*types.Map) which is the static type of the map being written.
// Takes collectionLocation (VarLocation) which holds the map register.
// Takes indexLocation (VarLocation) which holds the key register.
// Takes valueLocation (VarLocation) which holds the value register.
func (c *Compiler) compileIndexAssignMap(
	ctx context.Context,
	mapType *types.Map,
	collectionLocation, indexLocation, valueLocation program.VarLocation,
) {
	keyKind := c.kindFor(mapType.Key())
	valueKind := c.kindFor(mapType.Elem())
	if op, ok := selectTypedMapSetOpcode(keyKind, valueKind, indexLocation.Kind, valueLocation.Kind); ok {
		c.EmitTyped(ctx, op, collectionLocation, indexLocation, valueLocation)
		return
	}
	c.boxElementToGeneralTemp(ctx, &indexLocation)
	c.boxElementToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpMapSet, collectionLocation.Register, indexLocation.Register, valueLocation.Register)
}

// tryCompileIndexAssignTypedSlice emits a typed-slice fast-path `slice[i] = v` when the
// collection's element kind matches the value kind.
//
// Takes collectionType (types.Type) which is the static type of the slice being indexed.
// Takes collectionLocation (VarLocation) which holds the slice register.
// Takes indexLocation (VarLocation) which holds the integer index register.
// Takes valueLocation (VarLocation) which holds the value register.
//
// Returns true when the fast path was emitted; the caller falls back to the generic
// isa.OpIndexSet path on false.
func (c *Compiler) tryCompileIndexAssignTypedSlice(
	ctx context.Context,
	collectionType types.Type,
	collectionLocation, indexLocation, valueLocation program.VarLocation,
) bool {
	elementRegisterKind, ok := c.sliceElemRegisterKind(collectionType)
	if !ok || indexLocation.Kind != isa.RegisterInt {
		return false
	}
	if elementRegisterKind == isa.RegisterBool && valueLocation.Kind == isa.RegisterInt {
		booleanRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterBool)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpIntToBool), booleanRegister, valueLocation.Register)
		valueLocation = program.VarLocation{Register: booleanRegister, Kind: isa.RegisterBool}
	}
	if valueLocation.Kind != elementRegisterKind {
		return false
	}
	plan, ok := isaselect.PlanSliceSet(collectionLocation.Kind, elementRegisterKind)
	if !ok {
		return false
	}
	c.emitSliceSet(ctx, plan, collectionLocation, indexLocation, valueLocation)
	return true
}

// compileSelectorAssign compiles a struct field assignment: s.Field = value.
//
// Takes target (*ast.SelectorExpr) which is the selector expression identifying the
// struct field.
// Takes valueLocation (VarLocation) which is the register location holding the value to
// assign.
//
// Returns an error if the receiver expression fails to compile or the selector is
// unresolved.
func (c *Compiler) compileSelectorAssign(ctx context.Context, target *ast.SelectorExpr, valueLocation program.VarLocation) error {
	if handled, err := c.compileNativePackageVarAssign(ctx, target, valueLocation); handled {
		return err
	}
	receiverLocation, err := c.compileExpression(ctx, target.X)
	if err != nil {
		return fmt.Errorf("compiling selector assignment: %w", err)
	}
	c.boxToGeneral(ctx, &receiverLocation)
	return c.compileSelectorAssignFrom(ctx, target, receiverLocation, valueLocation)
}

// compileSelectorAssignFrom stores valueLocation into the field target selects on an
// already compiled, general-bank receiver.
//
// Takes target (*ast.SelectorExpr) which is the field expression being assigned.
// Takes receiverLocation (program.VarLocation) which holds the receiver.
// Takes valueLocation (program.VarLocation) which holds the value.
//
// Returns error when the selector is unresolved.
func (c *Compiler) compileSelectorAssignFrom(ctx context.Context, target *ast.SelectorExpr, receiverLocation, valueLocation program.VarLocation) error {
	selection := c.Info.Selections[target]
	if selection == nil {
		return fmt.Errorf("unresolved selector: %s", target.Sel.Name)
	}

	if c.tryCompileSelectorAssignFastPath(ctx, selection, receiverLocation, valueLocation) {
		return nil
	}

	index := selection.Index()
	if len(index) > 1 {
		return c.compileEmbeddedFieldWrite(ctx, receiverLocation, target, index, valueLocation)
	}

	c.boxElementToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpSetField, receiverLocation.Register, safeconv.MustIntToUint8(index[len(index)-1]), valueLocation.Register)
	return nil
}

// compileEmbeddedFieldWrite writes a value into a promoted field, walking the selection
// path to the leaf.
//
// Takes receiverLocation (VarLocation) which holds the lowered receiver.
// Takes target (*ast.SelectorExpr) which is the promoted field selector.
// Takes index ([]int) which is the selection path to the leaf field.
// Takes valueLocation (VarLocation) which holds the value to store.
//
// Returns error when the field write cannot be compiled.
func (c *Compiler) compileEmbeddedFieldWrite(ctx context.Context, receiverLocation program.VarLocation, target *ast.SelectorExpr, index []int, valueLocation program.VarLocation) error {
	parentLocation := c.compileEmbeddedParentPointerReusing(receiverLocation, target.X, index)
	leafFieldIndex := safeconv.MustIntToUint8(index[len(index)-1])

	c.boxElementToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpSetField, parentLocation.Register, leafFieldIndex, valueLocation.Register)
	return nil
}

// tryCompileSelectorAssignFastPath attempts to Emit the unsafe-pointer fast path for
// `s.Field = value`.
//
// Takes selection (*types.Selection) which is the resolved selector targeting the struct
// field.
// Takes receiverLocation (VarLocation) which holds the receiver register.
// Takes valueLocation (VarLocation) which holds the value register.
//
// Returns true when an op was emitted and the caller should not fall back to the generic
// isa.OpSetField.
func (c *Compiler) tryCompileSelectorAssignFastPath(ctx context.Context, selection *types.Selection, receiverLocation, valueLocation program.VarLocation) bool {
	if c.tryEmitSelectorAssignSliceFastPath(ctx, selection, receiverLocation, valueLocation) {
		return true
	}
	if !isaselect.StructFieldFastPathWriteKindEnabled(valueLocation.Kind) {
		return false
	}
	layoutIdx, ok := c.tryResolveStructFieldLayout(ctx, selection)
	if !ok {
		return false
	}
	layout := c.Function.StructLayoutTable[layoutIdx]
	if isa.RegisterKind(layout.RegisterKind) != valueLocation.Kind {
		return false
	}
	if fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		if op, hasOp := isaselect.PickSetStructFieldTier0Op(valueLocation.Kind); hasOp {
			program.Emit(c.Function, op, receiverLocation.Register, valueLocation.Register, safeconv.Uint16ToUint8(layoutIdx))
			return true
		}
	}
	sub, hasSubOp := isaselect.PickSetStructFieldUnsafeSubOp(valueLocation.Kind)
	if !hasSubOp {
		return false
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(sub), receiverLocation.Register, valueLocation.Register)
	c.emitStructFieldLayoutExtension(layoutIdx)
	return true
}

// compileStarAssign compiles a pointer dereference assignment: *p = value.
//
// Takes target (*ast.StarExpr) which is the star expression identifying the pointer to
// dereference.
// Takes valueLocation (VarLocation) which is the register location holding the value to
// assign.
//
// Returns an error if the pointer expression fails to compile or is not in a general
// register.
func (c *Compiler) compileStarAssign(ctx context.Context, target *ast.StarExpr, valueLocation program.VarLocation) error {
	pointerLocation, err := c.compileExpression(ctx, target.X)
	if err != nil {
		return fmt.Errorf("compiling pointer assignment: %w", err)
	}
	return c.compileStarAssignFrom(ctx, pointerLocation, valueLocation)
}

// compileStarAssignFrom stores valueLocation through an already compiled pointer.
//
// Takes pointerLocation (program.VarLocation) which holds the pointer.
// Takes valueLocation (program.VarLocation) which holds the value.
//
// Returns error when the pointer is not in the general bank.
func (c *Compiler) compileStarAssignFrom(ctx context.Context, pointerLocation, valueLocation program.VarLocation) error {
	if pointerLocation.Kind != isa.RegisterGeneral {
		return fault.ErrCompileDereferenceAssignRequiresPointer
	}
	c.boxToGeneralTemp(ctx, &valueLocation)

	program.Emit(c.Function, isa.OpSetField, pointerLocation.Register, isa.SentinelFieldDeref, valueLocation.Register)
	return nil
}

// compileDefer compiles a defer statement. The deferred call's function and arguments are
// evaluated eagerly; execution is deferred until the enclosing function returns.
//
// Takes statement (*ast.DeferStmt) which is the AST defer statement to compile.
//
// Returns a zero VarLocation and an error if the deferred function or its arguments fail
// to compile.
func (c *Compiler) compileDefer(ctx context.Context, statement *ast.DeferStmt) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureDefer, statement.Defer); err != nil {
		return program.VarLocation{}, err
	}
	c.hasDefers = true
	c.deferCount++
	if c.loopDepth > 0 {
		c.deferInLoop = true
	}
	callExpression := statement.Call
	if builtin, isBuiltin := c.deferrableBuiltin(callExpression); isBuiltin {
		return c.compileBuiltinStatementCall(ctx, callExpression, builtin, isa.OpDefer, engine.DeferModeBuiltin)
	}
	functionLocation, err := c.compileDeferFunction(ctx, callExpression)
	if err != nil {
		return program.VarLocation{}, err
	}
	argumentLocations, err := c.compileDeferArgs(ctx, callExpression.Args)
	if err != nil {
		return program.VarLocation{}, err
	}
	mode := c.classifyDeferMode(callExpression, len(argumentLocations))
	if mode == engine.DeferModeTrivial {
		c.trivialDeferPCs = append(c.trivialDeferPCs, program.CurrentPC(c.Function))
	}
	if callExpression.Ellipsis.IsValid() {
		mode |= engine.DeferModeSpread
	}
	program.Emit(c.Function, isa.OpDefer, functionLocation.Register, safeconv.MustIntToUint8(len(argumentLocations)), mode)
	for _, location := range argumentLocations {
		program.Emit(c.Function, isa.OpExt, 0, location.Register, uint8(location.Kind))
	}
	return program.VarLocation{}, nil
}

// compileDeferFunction compiles the deferred call's function expression. When the
// expression is a bare identifier resolving to a top-level compiled function, emits
// isa.OpMakeClosure directly to avoid a wasted lookup; otherwise falls back to
// compileExpression + boxToGeneral.
//
// Takes callExpression (*ast.CallExpr) which is the deferred call.
//
// Returns the function value's location in the general bank and any compilation error.
func (c *Compiler) compileDeferFunction(ctx context.Context, callExpression *ast.CallExpr) (program.VarLocation, error) {
	if identifier, ok := callExpression.Fun.(*ast.Ident); ok {
		if functionIndex, found := c.functionTable[identifier.Name]; found {
			return c.bindFunctionValue(ctx, identifier, functionIndex)
		}
	}
	functionLocation, err := c.compileExpression(ctx, callExpression.Fun)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &functionLocation)
	return functionLocation, nil
}

// compileDeferArgs compiles each defer-call argument expression and returns their
// resulting register locations in order.
//
// Takes args ([]ast.Expr) which are the call's argument expressions.
//
// Returns the per-argument VarLocation slice and any compilation error.
func (c *Compiler) compileDeferArgs(ctx context.Context, args []ast.Expr) ([]program.VarLocation, error) {
	argumentLocations := make([]program.VarLocation, len(args))
	for i, argument := range args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return nil, err
		}
		argumentLocations[i] = location
	}
	return argumentLocations, nil
}

// deferrableBuiltin reports whether call invokes a universe builtin that may appear in a
// defer or go statement.
//
// Takes call (*ast.CallExpr) which is the deferred or launched call.
//
// Returns the builtin identifier and true, or 0 and false for any other callee.
func (c *Compiler) deferrableBuiltin(call *ast.CallExpr) (uint8, bool) {
	identifier, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || c.Info == nil {
		return 0, false
	}
	if _, builtin := c.Info.Uses[identifier].(*types.Builtin); !builtin {
		return 0, false
	}
	id, known := deferrableBuiltinIDs[identifier.Name]
	return id, known
}

// compileBuiltinStatementCall emits `defer builtin(args)` or `go builtin(args)`: the
// arguments are evaluated now and the instruction carries the builtin identifier in
// operand A with the builtin mode in operand C, followed by one extension word per
// argument in the same layout as a closure target.
//
// Takes call (*ast.CallExpr) which is the builtin call.
// Takes builtin (uint8) which is the isa.Builtin* identifier.
// Takes op (isa.Opcode) which is isa.OpDefer or isa.OpGo.
// Takes mode (uint8) which is the opcode's builtin mode value.
//
// Returns an empty location and any compilation error.
func (c *Compiler) compileBuiltinStatementCall(ctx context.Context, call *ast.CallExpr, builtin uint8, op isa.Opcode, mode uint8) (program.VarLocation, error) {
	if err := c.checkBuiltinStatementFeature(builtin, call.Lparen); err != nil {
		return program.VarLocation{}, err
	}
	argumentLocations, err := c.compileDeferArgs(ctx, call.Args)
	if err != nil {
		return program.VarLocation{}, err
	}
	program.Emit(c.Function, op, builtin, safeconv.MustIntToUint8(len(argumentLocations)), mode)
	for _, location := range argumentLocations {
		program.Emit(c.Function, isa.OpExt, 0, location.Register, uint8(location.Kind))
	}
	return program.VarLocation{}, nil
}

// checkBuiltinStatementFeature applies the feature policy a direct call of the builtin
// would: channels for close, panic/recover for panic and recover.
//
// Takes builtin (uint8) which is the isa.Builtin* identifier.
// Takes position (token.Pos) which locates the call for the error.
//
// Returns the policy error, or nil.
func (c *Compiler) checkBuiltinStatementFeature(builtin uint8, position token.Pos) error {
	switch builtin {
	case isa.BuiltinClose:
		return c.checkFeature(policy.InterpFeatureChannels, position)
	case isa.BuiltinPanic, isa.BuiltinRecover:
		return c.checkFeature(policy.InterpFeaturePanicRecover, position)
	default:
		return nil
	}
}

// classifyDeferMode picks between DeferModeChain (the general defer stack path) and
// DeferModeTrivial (the per-frame fast path) for the current defer statement, updating
// the Compiler's per-body classification flags when the trivial path applies.
//
// Takes callExpression (*ast.CallExpr) which is the deferred call.
// Takes argumentCount (int) which is the number of arguments already compiled.
//
// Returns the chosen defer mode encoded as the C operand of isa.OpDefer.
func (c *Compiler) classifyDeferMode(callExpression *ast.CallExpr, argumentCount int) uint8 {
	if !classifyTrivialDeferShape(callExpression) || argumentCount > isa.MaxTrivialDeferArgs ||
		c.deferCount != 1 || c.hasRecover || c.deferInLoop ||
		callExpression.Ellipsis.IsValid() || c.deferNeedsPacking(callExpression) {
		return engine.DeferModeChain
	}
	c.thisDeferTrivial = true
	c.simpleDeferArgCount = safeconv.MustIntToUint8(argumentCount)
	return engine.DeferModeTrivial
}

// variadicSliceReflectType returns the reflect type of a variadic signature's final
// parameter (the []T its trailing arguments pack into), or nil for a fixed signature.
//
// Takes signature (*types.Signature) which may be nil.
//
// Returns reflect.Type which is nil unless the signature is variadic.
func (c *Compiler) variadicSliceReflectType(ctx context.Context, signature *types.Signature) reflect.Type {
	if signature == nil || !signature.Variadic() || signature.Params().Len() == 0 {
		return nil
	}
	return c.TypeToReflect(ctx, signature.Params().At(signature.Params().Len()-1).Type())
}

// deferNeedsPacking reports whether a deferred call's trailing arguments must be packed
// by the chain path: the callee is variadic and compiled (a native callee is invoked
// through reflect, which packs for itself).
//
// Takes call (*ast.CallExpr) which is the deferred call.
//
// Returns bool which is true when the trivial path cannot run the call.
func (c *Compiler) deferNeedsPacking(call *ast.CallExpr) bool {
	tv, ok := c.Info.Types[call.Fun]
	if !ok || tv.Type == nil {
		return false
	}
	signature, ok := types.Unalias(tv.Type).Underlying().(*types.Signature)
	if !ok || !signature.Variadic() {
		return false
	}
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := selector.X.(*ast.Ident); ok {
			if _, isPackage := c.Info.ObjectOf(ident).(*types.PkgName); isPackage {
				return false
			}
		}
	}
	return true
}

// compileNativePackageVarAssign stores into a settable package variable of a registered
// Go package (`runtime.MemProfileRate = n`): the variable's address is loaded as a
// general constant (descriptor.GeneralConstantPackageSymbolAddress) and the value is
// written through it. Writing a host global is gated by policy.InterpFeatureUnsafeOps,
// which the restricted feature sets withhold.
//
// Takes target (*ast.SelectorExpr) which is the assignment's left-hand side.
// Takes valueLocation (program.VarLocation) which holds the value to store.
//
// Returns handled (bool) which is false when the selector is not package-qualified.
// Returns error when the symbol is not a settable variable.
func (c *Compiler) compileNativePackageVarAssign(ctx context.Context, target *ast.SelectorExpr, valueLocation program.VarLocation) (bool, error) {
	if !c.isPackageQualifiedSelector(target) {
		return false, nil
	}
	object, ok := c.Info.Uses[target.Sel]
	if !ok || object.Pkg() == nil {
		return false, nil
	}
	if err := c.checkFeature(policy.InterpFeatureUnsafeOps, target.Pos()); err != nil {
		return true, err
	}
	var value reflect.Value
	found := false
	if c.symbols != nil {
		value, found = c.symbols.Lookup(object.Pkg().Path(), object.Name())
	}
	if !found || !value.CanAddr() || !value.CanSet() {
		return true, fmt.Errorf("%w: %s.%s", fault.ErrCompilePackageVariableNotSettable, object.Pkg().Path(), object.Name())
	}
	constIndex, err := program.AddGeneralConstant(c.Function, value.Addr(), descriptor.GeneralConstantDescriptor{
		Kind:           descriptor.GeneralConstantPackageSymbolAddress,
		PackagePath:    object.Pkg().Path(),
		SymbolName:     object.Name(),
		TypeDescriptor: descriptor.TypeDescriptor{},
	})
	if err != nil {
		return true, err
	}
	pointer := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, pointer, constIndex)
	c.emitIndirectWrite(ctx, program.VarLocation{Register: pointer, Kind: isa.RegisterGeneral}, valueLocation)
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, pointer)
	return true, nil
}

// isPackageQualifiedSelector reports whether selector names a symbol of an imported
// package (`runtime.MemProfileRate`) rather than a field or method of a value.
//
// Takes selector (*ast.SelectorExpr) which is inspected through the type information.
//
// Returns bool which is true when the operand is a package name.
func (c *Compiler) isPackageQualifiedSelector(selector *ast.SelectorExpr) bool {
	ident, ok := selector.X.(*ast.Ident)
	if !ok || c.Info == nil {
		return false
	}
	_, isPackage := c.Info.ObjectOf(ident).(*types.PkgName)
	return isPackage
}

// resultsHaveNames reports whether a result list declares any named result. Only then do
// result variables exist that must stay out of the canonical return slots; a plain `func
// f() int` keeps its usual register layout.
//
// Takes results (*ast.FieldList) which is the function's result list.
//
// Returns true when at least one result is named (blank names included).
func resultsHaveNames(results *ast.FieldList) bool {
	for _, field := range results.List {
		if len(field.Names) > 0 {
			return true
		}
	}
	return false
}

// resolveResultTypes returns the declared result types of declaration in source order,
// flattening multi-name fields. Returns nil when the function has no declared results,
// when go/types could not resolve the function signature, or when the function's
// signature has zero results.
//
// Used by compileFunctionBody to seed currentResultTypes so compileReturnExprs can detect
// typed-nil contexts (return nil from func() *T) without re-walking the AST result list
// at every return statement.
//
// Takes info (*types.Info) which provides the resolved type of the function declaration's
// name identifier.
// Takes declaration (*ast.FuncDecl) whose signature is inspected.
//
// Returns a slice of result types in source order, or nil when no declared results.
func resolveResultTypes(info *types.Info, declaration *ast.FuncDecl) []types.Type {
	if declaration == nil || declaration.Name == nil {
		return nil
	}
	object := info.Defs[declaration.Name]
	if object == nil {
		return nil
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok || signature.Results() == nil {
		return nil
	}
	results := signature.Results()
	if results.Len() == 0 {
		return nil
	}
	out := make([]types.Type, results.Len())
	for i := 0; i < results.Len(); i++ {
		out[i] = results.At(i).Type()
	}
	return out
}

// buildFunctionDeclStub constructs the CompiledFunction stub for a FuncDecl.
//
// Populates receiver metadata, generic type params, parameter kinds (with heap-promotion
// and typed-slice overrides), and result kinds. The body is compiled later in a second
// pass.
//
// Takes c (*Compiler) which carries the type-info and registries.
// Takes declaration (*ast.FuncDecl) which is the parsed declaration.
// Takes signature (*types.Signature) which is the resolved signature.
// Takes tableName (string) which is the function-table key.
//
// Returns *CompiledFunction which is the freshly built stub.
func buildFunctionDeclStub(ctx context.Context, c *Compiler, declaration *ast.FuncDecl, signature *types.Signature, tableName string) *program.CompiledFunction {
	compiledFunction := &program.CompiledFunction{Name: tableName}
	compiledFunction.RuntimeName = c.runtimeFunctionName(declaration)

	compiledFunction.HasRecover = bodyContainsRecoverCall(c.Info, declaration.Body)
	compiledFunction.InspectsCallStack = bodyInspectsCallStack(c.Info, declaration.Body)
	if receiver := signature.Recv(); receiver != nil && !typemap.ContainsTypeParameter(receiver.Type()) {
		compiledFunction.MethodReceiverReflectType = c.TypeToReflect(ctx, receiver.Type())
	}
	compiledFunction.IsVariadic = signature.Variadic()
	compiledFunction.VariadicSliceType = c.variadicSliceReflectType(ctx, signature)
	compiledFunction.SignatureReflectType = c.signatureReflectType(ctx, signature)
	if signature.TypeParams() != nil && signature.TypeParams().Len() > 0 {
		compiledFunction.IsGenericFunction = true
		compiledFunction.GenericTypeParams = signature.TypeParams()
		compiledFunction.GenericDeclaration = declaration

		compiledFunction.GenericRecvTypeParams = signature.RecvTypeParams()
		compiledFunction.RequiresSpecialisation = declarationCallsGenericMethodGenerically(c, declaration)
	}
	if signature.Recv() != nil {
		compiledFunction.ParameterKinds = append(compiledFunction.ParameterKinds, isa.RegisterGeneral)
		compiledFunction.ParameterIsGeneric = append(compiledFunction.ParameterIsGeneric, false)
		compiledFunction.ParameterTypeRefs = append(compiledFunction.ParameterTypeRefs, signature.Recv().Type())
		_, compiledFunction.IsPointerReceiver = signature.Recv().Type().(*types.Pointer)
		compiledFunction.HasReceiver = true
	}
	populateFunctionDeclParameterKinds(c, declaration, signature, compiledFunction)
	for r := range signature.Results().Variables() {
		compiledFunction.ResultKinds = append(compiledFunction.ResultKinds, c.kindForCallSlot(r.Type()))
		compiledFunction.ResultTypeRefs = append(compiledFunction.ResultTypeRefs, r.Type())
		compiledFunction.ResultReflectTypes = append(compiledFunction.ResultReflectTypes, c.exactReflectTypeForBoxing(r.Type()))
	}
	return compiledFunction
}

// declarationCallsGenericMethodGenerically reports whether a generic declaration's body
// calls a Go 1.27 generic method with type arguments that are not yet concrete.
//
// A generic method has no correct erased lowering, so such a body can only run once it
// has been monomorphised. Inside a generic declaration the only type parameters in scope
// are that declaration's own, so a type argument that still mentions one is exactly the
// case the requirement has to propagate outward for.
//
// Takes c (*Compiler) which carries the type-check info.
// Takes declaration (*ast.FuncDecl) which is the declaration to scan.
//
// Returns true when the erased body would contain an unresolvable generic-method call.
func declarationCallsGenericMethodGenerically(c *Compiler, declaration *ast.FuncDecl) bool {
	if declaration == nil || declaration.Body == nil || c.Info == nil {
		return false
	}
	found := false
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		if found {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selectorNeedsSpecialisation(c, selector) {
			found = true
			return false
		}
		return true
	})
	return found
}

// selectorNeedsSpecialisation reports whether one selector is a generic-method call whose
// type arguments are not yet concrete.
//
// Both the method's own type arguments and the receiver's are checked: either can carry a
// type parameter of the enclosing declaration, and either is enough to make the erased
// body unusable.
//
// Takes c (*Compiler) which carries the type-check info.
// Takes selector (*ast.SelectorExpr) which is the selector to classify.
//
// Returns true when the selector names a generic method that cannot be monomorphised
// here.
func selectorNeedsSpecialisation(c *Compiler, selector *ast.SelectorExpr) bool {
	if !isGenericMethodSelection(c, selector) {
		return false
	}
	instance, ok := c.Info.Instances[selector.Sel]
	if !ok || instance.TypeArgs == nil {
		return false
	}
	for argument := range instance.TypeArgs.Types() {
		if typemap.ContainsTypeParameter(argument) {
			return true
		}
	}
	return typemap.ContainsTypeParameter(c.Info.Types[selector.X].Type)
}

// isGenericMethodSelection reports whether a selector names a method that declares its
// own type parameters.
//
// The origin object is consulted rather than the instantiated one, because instantiation
// strips the type-parameter list that identifies a generic method.
//
// Takes c (*Compiler) which carries the type-check info.
// Takes selector (*ast.SelectorExpr) which is the selector to classify.
//
// Returns true when the selected object is a generic method.
func isGenericMethodSelection(c *Compiler, selector *ast.SelectorExpr) bool {
	selected, ok := c.Info.Uses[selector.Sel].(*types.Func)
	if !ok {
		return false
	}
	origin, ok := selected.Origin().Type().(*types.Signature)
	if !ok {
		return false
	}
	return origin.Recv() != nil && origin.TypeParams() != nil && origin.TypeParams().Len() > 0
}

// populateFunctionDeclParameterKinds fills parameter kinds on the stub.
//
// Derives the kind for each declared parameter, applying the heap-promotion,
// type-parameter, and typed-slice overrides that distinguish function declarations from
// closure literals.
//
// Takes c (*Compiler) which carries the type-info and registries.
// Takes declaration (*ast.FuncDecl) which is the parsed declaration.
// Takes signature (*types.Signature) which is the resolved signature.
// Takes compiledFunction (*CompiledFunction) which receives the kind metadata.
func populateFunctionDeclParameterKinds(c *Compiler, declaration *ast.FuncDecl, signature *types.Signature, compiledFunction *program.CompiledFunction) {
	parameterCount := signature.Params().Len()
	parameterIndex := 0
	heapPromotedParams := collectHeapPromotedParamNames(c, declaration.Type, declaration.Body)
	typedSliceParams := classifyTypedSliceParameters(c, declaration.Body, signature)
	for p := range signature.Params().Variables() {
		kind := c.parameterSlotKind(signature, p.Type(), parameterIndex, parameterCount)
		if heapPromotedParams[p.Name()] {
			kind = c.kindFor(p.Type())
		}
		if typemap.IsTypeParameter(p.Type()) || typemap.ContainsTypeParameter(p.Type()) {
			kind = c.kindFor(p.Type())
		}
		if isa.IsTypedSliceKind(kind) {
			if survivorKind, ok := typedSliceParams[p.Name()]; ok {
				kind = survivorKind
			} else {
				kind = c.kindFor(p.Type())
			}
		}
		compiledFunction.ParameterKinds = append(compiledFunction.ParameterKinds, kind)
		compiledFunction.ParameterIsGeneric = append(compiledFunction.ParameterIsGeneric, typemap.IsTypeParameter(p.Type()))
		compiledFunction.ParameterTypeRefs = append(compiledFunction.ParameterTypeRefs, p.Type())

		compiledFunction.ParameterTypedSlicePromoted = append(compiledFunction.ParameterTypedSlicePromoted, isa.IsTypedSliceKind(kind))
		parameterIndex++
	}
}

// specialisationSuffix builds a short, deterministic name suffix for a specialised
// function from its substitution map. Used only for diagnostic naming (disassembler
// output, error messages); not part of any cache key.
//
// Takes subs (map[*types.TypeParam]types.Type) which is the substitution map.
// Takes typeParams (*types.TypeParamList) which provides the canonical ordering.
//
// Returns a comma-separated string of substituted type names.
func specialisationSuffix(subs map[*types.TypeParam]types.Type, typeParams *types.TypeParamList) string {
	if typeParams == nil || typeParams.Len() == 0 {
		return ""
	}
	parts := make([]string, 0, typeParams.Len())
	for tp := range typeParams.TypeParams() {
		if substituted, ok := subs[tp]; ok {
			parts = append(parts, substituted.String())
		} else {
			parts = append(parts, tp.Obj().Name())
		}
	}
	return strings.Join(parts, ",")
}

// bodyContainsDefer reports whether body contains a defer statement of its own; defers
// inside nested function literals belong to those closures and are not counted.
//
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns true when a defer statement registers on this frame.
func bodyContainsDefer(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		switch node.(type) {
		case *ast.DeferStmt:
			found = true
			return false
		case *ast.FuncLit:
			return false
		default:
			return true
		}
	})
	return found
}

// bodyContainsRecoverCall scans body for a predeclared recover() call.
//
// Resolution against types.Info ensures shadowed locals named `recover` do not trigger
// false positives. Used by compileFunctionBody to set hasRecover once per body, so the
// per-defer trivial-defer classification can read it without re-walking.
//
// Takes info (*types.Info) which holds the type-checker's resolution of identifiers. May
// be nil; the conservative result in that case is true, which disables the trivial-defer
// fast path.
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns true when a recover() call resolves to the universe-scope builtin within body.
func bodyContainsRecoverCall(info *types.Info, body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	if info == nil {
		return true
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		if isUniverseRecoverCall(info, node) {
			found = true
			return false
		}
		return true
	})
	return found
}

// isUniverseRecoverCall reports whether the AST node is a call expression invoking the
// universe-scope recover() builtin (i.e. not a shadowed local or package symbol named
// recover).
//
// Takes info (*types.Info) which holds the type-checker's resolution of identifiers; must
// be non-nil for the universe-scope check.
// Takes node (ast.Node) which is the AST node to classify.
//
// Returns true when node is a call to the predeclared recover().
func isUniverseRecoverCall(info *types.Info, node ast.Node) bool {
	callExpression, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	identifier, ok := callExpression.Fun.(*ast.Ident)
	if !ok || identifier.Name != "recover" {
		return false
	}
	object := info.Uses[identifier]
	if object == nil {
		object = info.ObjectOf(identifier)
	}
	if object == nil {
		return false
	}
	return object.Pkg() == nil && object.Parent() == types.Universe
}

// finaliseSimpleDeferClassification downgrades provisionally trivial defer opcodes to
// engine.DeferModeChain.
//
// Runs after body compilation. When the whole-function classification (exactly one defer,
// no recover, not in a loop, trivial shape) ultimately failed, rewrites the recorded
// sites so the runtime can rely on a single uniform unwind path.
//
// Called once per function body.
//
// Takes sub (*Compiler) which is the sub-Compiler that compiled the body, holding the
// per-body deferCount, hasRecover, deferInLoop state and the recorded sites.
// Takes compiledFunction (*CompiledFunction) which is the compiled function whose body
// bytecode may need rewriting.
func finaliseSimpleDeferClassification(sub *Compiler, compiledFunction *program.CompiledFunction) {
	qualifies := sub.deferCount == 1 && !sub.hasRecover && !sub.deferInLoop && sub.thisDeferTrivial
	if qualifies {
		return
	}
	for _, pc := range sub.trivialDeferPCs {
		if compiledFunction.Body[pc].Op == isa.OpDefer && compiledFunction.Body[pc].C == engine.DeferModeTrivial {
			compiledFunction.Body[pc].C = engine.DeferModeChain
		}
	}
}

// classifyTrivialDeferShape reports whether the deferred call's function expression is a
// bare identifier or selector, qualifying it for the trivial defer path.
//
// Takes call (*ast.CallExpr) which is the defer call expression.
//
// Returns true when the call qualifies.
func classifyTrivialDeferShape(call *ast.CallExpr) bool {
	switch call.Fun.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return true
	default:
		return false
	}
}

// methodTableName returns the function table key for a function declaration. For methods
// it is "ReceiverType.MethodName"; for plain functions it is the bare function name.
//
// Takes declaration (*ast.FuncDecl) which is the function declaration to derive the table
// name from.
//
// Returns the method table key string for the given declaration.
func methodTableName(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return declaration.Name.Name
	}
	return receiverExpressionTypeName(declaration.Recv.List[0].Type) + "." + declaration.Name.Name
}

// receiverExpressionTypeName extracts the unqualified type name from a receiver type
// expression, stripping any pointer indirection and generic type parameter lists (e.g.,
// *Box[T] produces "Box").
//
// Takes expression (ast.Expr) which is the receiver type expression to extract from.
//
// Returns the bare type name string, or empty string if extraction fails.
func receiverExpressionTypeName(expression ast.Expr) string {
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
	}
	switch e := expression.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		if identifier, ok := e.X.(*ast.Ident); ok {
			return identifier.Name
		}
	case *ast.IndexListExpr:
		if identifier, ok := e.X.(*ast.Ident); ok {
			return identifier.Name
		}
	}
	return ""
}
