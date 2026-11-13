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

package typemap

import (
	"context"
	"fmt"
	"go/types"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
)

const (
	// BlankIdentName is the Go blank identifier "_", used to discard values in assignments
	// and declarations.
	BlankIdentName = "_"

	// CycleBrokenTagKey re-exports isa.CycleBrokenTagKey so the generated selfhost symbol
	// table can reference it. The compiler and engine read the canonical value from isa.
	CycleBrokenTagKey = isa.CycleBrokenTagKey

	// CycleBrokenTagValue re-exports isa.CycleBrokenTagValue for the same reason as
	// CycleBrokenTagKey.
	CycleBrokenTagValue = isa.CycleBrokenTagValue

	// maxConversionSteps bounds the types one conversion visits.
	maxConversionSteps = 1 << 20

	// maxConversionDepth bounds the nesting of one conversion, well below the depth at which
	// the goroutine stack would run out.
	maxConversionDepth = 1 << 13

	// conversionCancelCheckInterval is how many steps pass between checks of the context.
	conversionCancelCheckInterval = 1 << 10

	// braceNestingHint is the expected nesting depth of struct and interface literals in a
	// rendered type string. Deeper strings grow the tracking slice.
	braceNestingHint = 4
)

// typeConverterState carries registry lookups, the active cycle-detection set, and an
// optional shared cache through one typeToReflect call chain.
type typeConverterState struct {
	// conversionErr is the first failure of the conversion, such as a named composite type
	// that could not be given its own runtime type or a type past the step budget. Once it
	// is set every further conversion gives `any`, and the caller fails the compile.
	conversionErr error

	// symbols resolves pre-registered native types via the SymbolRegistry. May be nil when
	// conversion runs outside a registered-symbol context.
	symbols *symtab.SymbolRegistry

	// processing tracks the named types currently being synthesised along the active
	// recursion path, enabling cycle detection so self-referential struct fields collapse to
	// reflect.TypeFor[any]().
	processing map[types.Type]bool

	// cache holds the synthesised reflect.Type per go/types.Type so mutually recursive named
	// types share identity across entry points. May be nil.
	cache map[types.Type]reflect.Type

	// globals holds the per-Service shared registry for named interfaces, so the
	// reflect.TypeOf intercept can wrap results with the correct source-level identity. May
	// be nil outside a Service context.
	globals *engine.GlobalStore

	// cycleTypes maps the members of a group of self-referential named composite types being
	// built to their forged runtime types (convertNamedCompositeCycle).
	cycleTypes map[*types.Named]reflect.Type

	// cycles finds the groups of self-referential named composite types. It lives on the
	// TypeCache when there is one, so a compile walks each type once.
	cycles *cycleIndex

	// processingStructs tracks the anonymous struct types on the active recursion path
	// during type conversion.
	processingStructs []*types.Struct

	// steps counts the types the conversion has visited, which maxConversionSteps bounds.
	steps int

	// depth is the current nesting of convertType calls, which maxConversionDepth bounds.
	depth int
}

// braceRewriter is the state of reflectStyleBraces.
type braceRewriter struct {
	// out accumulates the rewritten type string.
	out strings.Builder

	// depthIsBlock tracks, per open brace, whether it opened a struct or interface block.
	depthIsBlock []bool
}

// consume rewrites the byte at position i of rendered and returns the position of the
// last byte it consumed.
//
// Takes rendered (string) which is the input.
// Takes i (int) which is the position to consume.
//
// Returns int which is the last consumed position.
func (r *braceRewriter) consume(rendered string, i int) int {
	ch := rendered[i]
	switch {
	case ch == '{':
		r.openBrace(rendered, i)
		return i
	case ch == '}':
		r.closeBrace(rendered, i)
		return i
	case ch == 'a' && strings.HasPrefix(rendered[i:], "any") && wordBoundary(rendered, i-1) && wordBoundary(rendered, i+3):
		r.out.WriteString("interface {}")
		return i + 2
	default:
		r.out.WriteByte(ch)
		return i
	}
}

// openBrace writes an opening brace, spaced as reflect prints it when it opens a struct
// or interface block.
//
// Takes rendered (string) which is the input.
// Takes i (int) which is the brace's position.
func (r *braceRewriter) openBrace(rendered string, i int) {
	written := r.out.String()
	block := strings.HasSuffix(written, "struct") || strings.HasSuffix(written, "interface")
	r.depthIsBlock = append(r.depthIsBlock, block)
	if !block {
		r.out.WriteByte('{')
		return
	}
	r.out.WriteString(" {")
	if i+1 < len(rendered) && rendered[i+1] != '}' {
		r.out.WriteByte(' ')
	}
}

// closeBrace writes a closing brace, spaced as reflect prints it when it closes a
// non-empty struct or interface block.
//
// Takes rendered (string) which is the input.
// Takes i (int) which is the brace's position.
func (r *braceRewriter) closeBrace(rendered string, i int) {
	if n := len(r.depthIsBlock); n > 0 {
		block := r.depthIsBlock[n-1]
		r.depthIsBlock = r.depthIsBlock[:n-1]
		if block && rendered[i-1] != '{' {
			r.out.WriteByte(' ')
		}
	}
	r.out.WriteByte('}')
}

// step charges one visited type to the conversion's budget and checks the context now and
// then.
//
// Returns error which wraps fault.ErrCompileTypeTooComplex past maxConversionSteps, or
// the context's cause when ctx is done.
func (state *typeConverterState) step(ctx context.Context) error {
	state.steps++
	if state.steps > maxConversionSteps {
		return tooComplex(fmt.Sprintf("more than %d steps", maxConversionSteps))
	}
	if state.steps%conversionCancelCheckInterval == 0 && ctx.Err() != nil {
		return fmt.Errorf("type conversion stopped: %w", context.Cause(ctx))
	}
	return nil
}

// descend charges one step and checks a recursion's depth.
//
// Takes depth (int) which is the recursion's current depth.
//
// Returns error which wraps fault.ErrCompileTypeTooComplex past maxConversionDepth or
// maxConversionSteps, or the context's cause when ctx is done.
func (state *typeConverterState) descend(ctx context.Context, depth int) error {
	if depth > maxConversionDepth {
		return tooComplex(fmt.Sprintf("nested more than %d levels", maxConversionDepth))
	}
	return state.step(ctx)
}

