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

package engine

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// fmtVerbSharpV marks a `%#v` operand: Go consults GoStringer for it, never String or
	// Error, so it is kept apart from the plain 'v' verb.
	fmtVerbSharpV byte = 'V'

	// decimalBase is the radix of an explicit operand index inside a format string.
	decimalBase = 10
)

// pipitFmtValue wraps a pipit-synthesised struct for fmt printing.
//
// Routes fmt's %v / %+v / %s verbs through a Format method that skips the `_pipitID_`
// sentinel field pipit appends to every synthesised struct. Recursively wraps nested
// struct fields so they print cleanly all the way down.
type pipitFmtValue struct {
	// underlying is the pipit-synthesised struct value being printed through the
	// sentinel-aware formatter.
	underlying reflect.Value

	// vm is the owning virtual machine, used to dispatch a source-level GoString method for
	// the `%#v` verb. nil when the wrapper was built without VM context (the `%#v` path then
	// falls back to the structural Go-syntax renderer).
	vm *VM

	// typeName is the pipit source-level type name of underlying, used to resolve a GoString
	// method in the VM method table.
	typeName string
}

// Format implements fmt.Formatter for pipitFmtValue.
//
// Reproduces fmt's default struct formatting shape for `%v` (e.g. `{1 2}`) and `%+v`
// (e.g. `{X:1 Y:2}`), but skips fields whose name has the `_pipitID_` prefix. Defers to
// the user's own `String()` method when the verb is `%s` and the underlying value has one
// (pipit-side or native). Falls back to fmt's default printer for verbs the wrapper does
// not recognise.
//
// Takes state (fmt.State) which carries flags, width, and precision from the original
// call site.
// Takes verb (rune) which is the format verb selected by the caller.
func (w pipitFmtValue) Format(state fmt.State, verb rune) {
	if !w.underlying.IsValid() {
		writeFormatFallback(state, verb, nil)
		return
	}
	switch verb {
	case 'v':
		if state.Flag('#') {
			w.formatGoSyntax(state)
			return
		}
		pipitFmtRenderer{vm: w.vm}.writeStructPipitSafe(state, w.underlying, state.Flag('+'))
	case 's':
		pipitFmtRenderer{vm: w.vm}.writeStructPipitSafe(state, w.underlying, state.Flag('+'))
	default:
		writeFormatFallback(state, verb, w.underlying.Interface())
	}
}

// formatGoSyntax renders the `%#v` (Go-syntax) representation of the wrapped value. When
// the source type declares a `GoString() string` method (fmt.GoStringer) it dispatches
// that method and writes the result verbatim; otherwise it emits a structural rendering
// `main.Type{Field:value, ...}` with the sentinel field hidden and each field itself
// rendered with `%#v`.
//
// Takes state (fmt.State) which receives the rendered output.
func (w pipitFmtValue) formatGoSyntax(state fmt.State) {
	if w.vm != nil && w.typeName != "" {
		if methodRoot, methodIndex, ok := lookupAdapterMethod(w.vm, w.typeName+".GoString"); ok {
			_, _ = fmt.Fprint(state, invokeStringReturnMethod(w.vm, methodRoot, methodIndex, w.underlying))
			return
		}
	}
	writeStructGoSyntax(state, w.underlying)
}

// fmtInterceptState carries the per-scan bookkeeping interceptFmtFormat keeps live across
// the runes of one format string: the rune view, the current implicit-argument cursor,
// and whether any %T rewrite happened.
type fmtInterceptState struct {
	// runes is the format string under inspection as a rune slice.
	runes []rune

	// implicitArgIndex is the cursor into args for the next implicit verb.
	implicitArgIndex int

	// intercepted records whether any %T rewrite happened during the scan.
	intercepted bool
}

// processFormatRune advances the format-string scan by one logical step: copies plain
// text, handles `%%`, decodes a single verb, and rewrites it when it is a `%T`.
//
// Takes rewritten which receives the emitted output bytes.
// Takes cursor which is the current rune position into state.runes.
// Takes site which provides argument static-type strings.
// Takes siteArgOffset which offsets variadic args within site arguments.
// Takes arguments which is the original argument slice; mutated on rewrite.
//
// Returns the cursor position the outer loop should resume at.
func (state *fmtInterceptState) processFormatRune(rewritten *strings.Builder, cursor int, site *program.CallSite, siteArgOffset int, arguments []any) int {
	current := state.runes[cursor]
	if current != '%' {
		_, _ = rewritten.WriteRune(current)
		return cursor
	}
	if cursor+1 < len(state.runes) && state.runes[cursor+1] == '%' {
		_, _ = rewritten.WriteString("%%")
		return cursor + 1
	}
	verbCursor := cursor + 1
	explicitIndex, advancedCursor, ok := state.parseExplicitIndex(rewritten, cursor, verbCursor)
	if !ok {
		return cursor
	}
	verbCursor = advancedCursor
	flagEnd := skipVerbFlagsAndWidth(state.runes, verbCursor)
	if flagEnd >= len(state.runes) {
		_, _ = rewritten.WriteRune(current)
		return cursor
	}
	verbRune := state.runes[flagEnd]

	starArgs := countVerbStarArgs(state.runes, verbCursor, flagEnd)
	var argumentIndex int
	if explicitIndex >= 0 {
		argumentIndex = explicitIndex + starArgs
		state.implicitArgIndex = explicitIndex + starArgs + 1
	} else {
		argumentIndex = state.implicitArgIndex + starArgs
		state.implicitArgIndex += starArgs + 1
	}

	if verbRune != 'T' || argumentIndex < 0 || argumentIndex >= len(arguments) {
		_, _ = rewritten.WriteString(string(state.runes[cursor : flagEnd+1]))
		return flagEnd
	}
	typeText := typeStringForFmtT(site, siteArgOffset+argumentIndex, arguments[argumentIndex])
	state.writeRewrittenVerb(rewritten, verbCursor, flagEnd, explicitIndex)
	arguments[argumentIndex] = typeText
	state.intercepted = true
	return flagEnd
}