// fail records the conversion's first failure.
//
// Takes err (error) which is the failure.
//
// Returns reflect.Type which is `any`, standing in for the type until the compile fails.
func (state *typeConverterState) fail(err error) reflect.Type {
	if state.conversionErr == nil {
		state.conversionErr = err
	}
	return reflect.TypeFor[any]()
}

// quota returns the named composite quota of the conversion's symbol registry.
//
// Returns *typemodel.NamedCompositeQuota which is nil without a registry.
func (state *typeConverterState) quota() *typemodel.NamedCompositeQuota {
	return state.symbols.NamedCompositeQuota()
}

// TypeToReflectCached converts a go/types.Type to a reflect.Type through cache, so
// mutually recursive named types share identity across calls and each type is converted
// once per compile.
//
// Takes t (types.Type) which is the type to convert.
// Takes symbols (*symtab.SymbolRegistry) which resolves host types and supplies the named
// composite quota. May be nil.
// Takes cache (*TypeCache) which is the compile's memory of converted types. May be nil.
// Takes globals (*GlobalStore) which receives user-declared named interface registrations
// as a side effect. May be nil.
//
// Returns reflect.Type which is the synthesised reflect.Type for t.
// Returns error which wraps typemodel.ErrNamedCompositeLimit,
// typemodel.ErrNamedCompositeQuota or typemodel.ErrNamedCompositeUnsupported when a named
// array, slice, map, channel or function type that t reaches could not be given its own
// runtime type, fault.ErrCompileTypeTooComplex when the conversion passed its step or
// depth bound, or the context's cause when ctx was done. The returned type then stands in
// for the real one and must not run. Every later call through the same cache returns the
// same error at once.
func TypeToReflectCached(ctx context.Context, t types.Type, symbols *symtab.SymbolRegistry, cache *TypeCache, globals *engine.GlobalStore) (reflect.Type, error) {
	if cache != nil && cache.failure != nil {
		return reflect.TypeFor[any](), cache.failure
	}
	state := &typeConverterState{
		symbols:           symbols,
		processing:        make(map[types.Type]bool),
		cache:             nil,
		globals:           globals,
		processingStructs: nil,
		cycleTypes:        nil,
		cycles:            nil,
		conversionErr:     nil,
		steps:             0,
		depth:             0,
	}
	if cache != nil {
		state.cache, state.cycles = cache.types, cache.cycles
	} else {
		state.cycles = newCycleIndex()
	}
	result := convertType(ctx, state, t)
	if state.conversionErr != nil && cache != nil {
		cache.failure = state.conversionErr
	}
	return result, state.conversionErr
}