// parseExplicitIndex consumes an optional `[N]` argument-index prefix.
//
// Takes rewritten which receives the leading `%` on malformed prefixes.
// Takes percentCursor which is the position of the leading `%` rune.
// Takes verbCursor which is the position immediately after the `%`.
//
// Returns the parsed zero-based index (or -1 when absent), the cursor just past the
// prefix, and false when the prefix was malformed and the caller should write the leading
// `%` and continue.
func (state *fmtInterceptState) parseExplicitIndex(rewritten *strings.Builder, percentCursor int, verbCursor int) (explicitIndex int, nextCursor int, ok bool) {
	if verbCursor >= len(state.runes) || state.runes[verbCursor] != '[' {
		return -1, verbCursor, true
	}
	closingBracket := indexOfRune(state.runes, verbCursor+1, ']')
	if closingBracket < 0 {
		_, _ = rewritten.WriteRune(state.runes[percentCursor])
		return 0, percentCursor, false
	}
	parsed, parsedOK := parsePositiveInt(string(state.runes[verbCursor+1 : closingBracket]))
	if !parsedOK {
		_, _ = rewritten.WriteRune(state.runes[percentCursor])
		return 0, percentCursor, false
	}
	return parsed - 1, closingBracket + 1, true
}

// writeRewrittenVerb emits the rewritten `%[...]s` form for a `%T` interception,
// preserving flags/width/precision between the verb start and the verb rune.
//
// Takes rewritten which receives the rebuilt verb fragment.
// Takes verbCursor which is the position of the verb's flag region.
// Takes flagEnd which is the position of the verb rune itself.
// Takes explicitIndex which is the zero-based [N] index or -1 when absent.
func (state *fmtInterceptState) writeRewrittenVerb(rewritten *strings.Builder, verbCursor int, flagEnd int, explicitIndex int) {
	_, _ = rewritten.WriteString("%")
	if explicitIndex >= 0 {
		_, _ = fmt.Fprintf(rewritten, "[%d]", explicitIndex+1)
	}
	_, _ = rewritten.WriteString(string(state.runes[verbCursor:flagEnd]))
	_, _ = rewritten.WriteRune('s')
}

// pipitFmtRenderer renders script values for fmt, carrying the VM so leaf methods print
// correctly.
type pipitFmtRenderer struct {
	// vm is the virtual machine whose method tables are consulted.
	vm *VM
}

// writeStructPipitSafe prints a struct value with the pipit sentinel field hidden.
//
// Reproduces fmt's default `%v` shape but skips any field whose name begins with the
// `_pipitID_` sentinel prefix. When verbose is true, field names are included (mirroring
// `%+v`). Nested struct fields recurse through the same printer so the sentinel-skip
// applies at every level.
//
// Takes writer (fmt.State) which receives the rendered output.
// Takes value (reflect.Value) which is the struct value to print.
// Takes verbose (bool) which selects the `%+v` shape when true.
func (r pipitFmtRenderer) writeStructPipitSafe(writer fmt.State, value reflect.Value, verbose bool) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			_, _ = fmt.Fprint(writer, "<nil>")
			return
		}
		_, _ = fmt.Fprint(writer, "&")
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		r.writeAnyValue(writer, value, verbose)
		return
	}
	structType := value.Type()
	_, _ = fmt.Fprint(writer, "{")
	firstFieldWritten := false
	for index := range structType.NumField() {
		fieldType := structType.Field(index)
		if strings.HasPrefix(fieldType.Name, pipitIDFieldPrefix) {
			continue
		}
		if firstFieldWritten {
			_, _ = fmt.Fprint(writer, " ")
		}
		firstFieldWritten = true
		if verbose {
			_, _ = fmt.Fprintf(writer, "%s:", pipitSourceFieldName(fieldType.Name))
		}
		fieldValue := value.Field(index)
		r.writeAnyValue(writer, fieldValue, verbose)
	}
	_, _ = fmt.Fprint(writer, "}")
}

// writeAnyValue prints a single reflect.Value, recursing into nested pipit-synth structs
// through writeStructPipitSafe and deferring to fmt's defaults for everything else.
//
// Takes writer (fmt.State) which receives the rendered output.
// Takes value (reflect.Value) which is the value to print.
// Takes verbose (bool) which selects the `%+v` shape when true.
func (r pipitFmtRenderer) writeAnyValue(writer fmt.State, value reflect.Value, verbose bool) {
	if !value.IsValid() {
		_, _ = fmt.Fprint(writer, "<invalid>")
		return
	}
	if text, ok := r.leafMethodText(value); ok {
		_, _ = fmt.Fprint(writer, text)
		return
	}
	if value.Kind() == reflect.Pointer && !value.IsNil() && isPipitSynthesisedReflectType(value.Type()) {
		r.writeStructPipitSafe(writer, value, verbose)
		return
	}
	if value.Kind() == reflect.Struct && isPipitSynthesisedReflectType(value.Type()) {
		r.writeStructPipitSafe(writer, value, verbose)
		return
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		if typeContainsPipitSynthesised(value.Type()) {
			r.writeSlicePipitSafe(writer, value, verbose)
			return
		}
	case reflect.Map:
		if typeContainsPipitSynthesised(value.Type()) {
			r.writeMapPipitSafe(writer, value, verbose)
			return
		}
	default:
	}
	if verbose {
		_, _ = fmt.Fprintf(writer, "%+v", fmtArgFromValue(value))
		return
	}
	_, _ = fmt.Fprintf(writer, "%v", fmtArgFromValue(value))
}

// writeSlicePipitSafe renders a slice or array as `[e0 e1 ...]`.
//
// Each element recurses through writeAnyValue() so nested pipit-synth struct elements
// skip the sentinel field.
//
// Takes writer (fmt.State) which receives the rendered output.
// Takes value (reflect.Value) which is the slice or array to print.
// Takes verbose (bool) which selects the `%+v` shape when true.
func (r pipitFmtRenderer) writeSlicePipitSafe(writer fmt.State, value reflect.Value, verbose bool) {
	_, _ = fmt.Fprint(writer, "[")
	for index := range value.Len() {
		if index > 0 {
			_, _ = fmt.Fprint(writer, " ")
		}
		r.writeAnyValue(writer, value.Index(index), verbose)
	}
	_, _ = fmt.Fprint(writer, "]")
}

// writeMapPipitSafe renders a map as fmt's default `map[k0:v0 k1:v1 ...]` with keys
// ordered the way fmt orders them, recursing into each value (and key) through
// writeAnyValue so a pipit-synth struct in either position skips its sentinel field.
//
// Takes writer (fmt.State) which receives the rendered output.
// Takes value (reflect.Value) which is the map to print.
// Takes verbose (bool) which selects the `%+v` shape when true.
func (r pipitFmtRenderer) writeMapPipitSafe(writer fmt.State, value reflect.Value, verbose bool) {
	_, _ = fmt.Fprint(writer, "map[")
	keys := value.MapKeys()
	slices.SortFunc(keys, reflectMapKeyCompare)
	for index, key := range keys {
		if index > 0 {
			_, _ = fmt.Fprint(writer, " ")
		}
		r.writeAnyValue(writer, key, verbose)
		_, _ = fmt.Fprint(writer, ":")
		r.writeAnyValue(writer, value.MapIndex(key), verbose)
	}
	_, _ = fmt.Fprint(writer, "]")
}

// leafMethodText renders a leaf through its Error or String method when the leaf's script
// type declares one that a value receiver can run; Error wins over String as in fmt.
//
// Takes value (reflect.Value) which is the leaf.
//
// Returns the rendered text and true, or false when the leaf has no such method.
func (r pipitFmtRenderer) leafMethodText(value reflect.Value) (string, bool) {
	if r.vm == nil || r.vm.rootFunction == nil {
		return "", false
	}
	typeName := ""
	if info, ok := typemodel.LookupNamedScalarPoolInfo(value.Type()); ok {
		typeName = info.BareName
	} else if isPipitSynthesisedReflectType(value.Type()) {
		typeName = bareSentinelName(value.Type())
	}
	if typeName == "" {
		return "", false
	}
	for _, method := range [...]string{"Error", "String"} {
		methodRoot, methodIndex, ok := lookupAdapterMethod(r.vm, typeName+"."+method)
		if !ok || !methodReceiverSatisfiesValueIn(methodRoot, r.vm, methodIndex, value) {
			continue
		}
		return invokeStringReturnMethod(r.vm, methodRoot, methodIndex, value), true
	}
	return "", false
}

// fmtVerbScanner walks a format string the way fmt does and records the verb of each
// operand it reaches.
type fmtVerbScanner struct {
	// format is the format string.
	format string

	// verbs receives one verb per operand.
	verbs []byte

	// i is the scan position.
	i int

	// argument is the index of the operand the next verb applies to.
	argument int
}

// assign records verb for operand index when it exists.
//
// Takes index (int) which is the operand position.
// Takes verb (byte) which is the verb to record.
func (s *fmtVerbScanner) assign(index int, verb byte) {
	if index >= 0 && index < len(s.verbs) {
		s.verbs[index] = verb
	}
}

// scanDirective consumes one directive after its `%`: flags, an operand index, a width, a
// precision and the verb, charging `*` widths and precisions to their own operands.
func (s *fmtVerbScanner) scanDirective() {
	sharp := s.skipFlags()
	s.skipOperandIndex()
	s.skipNumberOrStar()
	if s.i < len(s.format) && s.format[s.i] == '.' {
		s.i++
		s.skipOperandIndex()
		s.skipNumberOrStar()
	}
	s.skipOperandIndex()
	if s.i >= len(s.format) {
		return
	}
	verb := s.format[s.i]
	s.i++
	if verb == '%' {
		return
	}
	if verb == 'v' && sharp {
		verb = fmtVerbSharpV
	}
	s.assign(s.argument, verb)
	s.argument++
}

// skipFlags consumes the directive's flags.
//
// Returns bool which is true when the `#` flag was present.
func (s *fmtVerbScanner) skipFlags() bool {
	sharp := false
	for s.i < len(s.format) && strings.IndexByte("+-# 0", s.format[s.i]) >= 0 {
		if s.format[s.i] == '#' {
			sharp = true
		}
		s.i++
	}
	return sharp
}

// skipOperandIndex consumes an explicit `[n]` operand index, if present.
func (s *fmtVerbScanner) skipOperandIndex() {
	s.i, s.argument = fmtSkipOperandIndex(s.format, s.i, s.argument)
}

// skipNumberOrStar consumes a width or precision: a `*` charges an operand, digits are
// skipped.
func (s *fmtVerbScanner) skipNumberOrStar() {
	if s.i < len(s.format) && s.format[s.i] == '*' {
		s.assign(s.argument, '*')
		s.argument++
		s.i++
		return
	}
	for s.i < len(s.format) && s.format[s.i] >= '0' && s.format[s.i] <= '9' {
		s.i++
	}
}

// countVerbStarArgs counts the `*` width/precision markers in a verb's specification,
// each of which consumes one argument in fmt before the verb's own argument.
//
// Takes runes ([]rune) which is the format string being scanned.
// Takes verbCursor (int) which is the start of the verb specification.
// Takes flagEnd (int) which is the end of the verb specification.
//
// Returns int which is the number of `*` markers found.
func countVerbStarArgs(runes []rune, verbCursor, flagEnd int) int {
	count := 0
	for i := verbCursor; i < flagEnd; i++ {
		if runes[i] == '*' {
			count++
		}
	}
	return count
}