// RuntimeTypeString renders t the way Go's runtime prints it (`reflect.Type.String`,
// `%T`): package-name qualified, and with type arguments joined by "," without a space,
// where go/types writes ", ".
//
// Takes t (types.Type) which is the type to render.
//
// Returns string such as "main.Pair[string,int]".
func RuntimeTypeString(t types.Type) string {
	if signature, ok := t.(*types.Signature); ok {
		t = types.NewSignatureType(nil, nil, nil, unnamedTuple(signature.Params()), unnamedTuple(signature.Results()), signature.Variadic())
	}
	rendered := types.TypeString(t, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
	return reflectStyleBraces(compactTypeArgumentLists(rendered))
}

// ResolveNativeBackedType returns the canonical erased reflect.Type for object.
//
// Applies when object names a native-backed generic type (e.g. atomic.Pointer). Every
// instantiation atomic.Pointer[Config] resolves to the same method-bearing erased
// atomic.Pointer[struct{}]: methods are layout-erased over a single machine pointer, so
// one reflect.Type serves all element types.
//
// Takes object (*types.TypeName) which is the go/types type name.
// Takes symbols (*SymbolRegistry) which is the symbol registry. May be nil.
//
// Returns the erased reflect.Type or nil when object is not native-backed.
func ResolveNativeBackedType(object *types.TypeName, symbols *symtab.SymbolRegistry) reflect.Type {
	if symbols == nil || object.Pkg() == nil {
		return nil
	}
	rt, found := symbols.NativeBackedReflectType(object.Pkg().Path(), object.Name())
	if !found {
		return nil
	}
	return rt
}

// ResolveRegisteredType returns the pre-registered reflect.Type for object when the
// symbol registry holds one, otherwise nil.
//
// Takes object (*types.TypeName) which is the go/types type name to look up.
// Takes symbols (*SymbolRegistry) which is the symbol registry. May be nil.
//
// Returns the registered reflect.Type or nil when no entry exists.
func ResolveRegisteredType(object *types.TypeName, symbols *symtab.SymbolRegistry) reflect.Type {
	if symbols == nil || object.Pkg() == nil {
		return nil
	}
	rt, found := symbols.ReflectTypeForNamed(object.Pkg().Path(), object.Name())
	if !found {
		return nil
	}
	return rt
}

// ContainsTypeParameter reports whether t mentions a type parameter anywhere in its
// structure (slice element, map element, pointer pointee, struct field, instantiation
// argument, etc.). Slots that depend on a type parameter stay on the general bank until a
// specialised body with concrete types is created.
//
// Takes t (types.Type) which is the type to inspect.
//
// Returns true when t directly is or transitively contains a TypeParam.
func ContainsTypeParameter(t types.Type) bool {
	return containsTypeParameter(t, map[types.Type]struct{}{})
}

// IsTypeParameter reports whether t reduces to a TypeParam.
//
// The static type is tested before its underlying form because TypeParam.Underlying
// returns the constraint interface, not a TypeParam, so an underlying-only check misses
// bare parameters. Aliases are resolved first for consistency.
//
// Takes t (types.Type) which is the type to inspect.
//
// Returns true when t is a type parameter at any underlying level.
func IsTypeParameter(t types.Type) bool {
	if t == nil {
		return false
	}
	unaliased := types.Unalias(t)
	if _, ok := unaliased.(*types.TypeParam); ok {
		return true
	}
	if named, ok := unaliased.(*types.Named); ok {
		return IsTypeParameter(named.Underlying())
	}
	return false
}

// CanonicalType rewrites t into the canonical form Go uses when printing a type,
// resolving alias spellings that go/types preserves but the runtime does not.
//
// Takes t (types.Type) which is the type to canonicalise.
//
// Returns the canonical type, or t when nothing needed rewriting.
func CanonicalType(t types.Type) types.Type {
	return canonicalType(t, map[types.Type]types.Type{})
}

// convertNamedStruct converts a named struct type to a reflect.Type with cycle detection.
//
// Linked generics elide the sentinel field so they share structural identity with
// reflect-constructed instances.
//
// Takes state (*typeConverterState) which holds registry, processing set, and cache.
// Takes named (*types.Named) which is the go/types named struct to convert.
//
// Returns the synthesised reflect.Type for named.
func convertNamedStruct(ctx context.Context, state *typeConverterState, named *types.Named) reflect.Type {
	if rt := cachedReflectType(state, named); rt != nil {
		return rt
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return convertType(ctx, state, named)
	}

	if state.processing[named] {
		logTypeCycle(ctx, named, state.processing)
		return reflect.TypeFor[any]()
	}
	state.processing[named] = true
	defer delete(state.processing, named)

	fields := buildStructFields(ctx, state, st)
	if !isLinkedGenericNamed(state, named) {
		fields = append(fields, sentinelField(named, st))
	}

	result := reflect.StructOf(fields)
	storeReflectType(state, named, result)
	return result
}

// isLinkedGenericNamed reports whether named is registered as a linked generic type.
//
// Linked generics rely on reflect's structural type identity, so the converter omits the
// sentinel field for them.
//
// Takes state (*typeConverterState) which holds the symbol registry.
// Takes named (*types.Named) which is the go/types named type to test.
//
// Returns true when named is registered as a linked generic type.
func isLinkedGenericNamed(state *typeConverterState, named *types.Named) bool {
	if state.symbols == nil {
		return false
	}
	object := named.Obj()
	if object == nil || object.Pkg() == nil {
		return false
	}
	return state.symbols.IsLinkedGenericType(object.Pkg().Path(), object.Name())
}

// logTypeCycle logs a warning when a recursion cycle is detected during type conversion.
//
// The log entry includes the offending type and the chain of types on the active
// conversion path.
//
// Takes named (*types.Named) which is the named type that triggered the cycle.
// Takes processing (map[types.Type]bool) which is the active set of named types on the
// conversion path.
func logTypeCycle(ctx context.Context, named *types.Named, processing map[types.Type]bool) {
	typePath := named.Obj().Name()
	if named.Obj().Pkg() != nil {
		typePath = named.Obj().Pkg().Path() + "." + named.Obj().Name()
	}

	var chain []string
	for processingType := range processing {
		if processingNamed, ok := processingType.(*types.Named); ok {
			entryPath := processingNamed.Obj().Name()
			if processingNamed.Obj().Pkg() != nil {
				entryPath = processingNamed.Obj().Pkg().Path() + "." + processingNamed.Obj().Name()
			}
			chain = append(chain, entryPath)
		}
	}

	_, logger := logging.From(ctx)
	logger.Debug("Type cycle detected in compiler type conversion",
		slog.String("type", typePath),
		slog.Any("activeChain", chain),
	)
}

// buildStructFields converts each field of st into a reflect.StructField.
//
// Cycle-bearing field types collapse to any, and embedded unexported fields are renamed
// to satisfy reflect.StructOf's anonymous-plus-PkgPath restriction.
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes st (*types.Struct) which is the go/types struct whose fields are being converted.
//
// Returns the reflect.StructField slice ready for reflect.StructOf.
func buildStructFields(ctx context.Context, state *typeConverterState, st *types.Struct) []reflect.StructField {
	fields := make([]reflect.StructField, st.NumFields())
	for i := range st.NumFields() {
		f := st.Field(i)
		var fieldType reflect.Type
		cycleBroken := fieldTypeInvolvesCycle(state, f.Type())
		if cycleBroken {
			fieldType = convertFieldBreakingCycles(ctx, state, f.Type())
		} else {
			fieldType = convertType(ctx, state, f.Type())
		}
		tag := st.Tag(i)
		if cycleBroken {
			tag = appendCycleBrokenTag(tag)
		}
		fields[i] = reflect.StructField{
			Name:      f.Name(),
			Type:      fieldType,
			Tag:       reflect.StructTag(tag),
			Anonymous: f.Embedded(),
		}
		if f.Embedded() && embeddedFieldNeedsRename(f, fieldType) {
			fields[i].Name = isa.EmbeddedUnexportedPrefix + f.Name()
			fields[i].Anonymous = false
		}

		if !f.Exported() && f.Pkg() != nil {
			fields[i].PkgPath = f.Pkg().Path()
		}
	}
	return fields
}

// embeddedFieldNeedsRename reports whether an embedded field must be renamed to a
// non-anonymous reflect.StructField to avoid reflect.StructOf panics for unexported or
// method-carrying embeds.
//
// Takes f (*types.Var) which is the source-level field.
// Takes fieldType (reflect.Type) which is the field's converted type.
//
// Returns true when the field must be renamed and de-anonymised.
func embeddedFieldNeedsRename(f *types.Var, fieldType reflect.Type) bool {
	if !f.Exported() && f.Pkg() != nil {
		return true
	}
	if fieldType == nil {
		return false
	}
	return fieldType.NumMethod() > 0
}

// appendCycleBrokenTag returns the struct tag with the CycleBrokenTagKey marker appended,
// or returns the marker alone when tag is empty.
//
// Takes tag (string) which is the existing struct tag string.
//
// Returns the tag with the cycle-broken marker appended.
func appendCycleBrokenTag(tag string) string {
	marker := isa.CycleBrokenTagKey + `:"` + isa.CycleBrokenTagValue + `"`
	if tag == "" {
		return marker
	}
	return tag + " " + marker
}

// convertFieldBreakingCycles lowers a cycle-bearing field type to a reflect.Type,
// collapsing only the cycle-causing leaf to any while preserving the surrounding
// container.
//
// Cycle-free sub-branches fall through to convertType for their exact reflect.Type.
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes fieldType (types.Type) which is the field type that involves the cycle.
//
// Returns the lowered reflect.Type for fieldType.
func convertFieldBreakingCycles(ctx context.Context, state *typeConverterState, fieldType types.Type) reflect.Type {
	if !fieldTypeInvolvesCycle(state, fieldType) {
		return convertType(ctx, state, fieldType)
	}
	switch typ := fieldType.(type) {
	case *types.Pointer:
		return reflect.TypeFor[any]()
	case *types.Array:
		return reflect.ArrayOf(int(typ.Len()),
			convertFieldBreakingCycles(ctx, state, typ.Elem()))
	case *types.Slice:
		return reflect.SliceOf(convertFieldBreakingCycles(ctx, state, typ.Elem()))
	case *types.Map:
		return reflect.MapOf(
			convertFieldBreakingCycles(ctx, state, typ.Key()),
			convertFieldBreakingCycles(ctx, state, typ.Elem()),
		)
	case *types.Chan:
		elem := convertFieldBreakingCycles(ctx, state, typ.Elem())
		var direction reflect.ChanDir
		switch typ.Dir() {
		case types.SendRecv:
			direction = reflect.BothDir
		case types.SendOnly:
			direction = reflect.SendDir
		case types.RecvOnly:
			direction = reflect.RecvDir
		}
		return reflect.ChanOf(direction, elem)
	case *types.Alias:
		return convertFieldBreakingCycles(ctx, state, types.Unalias(fieldType))
	case *types.Named:

		if _, isSig := typ.Underlying().(*types.Signature); isSig {
			return reflect.TypeFor[any]()
		}
		return reflect.TypeFor[any]()
	case *types.Signature:

		_ = typ
		return reflect.TypeFor[any]()
	default:
		return reflect.TypeFor[any]()
	}
}

// sentinelField builds the zero-size struct field that encodes named's type identity.
//
// The field is unexported with a PkgPath derived from named (falling back to the first
// unexported field's package) so reflect treats it as private to the original package.
//
// Takes named (*types.Named) which is the go/types named type whose identity is being
// encoded.
// Takes st (*types.Struct) which is the underlying struct type used to source a fallback
// PkgPath.
//
// Returns the unexported sentinel StructField.
func sentinelField(named *types.Named, st *types.Struct) reflect.StructField {
	sentinelPackagePath := ""
	if named.Obj().Pkg() != nil {
		sentinelPackagePath = named.Obj().Pkg().Path() + "." + named.Obj().Name()
	}
	for f := range st.Fields() {
		if !f.Exported() && f.Pkg() != nil {
			sentinelPackagePath = f.Pkg().Path()
			break
		}
	}
	field := reflect.StructField{
		Name:    isa.SynthesisedIDFieldPrefix + named.Obj().Name(),
		Type:    reflect.TypeFor[struct{}](),
		PkgPath: sentinelPackagePath,
	}
	if typeArgs := named.TypeArgs(); typeArgs != nil && typeArgs.Len() > 0 {
		field.Tag = reflect.StructTag(program.SentinelTypeArgsTagKey + `:"` + typeArgsTagValue(typeArgs) + `"`)
	}
	return field
}

// typeArgsTagValue renders a type-argument list for the sentinel tag.
//
// Takes typeArgs (*types.TypeList) which are the instantiation's type arguments.
//
// Returns string which is the tag value.
func typeArgsTagValue(typeArgs *types.TypeList) string {
	parts := make([]string, 0, typeArgs.Len())
	for typeArg := range typeArgs.Types() {
		parts = append(parts, RuntimeTypeString(typeArg))
	}
	return strings.Join(parts, program.TypeArgsTagSeparator)
}

// reflectStyleBraces rewrites go/types brace and `any` spellings to match
// reflect.Type.String output.
//
// Takes rendered (string) which is a go/types type string.
//
// Returns string which is the reflect-style rendering.
func reflectStyleBraces(rendered string) string {
	if !strings.Contains(rendered, "{") && !strings.Contains(rendered, "any") {
		return rendered
	}
	rewriter := braceRewriter{out: strings.Builder{}, depthIsBlock: make([]bool, 0, braceNestingHint)}
	for i := 0; i < len(rendered); i++ {
		i = rewriter.consume(rendered, i)
	}
	return rewriter.out.String()
}

// wordBoundary reports whether position index of s is outside the string or holds a
// character that cannot be part of an identifier.
//
// Takes s (string) which is scanned.
// Takes index (int) which may be out of range.
//
// Returns bool which is true at an identifier boundary.
func wordBoundary(s string, index int) bool {
	if index < 0 || index >= len(s) {
		return true
	}
	ch := s[index]
	switch {
	case ch == '_', ch == '.', ch >= '0' && ch <= '9', ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z':
		return false
	default:
		return true
	}
}

// unnamedTuple copies a parameter or result tuple without its names, which Go's runtime
// never prints in a func type.
//
// Takes tuple (*types.Tuple) which is the tuple to copy.
//
// Returns *types.Tuple which carries the same types unnamed.
func unnamedTuple(tuple *types.Tuple) *types.Tuple {
	if tuple == nil || tuple.Len() == 0 {
		return tuple
	}
	vars := make([]*types.Var, tuple.Len())
	for i := range tuple.Len() {
		vars[i] = types.NewParam(tuple.At(i).Pos(), tuple.At(i).Pkg(), "", tuple.At(i).Type())
	}
	return types.NewTuple(vars...)
}

// compactTypeArgumentLists removes the space after commas that separate type arguments
// inside square brackets, leaving commas inside parentheses (function parameter lists)
// untouched.
//
// Takes rendered (string) which is a go/types type string.
//
// Returns string which is the compacted rendering.
func compactTypeArgumentLists(rendered string) string {
	if !strings.Contains(rendered, ", ") {
		return rendered
	}
	var builder strings.Builder
	builder.Grow(len(rendered))
	var stack []byte
	for i := 0; i < len(rendered); i++ {
		ch := rendered[i]
		switch ch {
		case '[', '(', '{':
			stack = append(stack, ch)
		case ']', ')', '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		builder.WriteByte(ch)
		if ch == ',' && i+1 < len(rendered) && rendered[i+1] == ' ' && len(stack) > 0 && stack[len(stack)-1] == '[' {
			i++
		}
	}
	return builder.String()
}

// convertType converts a go/types.Type to a reflect.Type, preferring a pre-registered
// native reflect.Type from the symbol registry where available.
//
// Aliases and named types resolve via the registry first. Unmatched types fall through to
// the structural converter for the underlying form.
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes t (types.Type) which is the go/types.Type to convert.
//
// Returns the synthesised or registered reflect.Type for t.
func convertType(ctx context.Context, state *typeConverterState, t types.Type) reflect.Type {
	if state.conversionErr != nil {
		return reflect.TypeFor[any]()
	}
	if err := state.descend(ctx, state.depth); err != nil {
		return state.fail(err)
	}
	state.depth++
	defer func() { state.depth-- }()
	if alias, ok := t.(*types.Alias); ok {
		if rt := ResolveRegisteredType(alias.Obj(), state.symbols); rt != nil {
			return rt
		}
		return convertType(ctx, state, types.Unalias(t))
	}
	if named, ok := t.(*types.Named); ok {
		if rt := convertNamedType(ctx, state, named); rt != nil {
			return rt
		}
		return convertNamedNonStruct(ctx, state, named)
	}
	return convertUnderlying(ctx, state, t.Underlying())
}

// convertNamedNonStruct converts a named non-struct type, guarding against
// self-reference.
//
// Takes state (*typeConverterState) which tracks the types in progress.
// Takes named (*types.Named) which is the type to convert.
//
// Returns the reflect type of the underlying type, with self-references as any.
func convertNamedNonStruct(ctx context.Context, state *typeConverterState, named *types.Named) reflect.Type {
	if state.processing[named] {
		logTypeCycle(ctx, named, state.processing)
		return reflect.TypeFor[any]()
	}
	state.processing[named] = true
	defer delete(state.processing, named)
	return convertUnderlying(ctx, state, named.Underlying())
}

// convertNamedType handles the named-type branch of convertType.
//
// Resolves native backing, pre-registered reflect.Types, well-known interface
// short-circuits, and struct-typed named declarations.
//
// Takes state (*typeConverterState) which carries the active cache and registries.
// Takes named (*types.Named) which is the named type to convert.
//
// Returns reflect.Type which is the resolved type, or nil to signal "fall through to
// convertUnderlying on t.Underlying()".
func convertNamedType(ctx context.Context, state *typeConverterState, named *types.Named) reflect.Type {
	if rt := ResolveNativeBackedType(named.Obj(), state.symbols); rt != nil {
		return rt
	}
	if rt := ResolveRegisteredType(named.Obj(), state.symbols); rt != nil {
		return rt
	}
	if intf, isInterface := named.Underlying().(*types.Interface); isInterface {
		pkgPath := ""
		if named.Obj().Pkg() != nil {
			pkgPath = named.Obj().Pkg().Path()
		}
		if rt, ok := engine.WellKnownNamedInterfaceReflectType(pkgPath, named.Obj().Name()); ok {
			return rt
		}
		registerUserNamedInterfacePipitType(state, pkgPath, named.Obj().Name(), intf)
	}
	if _, isStruct := named.Underlying().(*types.Struct); isStruct {
		return convertNamedStruct(ctx, state, named)
	}
	if pointer, isPointer := named.Underlying().(*types.Pointer); isPointer && pointsBackTo(pointer, named) {
		return reflect.TypeFor[any]()
	}
	if _, isBasic := named.Underlying().(*types.Basic); isBasic {
		if poolType := typemodel.NamedScalarPoolTypeFor(ctx, named); poolType != nil {
			return poolType
		}
	}
	if IsNamedCompositeType(named) && !declaredInHostPackage(state, named) {
		return convertNamedComposite(ctx, state, named)
	}
	return nil
}

// declaredInHostPackage reports whether named belongs to a package the symbol registry
// supplies.
//
// Takes state (*typeConverterState) which holds the symbol registry.
// Takes named (*types.Named) which is the type.
//
// Returns bool which is true for a host package's type.
func declaredInHostPackage(state *typeConverterState, named *types.Named) bool {
	pkg := named.Obj().Pkg()
	return pkg != nil && state.symbols != nil && state.symbols.HasPackage(pkg.Path())
}

// convertNamedComposite converts a named array, slice, map, channel or function type to
// the runtime type typemodel builds for it, so values of the type keep their identity
// when stored in interfaces (see typemodel's namedCompositeRegistry).
//
// A failure to build the type is recorded on state and the underlying type is returned in
// its place, so the caller can fail the compile. A build without the forging code gives
// the type its underlying type's runtime type, as before named composite types had their
// own (typemodel.NamedCompositeTypesSupported).
//
// Takes state (*typeConverterState) which carries the cache and the recursion path.
// Takes named (*types.Named) which IsNamedCompositeType accepted.
//
// Returns reflect.Type which is the named type's runtime type.
func convertNamedComposite(ctx context.Context, state *typeConverterState, named *types.Named) reflect.Type {
	if rt := cachedReflectType(state, named); rt != nil {
		return rt
	}
	if rt, building := state.cycleTypes[named]; building {
		return rt
	}
	if state.processing[named] {
		logTypeCycle(ctx, named, state.processing)
		return reflect.TypeFor[any]()
	}
	members, component, err := state.cycles.membersOf(ctx, state, named)
	if err != nil {
		return state.fail(err)
	}
	if !typemodel.NamedCompositeTypesSupported() {
		return convertSharedNamedComposite(ctx, state, named, members != nil)
	}
	if members != nil {
		return convertNamedCompositeCycle(ctx, state, named, members, component)
	}
	underlying := convertNamedNonStruct(ctx, state, named)
	if state.conversionErr != nil {
		return underlying
	}
	result, err := state.quota().TypeFor(NamedCompositeIdentityOf(named), underlying)
	if err != nil {
		state.fail(err)
		return underlying
	}
	storeReflectType(state, named, result)
	return result
}

// convertSharedNamedComposite converts a named composite type in a build without the
// forging code, where it shares its underlying type's runtime type.
//
// Takes state (*typeConverterState) which carries the cache and the recursion path.
// Takes named (*types.Named) which is the type.
// Takes cyclic (bool) which is true when named reaches itself.
//
// Returns reflect.Type which is the underlying type's runtime type.
func convertSharedNamedComposite(ctx context.Context, state *typeConverterState, named *types.Named, cyclic bool) reflect.Type {
	result := convertNamedNonStruct(ctx, state, named)
	if !cyclic && state.conversionErr == nil {
		storeReflectType(state, named, result)
	}
	return result
}

// IsNamedCompositeType reports whether t is a named array, slice, map, channel or
// function type with no type parameters, which gets a runtime type of its own. A
// self-referential type such as `type List []List` is built with the types it reaches
// (convertNamedCompositeCycle).
//
// Takes t (types.Type) which may be an alias.
//
// Returns bool which is true for such a type.
func IsNamedCompositeType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil {
		return false
	}
	switch named.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map, *types.Chan, *types.Signature:
		return !ContainsTypeParameter(named)
	default:
		return false
	}
}