// writeStructGoSyntax prints a pipit-synthesised struct in Go-syntax (`%#v`) form: a
// package-qualified type name followed by brace-delimited `Field:value` pairs, the
// `_pipitID_` sentinel field hidden, each field value rendered recursively with `%#v` so
// strings are quoted and nested structs expand.
//
// Takes writer (fmt.State) which receives the output.
// Takes value (reflect.Value) which is the struct (or pointer to struct) to render.
func writeStructGoSyntax(writer fmt.State, value reflect.Value) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			_, _ = fmt.Fprint(writer, "(nil)")
			return
		}
		_, _ = fmt.Fprint(writer, "&")
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		_, _ = fmt.Fprintf(writer, "%#v", fmtArgFromValue(value))
		return
	}
	structType := value.Type()
	typeName := extractPipitSentinelTypeName(structType)
	if typeName == "" {
		typeName = structType.String()
	}
	_, _ = fmt.Fprintf(writer, "%s{", typeName)
	firstFieldWritten := false
	for index := range structType.NumField() {
		fieldType := structType.Field(index)
		if strings.HasPrefix(fieldType.Name, pipitIDFieldPrefix) {
			continue
		}
		if firstFieldWritten {
			_, _ = fmt.Fprint(writer, ", ")
		}
		firstFieldWritten = true
		fieldValue := value.Field(index)
		if isPipitSynthesisedReflectType(derefReflectType(fieldValue.Type())) {
			_, _ = fmt.Fprintf(writer, "%s:", pipitSourceFieldName(fieldType.Name))
			writeStructGoSyntax(writer, fieldValue)
			continue
		}
		_, _ = fmt.Fprintf(writer, "%s:%#v", pipitSourceFieldName(fieldType.Name), fmtArgFromValue(fieldValue))
	}
	_, _ = fmt.Fprint(writer, "}")
}

// derefReflectType returns t with one pointer layer removed, or t unchanged when it is
// not a pointer.
//
// Takes t (reflect.Type).
//
// Returns the pointee type for a pointer, else t.
func derefReflectType(t reflect.Type) reflect.Type {
	if t != nil && t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}

// reflectMapKeyCompare orders two map keys for fmt-style printing.
//
// Keys in one map share a kind, so switching on the first key suffices. Integer, unsigned
// and float kinds compare by value, strings compare lexically, bools order false before
// true, and everything else compares by rendered form.
//
// Takes a (reflect.Value) which is the first key.
// Takes b (reflect.Value) which is the second key.
//
// Returns a negative, zero or positive int per slices.SortFunc().
func reflectMapKeyCompare(a, b reflect.Value) int {
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmp.Compare(a.Int(), b.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cmp.Compare(a.Uint(), b.Uint())
	case reflect.Float32, reflect.Float64:
		return cmp.Compare(a.Float(), b.Float())
	case reflect.String:
		return cmp.Compare(a.String(), b.String())
	case reflect.Bool:
		switch {
		case a.Bool() == b.Bool():
			return 0
		case b.Bool():
			return -1
		default:
			return 1
		}
	default:
		return cmp.Compare(fmt.Sprint(fmtArgFromValue(a)), fmt.Sprint(fmtArgFromValue(b)))
	}
}

// fmtArgFromValue converts a reflect.Value into an argument fmt can print. Read-only
// values (pipit lowercases struct field names, making them unexported to reflect) are
// passed as the reflect.Value itself, because fmt's reflection walker tolerates them.
//
// Takes value (reflect.Value) which is the field or element to print.
//
// Returns the argument to hand to fmt.
func fmtArgFromValue(value reflect.Value) any {
	if !value.CanInterface() {
		return value
	}
	return value.Interface()
}

// writeFormatFallback defers to fmt's default printer for unknown verbs. Mirrors the
// `%!verb(value)` shape fmt itself emits when no Format method is involved, but routed
// through `Fprintf` so width and precision flags on `state` propagate as expected.
//
// Takes state (fmt.State) which carries flags, width, and precision.
// Takes verb (rune) which is the unfamiliar verb being rendered.
// Takes value (any) which is the argument to print.
func writeFormatFallback(state fmt.State, verb rune, value any) {
	format := reconstructVerb(state, verb)
	fmt.Fprintf(state, format, value)
}

// reconstructVerb rebuilds the original `%<flags><width>.<prec><verb>` substring from a
// fmt.State so that a fallback call to Fprintf honours the same width / precision / flags
// the caller specified. Used only on unfamiliar verbs, so a tiny allocation per call is
// acceptable.
//
// Takes state (fmt.State) which carries flags, width, and precision.
// Takes verb (rune) which is the trailing verb rune.
//
// Returns the reconstructed verb fragment beginning with `%`.
func reconstructVerb(state fmt.State, verb rune) string {
	var builder strings.Builder
	builder.WriteByte('%')
	for _, flag := range "+-# 0" {
		if state.Flag(int(flag)) {
			_, _ = builder.WriteRune(flag)
		}
	}
	if width, ok := state.Width(); ok {
		_, _ = fmt.Fprintf(&builder, "%d", width)
	}
	if precision, ok := state.Precision(); ok {
		_, _ = fmt.Fprintf(&builder, ".%d", precision)
	}
	_, _ = builder.WriteRune(verb)
	return builder.String()
}

// restoreNamedTypeForFmt re-clothes a scalar fmt argument with its source-level named
// type so fmt can find a Stringer method on it, because typed register banks strip
// named-type identity (e.g. reflect.Kind becomes raw uint64) and the call site's static
// type string is needed to reconstitute it.
//
// Takes vm (*VM) which provides the symbol registry.
// Takes argument (any) which is the boxed scalar from a register.
// Takes staticTypeString (string) which is the Compiler-recorded type as Go syntax (e.g.
// `"reflect.Kind"`, `"time.Duration"`).
//
// Returns the restored typed-value `any`, or argument unchanged when restoration isn't
// applicable (unregistered type, builtin name, compound type form, kind mismatch).
func restoreNamedTypeForFmt(vm *VM, argument any, staticTypeString string) any {
	if argument == nil || vm == nil || vm.symbols == nil || staticTypeString == "" {
		return argument
	}
	dotIndex := strings.IndexByte(staticTypeString, '.')
	if dotIndex <= 0 || dotIndex >= len(staticTypeString)-1 {
		return argument
	}
	pkgQualifier := staticTypeString[:dotIndex]
	typeName := staticTypeString[dotIndex+1:]
	if strings.ContainsAny(pkgQualifier, "[]*") || strings.ContainsAny(typeName, "[]*") {
		return argument
	}
	namedType, ok := resolveRegisteredNamedType(vm.symbols, pkgQualifier, typeName)
	if !ok {
		return argument
	}
	argValue := reflect.ValueOf(argument)
	if !argValue.IsValid() {
		return argument
	}
	out := reflect.New(namedType).Elem()
	if !setScalarFromValue(out, argValue) {
		return argument
	}
	return out.Interface()
}

// resolveRegisteredNamedType resolves a named type registered in the symbol registry from
// the qualifier recorded in a call site's static type string.
//
// Takes symbols (*symtab.SymbolRegistry) which holds the typed-nil pointer registrations.
// Takes pkgQualifier (string) which is the package path or short name from the static
// type string.
// Takes typeName (string) which is the bare type symbol name.
//
// Returns the registered reflect.Type and true on success, or nil and false on a miss.
func resolveRegisteredNamedType(symbols *symtab.SymbolRegistry, pkgQualifier, typeName string) (reflect.Type, bool) {
	if symbols == nil {
		return nil, false
	}
	if namedType, ok := symbols.ReflectTypeForNamed(pkgQualifier, typeName); ok {
		return namedType, true
	}
	return symbols.ReflectTypeForNamedByPackageName(pkgQualifier, typeName)
}

// setScalarFromValue copies the scalar payload of source into target, which must be a
// settable value of a scalar named type.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (reflect.Value) which holds the boxed scalar payload.
//
// Returns true when the kinds were compatible and the copy succeeded, or false when the
// source kind did not match the target kind.
func setScalarFromValue(target, source reflect.Value) bool {
	switch target.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !source.CanInt() {
			return false
		}
		target.SetInt(source.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if !source.CanUint() {
			return false
		}
		target.SetUint(source.Uint())
	case reflect.Float32, reflect.Float64:
		if !source.CanFloat() {
			return false
		}
		target.SetFloat(source.Float())
	case reflect.String:
		if source.Kind() != reflect.String {
			return false
		}
		target.SetString(source.String())
	case reflect.Bool:
		if source.Kind() != reflect.Bool {
			return false
		}
		target.SetBool(source.Bool())
	default:
		return false
	}
	return true
}

// wrapPipitSynthesisedFmtArg returns a fmt.Formatter wrapper for pipit-synthesised struct
// arguments so the user-visible output skips the `_pipitID_` sentinel field. Other
// argument types flow through unchanged.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (any) which is the raw argument from a register.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns the argument unchanged when no wrap is needed, or a pipitFmtValue wrapping the
// argument when its dynamic type is pipit-synthesised.
func wrapPipitSynthesisedFmtArg(vm *VM, argument any, staticTypeName string) any {
	if argument == nil {
		return nil
	}
	if rt, ok := argument.(reflect.Type); ok {
		if rendered := renderPipitTypeString(rt); rendered != "" {
			return pipitTypeStringer(rendered)
		}
		return argument
	}
	value := reflect.ValueOf(argument)
	if !value.IsValid() {
		return argument
	}
	if !isPipitSynthesisedStructValue(value) {
		return wrapContainerOrScalarForFmt(vm, value, argument, staticTypeName)
	}
	if adapted, ok := resolveFmtArgAdapter(vm, value, staticTypeName); ok {
		return adapted
	}
	return fmtRendererFor(vm, value)
}

// wrapContainerOrScalarForFmt wraps a value that is not itself a synthesised struct for
// fmt printing. Named scalars with adapter methods get the adapter, containers holding
// script values get the renderer, and everything else passes through.
//
// Takes vm (*VM) which resolves script method tables.
// Takes value (reflect.Value) which is the operand.
// Takes argument (any) which is the operand as handed to fmt.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns any which is the value to hand to fmt.
func wrapContainerOrScalarForFmt(vm *VM, value reflect.Value, argument any, staticTypeName string) any {
	if adapted, ok := resolveFmtArgAdapter(vm, value, staticTypeName); ok {
		return adapted
	}
	if typeContainsPipitSynthesised(value.Type()) {
		return pipitFmtValue{underlying: value, vm: vm, typeName: ""}
	}
	return argument
}

// isPipitSynthesisedStructValue reports whether value (after one pointer dereference) is
// a pipit-synthesised struct carrying the `_pipitID_` sentinel field.
//
// Takes value (reflect.Value) which is the candidate argument value.
//
// Returns true when value is a pipit-synthesised struct.
func isPipitSynthesisedStructValue(value reflect.Value) bool {
	probe := value
	if probe.Kind() == reflect.Pointer && !probe.IsNil() {
		probe = probe.Elem()
	}
	if probe.Kind() != reflect.Struct {
		return false
	}
	return isPipitSynthesisedReflectType(probe.Type())
}

// fmtAdapterTypeName resolves the source-level type name a printed operand's Format,
// Error and String methods are registered under. Falls back to the call site's static
// name for non-synthesised types whose named identity is not preserved at runtime.
//
// Takes vm (*VM) which provides the typeNames registry.
// Takes value (reflect.Value) which is the operand.
// Takes staticTypeName (string) which is the call site's recorded name, or "".
//
// Returns the resolved name and true, or "" and false when no name is known.
func fmtAdapterTypeName(vm *VM, value reflect.Value, staticTypeName string) (string, bool) {
	if info, ok := typemodel.LookupNamedScalarPoolInfo(value.Type()); ok {
		return info.BareName, true
	}
	if !isPipitSynthesisedReflectType(value.Type()) {
		return staticTypeName, staticTypeName != ""
	}
	if typeName, ok := pipitTypeName(vm, value); ok {
		return typeName, true
	}
	return staticTypeName, staticTypeName != ""
}