// NamedCompositeIdentityOf names a named composite type the way the runtime prints it.
//
// Takes named (*types.Named) which is the type.
//
// Returns typemodel.NamedCompositeIdentity which carries its package, its name with any
// type arguments, and its declared identifier.
func NamedCompositeIdentityOf(named *types.Named) typemodel.NamedCompositeIdentity {
	identity := typemodel.NamedCompositeIdentity{
		PkgPath:  "",
		PkgName:  "",
		Name:     RuntimeTypeString(named),
		BareName: named.Obj().Name(),
	}
	if pkg := named.Obj().Pkg(); pkg != nil {
		identity.PkgPath, identity.PkgName = pkg.Path(), pkg.Name()
		identity.Name = strings.TrimPrefix(identity.Name, pkg.Name()+".")
	}
	return identity
}

// registerUserNamedInterfacePipitType publishes a symtab.Type for a user-declared named
// interface into the per-Service registry so the runtime reflect.TypeOf intercept can
// wrap *interface{} results with the source-level identity (pkg.IfaceName, method set,
// etc.) that Go's reflect cannot otherwise preserve.
//
// Called from convertNamedType when the named type's underlying is *types.Interface and
// the type is NOT in the wellKnownNamedInterfaceRegistry. No-op when state.Globals is nil
// (ad-hoc type conversion outside a Service context).
//
// Takes state (*typeConverterState) which must carry a non-nil globals to publish.
// Takes pkgPath (string) which is the defining package's full import path.
// Takes name (string) which is the bare interface name (e.g. "myiface").
// Takes intf (*types.Interface) whose methods are read to populate the symtab.Type
// method-name set.
func registerUserNamedInterfacePipitType(state *typeConverterState, pkgPath, name string, intf *types.Interface) {
	if state == nil || state.globals == nil || name == "" {
		return
	}
	completed := intf.Complete()
	count := completed.NumMethods()
	methods := make([]string, count)
	for i := range count {
		methods[i] = completed.Method(i).Name()
	}
	slices.Sort(methods)
	pipit := typemodel.NewNamedInterfaceType(pkgPath, name, methods)
	state.globals.RegisterUserNamedInterface(pipit.QualifiedName, pipit)
}

// convertUnderlying converts the underlying form of a go/types.Type to a reflect.Type by
// dispatching on the concrete type.
//
// Interfaces and unknown types collapse to reflect.TypeFor[any]().
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes underlying (types.Type) which is the underlying go/types form to convert.
//
// Returns the reflect.Type for the underlying form.
func convertUnderlying(ctx context.Context, state *typeConverterState, underlying types.Type) reflect.Type {
	switch typ := underlying.(type) {
	case *types.Basic:
		return basicToReflect(typ.Kind())
	case *types.Slice:
		return reflect.SliceOf(convertType(ctx, state, typ.Elem()))
	case *types.Map:
		return reflect.MapOf(convertType(ctx, state, typ.Key()), convertType(ctx, state, typ.Elem()))
	case *types.Pointer:
		return reflect.PointerTo(convertType(ctx, state, typ.Elem()))
	case *types.Array:
		return reflect.ArrayOf(int(typ.Len()), convertType(ctx, state, typ.Elem()))
	case *types.Struct:
		return convertStruct(ctx, state, typ)
	case *types.Signature:
		return convertSignature(ctx, state, typ)
	case *types.Chan:
		return convertChannel(ctx, state, typ)
	case *types.Interface:
		return reflect.TypeFor[any]()
	default:
		return reflect.TypeFor[any]()
	}
}