// resolveFmtArgAdapter tries the fmt.Formatter, error, and fmt.Stringer adapters in turn
// for a pipit-synthesised value so a source-level custom formatter is honoured by fmt.
//
// Takes vm (*VM) which provides the method registry.
// Takes value (reflect.Value) which is the pipit-synthesised value.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns the adapter `any` and true when one applies, or nil and false when no adapter
// is registered.
func resolveFmtArgAdapter(vm *VM, value reflect.Value, staticTypeName string) (any, bool) {
	if vm == nil || vm.rootFunction == nil {
		return nil, false
	}
	typeName, ok := fmtAdapterTypeName(vm, value, staticTypeName)
	if !ok {
		return nil, false
	}
	if adapter := buildFormatterAdapterIfRegistered(vm, value, typeName); adapter.IsValid() {
		return adapter.Interface(), true
	}
	if adapter := buildErrorAdapterIfRegistered(vm, value, typeName); adapter.IsValid() {
		return adapter.Interface(), true
	}
	if adapter := buildStringerAdapterIfRegistered(vm, value, typeName); adapter.IsValid() {
		return adapter.Interface(), true
	}
	return nil, false
}

// interceptFmtFormat rewrites a fmt format string for pipit types. Substitutes %T (and
// %[N]T) verbs with the source-level type string recorded on the call site, rewriting to
// %s, because %T resolves through reflect.TypeOf and cannot be intercepted via
// fmt.Formatter.
//
// Takes site (*CallSite) which provides argument static-type strings.
// Takes siteArgOffset (int) which is the offset of the first variadic argument into
// site.ArgumentStaticTypeStrings.
// Takes format (string) which is the original fmt format string.
// Takes arguments ([]any) which is the original argument slice (may be mutated in place
// when a rewrite occurs).
//
// Returns the rewritten format and possibly-mutated arguments plus true when a rewrite
// happened; returns the original inputs and false when no rewrite was needed.
func interceptFmtFormat(site *program.CallSite, siteArgOffset int, format string, arguments []any) (string, []any, bool) {
	if !strings.ContainsRune(format, 'T') {
		return format, arguments, false
	}
	var rewritten strings.Builder
	rewritten.Grow(len(format))
	state := fmtInterceptState{
		runes:            []rune(format),
		implicitArgIndex: 0,
		intercepted:      false,
	}
	for cursor := 0; cursor < len(state.runes); cursor++ {
		cursor = state.processFormatRune(&rewritten, cursor, site, siteArgOffset, arguments)
	}
	if !state.intercepted {
		return format, arguments, false
	}
	return rewritten.String(), arguments, true
}

// typeStringForFmtT chooses the type-string substitution for a %T verb.
//
// Prefers the compile-time argumentStaticTypeStrings entry recorded by the Compiler
// (which preserves source-level names like "int" rather than pipit's runtime "int64").
// Falls back to reflect.TypeOf with the `_pipitID_` sentinel stripped so pipit-synth
// struct types still print their bare name (e.g. "main.Point" rather than `struct { X
// int; Y int; _pipitID_Point struct{} }`).
//
// Takes site (*CallSite) which provides static-type strings.
// Takes argumentIndex (int) which is the index into site.ArgumentStaticTypeStrings.
// Takes value (any) which is the runtime argument value.
//
// Returns the type string suitable for substitution under %s.
func typeStringForFmtT(site *program.CallSite, argumentIndex int, value any) string {
	if site != nil && argumentIndex < len(site.ArgumentStaticTypeStrings) {
		if staticString := site.ArgumentStaticTypeStrings[argumentIndex]; staticString != "" && !staticTypeIsBareInterface(staticString) {
			return staticString
		}
	}
	if value == nil {
		return "<nil>"
	}
	if closure, ok := value.(*RuntimeClosure); ok && closure != nil {
		return closureTypeString(closure)
	}
	if underlying, ok := fmtWrappedUnderlying(value); ok {
		return scriptTypeString(underlying.Type())
	}
	reflectType := reflect.TypeOf(value)
	if reflectType == nil {
		return "<nil>"
	}
	return scriptTypeString(reflectType)
}

// fmtWrappedUnderlying returns the script value behind a fmt adapter or renderer.
//
// Takes value (any) which is the operand fmt received.
//
// Returns the underlying value and true, or false when value is not a wrapper or wraps
// nothing.
func fmtWrappedUnderlying(value any) (reflect.Value, bool) {
	if adapter, ok := value.(fmtAdapterUnwrapper); ok {
		if underlying := adapter.adapterUnderlying(); underlying.IsValid() {
			return underlying, true
		}
	}
	if wrapper, ok := value.(pipitFmtValue); ok && wrapper.underlying.IsValid() {
		return wrapper.underlying, true
	}
	return reflect.Value{}, false
}

// scriptTypeString renders a runtime type the way %T names it: a named scalar by its
// qualified source name, a synthesised struct by its sentinel name, and anything else by
// reflect's rendering with sentinel fields stripped.
//
// Takes t (reflect.Type) which is the type to render.
//
// Returns string which is the rendered name.
func scriptTypeString(t reflect.Type) string {
	if info, ok := typemodel.LookupNamedScalarPoolInfo(t); ok {
		return info.QualifiedName
	}
	if name := extractPipitSentinelTypeName(t); name != "" {
		return name
	}
	return stripPipitSentinelFromTypeString(t.String())
}