// convertStruct converts an anonymous struct type to a reflect.Type without adding a
// sentinel identity field.
//
// Unexported fields keep their PkgPath so reflect honours their visibility.
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes typ (*types.Struct) which is the anonymous struct type to convert.
//
// Returns the synthesised reflect.Type for typ.
func convertStruct(ctx context.Context, state *typeConverterState, typ *types.Struct) reflect.Type {
	state.processingStructs = append(state.processingStructs, typ)
	defer func() { state.processingStructs = state.processingStructs[:len(state.processingStructs)-1] }()

	return reflect.StructOf(buildStructFields(ctx, state, typ))
}

// convertSignature converts a go/types function signature to a reflect.FuncOf type,
// preserving the variadic flag.
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes typ (*types.Signature) which is the function signature to convert.
//
// Returns the synthesised reflect.Type for typ.
func convertSignature(ctx context.Context, state *typeConverterState, typ *types.Signature) reflect.Type {
	var parameterTypes []reflect.Type
	for v := range typ.Params().Variables() {
		parameterTypes = append(parameterTypes, convertType(ctx, state, v.Type()))
	}
	var resultTypes []reflect.Type
	for v := range typ.Results().Variables() {
		resultTypes = append(resultTypes, convertType(ctx, state, v.Type()))
	}
	return reflect.FuncOf(parameterTypes, resultTypes, typ.Variadic())
}

// convertChannel converts a go/types channel type to a reflect.ChanOf type, preserving
// the channel direction.
//
// Takes state (*typeConverterState) which is the per-call converter state.
// Takes typ (*types.Chan) which is the channel type to convert.
//
// Returns the synthesised reflect.Type for typ.
func convertChannel(ctx context.Context, state *typeConverterState, typ *types.Chan) reflect.Type {
	elementType := convertType(ctx, state, typ.Elem())
	var directory reflect.ChanDir
	switch typ.Dir() {
	case types.SendRecv:
		directory = reflect.BothDir
	case types.SendOnly:
		directory = reflect.SendDir
	case types.RecvOnly:
		directory = reflect.RecvDir
	}
	return reflect.ChanOf(directory, elementType)
}

// fieldTypeInvolvesCycle reports whether fieldType references any named type on the
// active conversion path, transitively through pointers, slices, arrays, maps, channels,
// and aliases.
//
// Takes state (*typeConverterState) which holds the active conversion paths.
// Takes fieldType (types.Type) which is the field type to inspect.
//
// Returns true when fieldType reaches a named type already on the path.
func fieldTypeInvolvesCycle(state *typeConverterState, fieldType types.Type) bool {
	return typeReachesConversionPath(state, fieldType, map[types.Type]bool{})
}

// typeReachesConversionPath is fieldTypeInvolvesCycle's walk, carrying the set of types
// already visited so a type that recurses through itself terminates.
//
// Takes state (*typeConverterState) which holds both conversion paths.
// Takes fieldType (types.Type) which is the type to inspect.
// Takes seen (map[types.Type]bool) which guards against revisiting a type.
//
// Returns bool which is true when fieldType reaches a type being converted.
func typeReachesConversionPath(state *typeConverterState, fieldType types.Type, seen map[types.Type]bool) bool {
	if fieldType == nil || seen[fieldType] {
		return false
	}
	seen[fieldType] = true
	switch t := fieldType.(type) {
	case *types.Named:
		return namedTypeInvolvesCycle(state, t, seen)
	case *types.Struct:
		return structIsOnConversionPath(state, t)
	case *types.Pointer:
		return typeReachesConversionPath(state, t.Elem(), seen)
	case *types.Slice:
		return typeReachesConversionPath(state, t.Elem(), seen)
	case *types.Array:
		return typeReachesConversionPath(state, t.Elem(), seen)
	case *types.Map:
		return typeReachesConversionPath(state, t.Key(), seen) || typeReachesConversionPath(state, t.Elem(), seen)
	case *types.Chan:
		return typeReachesConversionPath(state, t.Elem(), seen)
	case *types.Alias:
		return typeReachesConversionPath(state, types.Unalias(t), seen)
	case *types.Signature:
		return signatureInvolvesCycle(state, t, seen)
	default:
		return false
	}
}

// structIsOnConversionPath reports whether an anonymous struct type is one of those being
// converted right now, compared structurally because a literal type written twice is two
// distinct go/types values.
//
// Takes state (*typeConverterState) which holds the active literal-struct path.
// Takes t (*types.Struct) which is the struct to match.
//
// Returns bool which is true when t is on the path.
func structIsOnConversionPath(state *typeConverterState, t *types.Struct) bool {
	for _, active := range state.processingStructs {
		if active == t || types.Identical(active, t) {
			return true
		}
	}
	return false
}

// namedTypeInvolvesCycle reports whether a *types.Named field reaches the active
// named-cycle set. Named func types descend into their signature so cyclic function-typed
// fields take the cycle-breaking conversion path rather than the noisy logTypeCycle bail.
//
// Takes state (*typeConverterState) which holds the active conversion paths.
// Takes t (*types.Named) which is the named type to inspect.
// Takes seen (map[types.Type]bool) which guards against revisiting a type.
//
// Returns bool which is true when t reaches the processing path.
func namedTypeInvolvesCycle(state *typeConverterState, t *types.Named, seen map[types.Type]bool) bool {
	if state.processing[t] {
		return true
	}
	if _, isSig := t.Underlying().(*types.Signature); isSig {
		return typeReachesConversionPath(state, t.Underlying(), seen)
	}

	if len(state.processingStructs) == 0 {
		return false
	}
	return typeReachesConversionPath(state, t.Underlying(), seen)
}