// extractPipitSentinelTypeName returns the source-level type name encoded in the
// `_pipitID_<Name>` sentinel field of a pipit-synthesised struct, formatted as
// "main.<Name>". Returns "" when reflectType carries no sentinel.
//
// Takes reflectType (reflect.Type) which is the synthesised type to inspect (pointer
// wrappers are peeled).
//
// Returns the qualified "main.<Name>" type label, or the empty string when no sentinel
// field is found.
func extractPipitSentinelTypeName(reflectType reflect.Type) string {
	if reflectType == nil {
		return ""
	}
	t := reflectType
	pointerPrefix := ""
	if t.Kind() == reflect.Pointer {
		pointerPrefix = "*"
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	for field := range t.Fields() {
		fieldName := field.Name
		if !strings.HasPrefix(fieldName, pipitIDFieldPrefix) {
			continue
		}
		return pointerPrefix + "main." + fieldName[len(pipitIDFieldPrefix):] + sentinelTypeArgsSuffix(t)
	}
	return ""
}

// staticTypeIsBareInterface reports whether the static type names any. Detected so %T
// falls through to reflect.TypeOf instead of printing the literal static string.
//
// Takes staticString (string) which is the Compiler-recorded static type name.
//
// Returns true when staticString names the empty interface in any of its surface forms.
func staticTypeIsBareInterface(staticString string) bool {
	switch staticString {
	case "interface{}", "interface {}", "any":
		return true
	}
	return false
}

// stripPipitSentinelFromTypeString removes the `_pipitID_<Name> struct{}` segment from a
// reflect.Type.String() output.
//
// Best-effort: when the rendered string is just an opaque synthesised struct ("struct {
// ... }") it returns the cleaned string; when the rendered string is already a named type
// it returns the input unchanged.
//
// Takes rendered (string) which is reflect.Type.String() output.
//
// Returns the cleaned type string, or the input unchanged when no sentinel segment is
// present.
func stripPipitSentinelFromTypeString(rendered string) string {
	prefixIndex := strings.Index(rendered, pipitIDFieldPrefix)
	if prefixIndex < 0 {
		return rendered
	}
	delimiter := pipitSentinelFieldEnd(rendered, prefixIndex)
	if delimiter < 0 {
		return rendered
	}
	fieldEnd := delimiter
	for fieldEnd > prefixIndex && rendered[fieldEnd-1] == ' ' {
		fieldEnd--
	}

	if semicolon := strings.LastIndex(rendered[:prefixIndex], ";"); semicolon >= 0 {
		return rendered[:semicolon] + rendered[fieldEnd:]
	}
	openBrace := strings.LastIndex(rendered[:prefixIndex], "{")
	if openBrace < 0 {
		return rendered
	}
	return rendered[:openBrace+1] + strings.TrimPrefix(rendered[fieldEnd:], ";")
}

// pipitSentinelFieldEnd finds the ";" or "}" that closes the sentinel field starting at
// start. The sentinel's own "struct {}" braces are skipped by the depth count.
//
// Takes rendered (string) which is the reflect.Type.String() output.
// Takes start (int) which is the index of the sentinel field's name.
//
// Returns the delimiter's index, or -1 when the string ends mid-field.
func pipitSentinelFieldEnd(rendered string, start int) int {
	depth := 0
	for index := start; index < len(rendered); index++ {
		switch rendered[index] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return index
			}
			depth--
		case ';':
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// skipVerbFlagsAndWidth advances past the optional flag / width / precision characters in
// a fmt verb, returning the cursor at the position of the verb rune itself. Handles `+ -
// # 0 ' '` flags, numeric width / precision, and the `*` indirect-width form.
//
// Takes runes ([]rune) which is the format string as a rune slice.
// Takes cursor (int) which is the starting position immediately after the `%` (or after
// an optional `[N]` argument index).
//
// Returns the rune index of the verb itself, or len(runes) when the format string ends
// mid-verb.
func skipVerbFlagsAndWidth(runes []rune, cursor int) int {
	cursor = skipVerbFlagChars(runes, cursor)
	cursor = skipVerbWidth(runes, cursor)
	if cursor < len(runes) && runes[cursor] == '.' {
		cursor = skipVerbPrecision(runes, cursor+1)
	}
	return cursor
}

// skipVerbFlagChars advances past leading fmt verb flag runes: `+`, `-`, `#`, `0`, and
// space.
//
// Takes runes which is the format string as a rune slice.
// Takes cursor which is the position immediately after the `%`.
//
// Returns the cursor at the first non-flag rune.
func skipVerbFlagChars(runes []rune, cursor int) int {
	for cursor < len(runes) {
		switch runes[cursor] {
		case '+', '-', '#', '0', ' ':
			cursor++
		default:
			return cursor
		}
	}
	return cursor
}

// skipVerbWidth advances past width digits and the `*` indirect-width marker.
//
// Takes runes which is the format string as a rune slice.
// Takes cursor which is the position after any flag runes.
//
// Returns the cursor at the first non-width rune.
func skipVerbWidth(runes []rune, cursor int) int {
	for cursor < len(runes) && runes[cursor] >= '0' && runes[cursor] <= '9' {
		cursor++
	}
	if cursor < len(runes) && runes[cursor] == '*' {
		cursor++
	}
	return cursor
}

// skipVerbPrecision advances past precision digits and the `*` indirect-precision marker.
//
// Takes runes which is the format string as a rune slice.
// Takes cursor which is the position after the `.` separator.
//
// Returns the cursor at the first non-precision rune.
func skipVerbPrecision(runes []rune, cursor int) int {
	for cursor < len(runes) && runes[cursor] >= '0' && runes[cursor] <= '9' {
		cursor++
	}
	if cursor < len(runes) && runes[cursor] == '*' {
		cursor++
	}
	return cursor
}

// indexOfRune returns the index of the first occurrence of target in runes starting at
// cursor, or -1 when not found.
//
// Takes runes ([]rune) which is the haystack.
// Takes cursor (int) which is the starting position.
// Takes target (rune) which is the rune to locate.
//
// Returns the index of the first match, or -1 when target is absent from runes[cursor:].
func indexOfRune(runes []rune, cursor int, target rune) int {
	for index := cursor; index < len(runes); index++ {
		if runes[index] == target {
			return index
		}
	}
	return -1
}

// parsePositiveInt parses a decimal positive integer, returning the value and true on
// success or 0 and false on any parse error.
//
// Takes text (string) which is the candidate decimal digits.
//
// Returns the parsed value and true on success; returns 0 and false when text is empty,
// non-numeric, or evaluates to zero or below.
func parsePositiveInt(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	const decimalRadix = 10
	value := 0
	for _, character := range text {
		if character < '0' || character > '9' {
			return 0, false
		}
		value = value*decimalRadix + int(character-'0')
	}
	if value <= 0 {
		return 0, false
	}
	return value, true
}

// closureTypeString renders a closure's type as Go prints it: the named func type it was
// converted to, else its static signature, else the erased shape.
//
// Takes closure (*RuntimeClosure) which is the boxed closure.
//
// Returns string such as "main.Op" or "func(int) string".
func closureTypeString(closure *RuntimeClosure) string {
	if closure.namedType != "" {
		return "main." + closure.namedType
	}
	if closure.Function != nil && closure.Function.SignatureReflectType != nil {
		return closure.Function.SignatureReflectType.String()
	}
	return "func"
}

// fmtArgumentVerbs pairs each operand of a Printf-style call with the verb that formats
// it, following fmt's scanning rules: flags, `*` widths and precisions (which consume an
// operand of their own), explicit `[n]` operand indexes and `%%`. Operands the format
// never reaches keep 'v', which is how fmt prints extra operands.
//
// Takes format (string) which is the format string.
// Takes count (int) which is the number of operands.
//
// Returns []byte which holds one verb per operand.
func fmtArgumentVerbs(format string, count int) []byte {
	scanner := fmtVerbScanner{format: format, i: 0, argument: 0, verbs: make([]byte, count)}
	for i := range scanner.verbs {
		scanner.verbs[i] = 'v'
	}
	for scanner.i < len(format) {
		if format[scanner.i] != '%' {
			scanner.i++
			continue
		}
		scanner.i++
		scanner.scanDirective()
	}
	return scanner.verbs
}

// fmtSkipOperandIndex consumes an explicit operand index (`[n]`) at position i of format,
// returning the position after it and the zero-based operand it selects; without one the
// current operand stands.
//
// Takes format (string) which is the format string.
// Takes i (int) which is the scan position.
// Takes argument (int) which is the current operand index.
//
// Returns the new scan position and operand index.
func fmtSkipOperandIndex(format string, i, argument int) (position, operand int) {
	if i >= len(format) || format[i] != '[' {
		return i, argument
	}
	end := strings.IndexByte(format[i:], ']')
	if end < 0 {
		return i, argument
	}
	n := 0
	for _, ch := range format[i+1 : i+end] {
		if ch < '0' || ch > '9' {
			return i + end + 1, argument
		}
		n = n*decimalBase + int(ch-'0')
	}
	if n >= 1 {
		argument = n - 1
	}
	return i + end + 1, argument
}

// fmtVerbCallsStringMethods reports whether fmt consults Error and String for verb: it
// does for %v, %s, %x, %X and %q, and %w (Errorf) needs an error operand; every other
// verb (%d, %f, %#v, %t, ...) prints the value itself, with only a Format method still
// honoured.
//
// Takes verb (byte) which is the operand's verb, fmtVerbSharpV for `%#v`.
//
// Returns bool which is true when Error or String would be called.
func fmtVerbCallsStringMethods(verb byte) bool {
	switch verb {
	case 'v', 's', 'x', 'X', 'q', 'w':
		return true
	default:
		return false
	}
}

// wrapPipitSynthesisedFmtArgForVerb wraps a script value for fmt according to the verb
// being applied.
//
// When verb calls String or Error the full adapter pipeline runs. For other verbs only a
// Format method is honoured, so a scalar of a named type is handed over as its own kind
// and a synthesised struct or container keeps the renderer.
//
// Takes vm (*VM) which resolves script method tables.
// Takes argument (any) which is the operand.
// Takes verb (byte) which is the operand's verb.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns any which is the value to hand to fmt.
func wrapPipitSynthesisedFmtArgForVerb(vm *VM, argument any, verb byte, staticTypeName string) any {
	if fmtVerbCallsStringMethods(verb) {
		return wrapPipitSynthesisedFmtArg(vm, argument, staticTypeName)
	}
	value := reflect.ValueOf(argument)
	if !value.IsValid() {
		return argument
	}
	if adapter, ok := formatterAdapterFor(vm, value, staticTypeName); ok {
		return adapter
	}
	if _, isPool := typemodel.LookupNamedScalarPoolInfo(value.Type()); isPool {
		return argument
	}
	if isPipitSynthesisedStructValue(value) || typeContainsPipitSynthesised(value.Type()) {
		return fmtRendererFor(vm, value)
	}
	return argument
}

// formatterAdapterFor mints the Formatter adapter for a script value whose type declares
// a fmt.Formatter Format method.
//
// Takes vm (*VM) which resolves script method tables.
// Takes value (reflect.Value) which is the operand.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns the adapter and true, or false when the type has no such method.
func formatterAdapterFor(vm *VM, value reflect.Value, staticTypeName string) (any, bool) {
	if vm == nil || vm.rootFunction == nil {
		return nil, false
	}
	typeName, ok := fmtAdapterTypeName(vm, value, staticTypeName)
	if !ok {
		return nil, false
	}
	if adapter := buildFormatterAdapterIfRegistered(vm, value, typeName); adapter.IsValid() {
		return adapter.Interface(), true
	}
	return nil, false
}

// fmtRendererFor wraps a script value in the fmt renderer, naming its source type so a
// GoString method is dispatched for %#v.
//
// Takes vm (*VM) which resolves script method tables.
// Takes value (reflect.Value) which is the operand.
//
// Returns pipitFmtValue which formats the value for every verb.
func fmtRendererFor(vm *VM, value reflect.Value) pipitFmtValue {
	wrapped := pipitFmtValue{underlying: value, vm: vm, typeName: ""}
	if vm != nil && vm.rootFunction != nil {
		if name, ok := pipitTypeName(vm, value); ok {
			wrapped.typeName = name
		}
	}
	return wrapped
}