// signatureInvolvesCycle reports whether either the parameter or the result list of a
// function signature reaches a named type currently on the conversion path (e.g.
// yaml_parser_t.read_handler takes *yaml_parser_t).
//
// Takes state (*typeConverterState) which holds the active conversion paths.
// Takes t (*types.Signature) which is the signature to inspect.
// Takes seen (map[types.Type]bool) which guards against revisiting a type.
//
// Returns bool which is true when any parameter or result type reaches the processing
// path.
func signatureInvolvesCycle(state *typeConverterState, t *types.Signature, seen map[types.Type]bool) bool {
	if params := t.Params(); params != nil {
		for v := range params.Variables() {
			if typeReachesConversionPath(state, v.Type(), seen) {
				return true
			}
		}
	}
	if results := t.Results(); results != nil {
		for v := range results.Variables() {
			if typeReachesConversionPath(state, v.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// cachedReflectType returns the cached reflect.Type for t, or nil when no cache is
// configured or no entry exists.
//
// Takes state (*typeConverterState) which holds the shared cache.
// Takes t (types.Type) which is the go/types.Type whose cached entry is requested.
//
// Returns the cached reflect.Type or nil when absent.
func cachedReflectType(state *typeConverterState, t types.Type) reflect.Type {
	if state.cache == nil {
		return nil
	}
	return state.cache[t]
}

// storeReflectType records rt for t in the shared cache after full synthesis so
// subsequent conversions yield an identical reflect.Type.
//
// No-op when no cache is configured.
//
// Takes state (*typeConverterState) which holds the shared cache.
// Takes t (types.Type) which is the go/types.Type used as the cache key.
// Takes rt (reflect.Type) which is the synthesised reflect.Type to store.
func storeReflectType(state *typeConverterState, t types.Type, rt reflect.Type) {
	if state.cache == nil {
		return
	}
	state.cache[t] = rt
}

// basicToReflect converts a types.BasicKind to the matching reflect.Type, returning
// reflect.TypeFor[any]() for unrecognised kinds.
//
// Takes k (types.BasicKind) which is the basic kind to convert.
//
// Returns the matching reflect.Type or reflect.TypeFor[any]() when unknown.
func basicToReflect(k types.BasicKind) reflect.Type {
	switch k {
	case types.Bool, types.UntypedBool:
		return reflect.TypeFor[bool]()
	case types.Int, types.UntypedInt:
		return reflect.TypeFor[int]()
	case types.Int8:
		return reflect.TypeFor[int8]()
	case types.Int16:
		return reflect.TypeFor[int16]()
	case types.Int32, types.UntypedRune:
		return reflect.TypeFor[int32]()
	case types.Int64:
		return reflect.TypeFor[int64]()
	case types.Uint:
		return reflect.TypeFor[uint]()
	case types.Uint8:
		return reflect.TypeFor[uint8]()
	case types.Uint16:
		return reflect.TypeFor[uint16]()
	case types.Uint32:
		return reflect.TypeFor[uint32]()
	case types.Uint64:
		return reflect.TypeFor[uint64]()
	case types.Uintptr:
		return reflect.TypeFor[uintptr]()
	case types.Float32:
		return reflect.TypeFor[float32]()
	case types.Float64, types.UntypedFloat:
		return reflect.TypeFor[float64]()
	case types.Complex64:
		return reflect.TypeFor[complex64]()
	case types.Complex128, types.UntypedComplex:
		return reflect.TypeFor[complex128]()
	case types.String, types.UntypedString:
		return reflect.TypeFor[string]()
	case types.UnsafePointer:
		return reflect.TypeFor[unsafe.Pointer]()
	default:
		return reflect.TypeFor[any]()
	}
}

// containsTypeParameter is the cycle-guarded worker behind ContainsTypeParameter.
//
// A recursive generic type such as "type Node[T any] struct { next *Node[T] }" would
// otherwise recurse forever once struct fields are walked, so each type is visited once.
//
// Takes t (types.Type) which is the type to inspect.
// Takes seen (map[types.Type]struct{}) which records the types already visited on this
// walk.
//
// Returns true when t directly is or transitively contains a TypeParam.
func containsTypeParameter(t types.Type, seen map[types.Type]struct{}) bool {
	if t == nil {
		return false
	}
	if IsTypeParameter(t) {
		return true
	}
	if _, visited := seen[t]; visited {
		return false
	}
	seen[t] = struct{}{}

	if instantiationContainsTypeParameter(t, seen) {
		return true
	}

	switch u := t.Underlying().(type) {
	case *types.Slice:
		return containsTypeParameter(u.Elem(), seen)
	case *types.Array:
		return containsTypeParameter(u.Elem(), seen)
	case *types.Map:
		return containsTypeParameter(u.Key(), seen) || containsTypeParameter(u.Elem(), seen)
	case *types.Chan:
		return containsTypeParameter(u.Elem(), seen)
	case *types.Pointer:
		return containsTypeParameter(u.Elem(), seen)
	case *types.Struct:
		return structContainsTypeParameter(u, seen)
	case *types.Signature:
		return signatureContainsTypeParameter(u, seen)
	}
	return false
}

// instantiationContainsTypeParameter reports whether a named type's instantiation
// arguments mention a type parameter.
//
// Those arguments live on the Named itself rather than on its underlying type, so List[T]
// has to be inspected here because the underlying struct it expands to no longer names T.
//
// Takes t (types.Type) which is the type to inspect.
// Takes seen (map[types.Type]struct{}) which records the types already visited.
//
// Returns true when any instantiation argument contains a TypeParam.
func instantiationContainsTypeParameter(t types.Type, seen map[types.Type]struct{}) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	for argument := range named.TypeArgs().Types() {
		if containsTypeParameter(argument, seen) {
			return true
		}
	}
	return false
}

// structContainsTypeParameter reports whether any field of a struct mentions a type
// parameter.
//
// Takes structure (*types.Struct) which is the struct to inspect.
// Takes seen (map[types.Type]struct{}) which records the types already visited.
//
// Returns true when any field type contains a TypeParam.
func structContainsTypeParameter(structure *types.Struct, seen map[types.Type]struct{}) bool {
	for field := range structure.Fields() {
		if containsTypeParameter(field.Type(), seen) {
			return true
		}
	}
	return false
}

// signatureContainsTypeParameter reports whether a function signature mentions a type
// parameter in any parameter or result slot.
//
// Takes signature (*types.Signature) which is the signature to inspect.
// Takes seen (map[types.Type]struct{}) which records the types already visited on this
// walk.
//
// Returns true when any parameter or result type contains a TypeParam.
func signatureContainsTypeParameter(signature *types.Signature, seen map[types.Type]struct{}) bool {
	for _, tuple := range []*types.Tuple{signature.Params(), signature.Results()} {
		if tuple == nil {
			continue
		}
		for variable := range tuple.Variables() {
			if containsTypeParameter(variable.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// canonicalType is the memoised worker behind CanonicalType.
//
// Takes t (types.Type) which is the type to canonicalise.
// Takes seen (map[types.Type]types.Type) which memoises results and breaks reference
// cycles through recursive composite types.
//
// Returns the canonical type, or t when nothing needed rewriting.
func canonicalType(t types.Type, seen map[types.Type]types.Type) types.Type {
	if t == nil {
		return nil
	}
	if cached, ok := seen[t]; ok {
		return cached
	}
	unaliased := types.Unalias(t)
	if basic, ok := unaliased.(*types.Basic); ok {
		if int(basic.Kind()) < len(types.Typ) {
			return types.Typ[basic.Kind()]
		}
		return basic
	}
	seen[t] = unaliased

	canonical := rebuildCanonicalComposite(unaliased, seen)
	seen[t] = canonical
	return canonical
}

// rebuildCanonicalComposite canonicalises the components of a composite type, rebuilding
// it only when a component changed.
//
// Takes t (types.Type) which is the already-unaliased type to inspect.
// Takes seen (map[types.Type]types.Type) which memoises results.
//
// Returns the rebuilt type, or t when no component changed.
func rebuildCanonicalComposite(t types.Type, seen map[types.Type]types.Type) types.Type {
	switch u := t.(type) {
	case *types.Slice:
		if elem := canonicalType(u.Elem(), seen); elem != u.Elem() {
			return types.NewSlice(elem)
		}
	case *types.Array:
		if elem := canonicalType(u.Elem(), seen); elem != u.Elem() {
			return types.NewArray(elem, u.Len())
		}
	case *types.Pointer:
		if elem := canonicalType(u.Elem(), seen); elem != u.Elem() {
			return types.NewPointer(elem)
		}
	case *types.Chan:
		if elem := canonicalType(u.Elem(), seen); elem != u.Elem() {
			return types.NewChan(u.Dir(), elem)
		}
	case *types.Map:
		key := canonicalType(u.Key(), seen)
		elem := canonicalType(u.Elem(), seen)
		if key != u.Key() || elem != u.Elem() {
			return types.NewMap(key, elem)
		}
	}
	return t
}

// pointsBackTo reports whether following pointer's element through any further pointer
// levels reaches named itself (`type N *N`, `type N **N`).
//
// Takes pointer (*types.Pointer) which is the named type's underlying pointer type.
// Takes named (*types.Named) which is the type being converted.
//
// Returns bool which is true for a self-referential pointer type.
func pointsBackTo(pointer *types.Pointer, named *types.Named) bool {
	element := types.Unalias(pointer.Elem())
	for {
		if types.Identical(element, named) {
			return true
		}
		next, ok := element.(*types.Pointer)
		if !ok {
			return false
		}
		element = types.Unalias(next.Elem())
	}
}
