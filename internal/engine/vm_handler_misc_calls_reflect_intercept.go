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
	"errors"
	"fmt"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// reflectFieldByNameMethod is reflect's FieldByName method, intercepted on both the Type
	// and Value sides so a renamed embedded field answers to its source name.
	reflectFieldByNameMethod = "FieldByName"

	// reflectFieldMethod is reflect's Field method, intercepted to hide the sentinel field
	// and restore an embedded field's source-level identity.
	reflectFieldMethod = "Field"

	// structTagDelete is the DEL byte, the upper bound of the printable range a struct tag
	// key may use. reflect.StructTag.Lookup excludes it, and so does the scan here.
	structTagDelete = 0x7f
)

// tryInterceptPipitReflectTypeMethod filters pipit-synth sentinels from reflect.Type
// method results.
//
// When pipit code calls NumField/Field/Name on a reflect.Type that wraps a pipit
// synthesised struct, the underlying reflect.StructOf type carries an extra
// `_pipitID_<Name>` field that should be hidden from user code.
//
// Takes vm (*VM) which owns the dispatch arena.
// Takes registers (*Registers) which receives the call result.
// Takes site (*CallSite) which describes the return slot and any argument slots needed
// for Field/FieldByIndex.
// Takes receiverValue (reflect.Value) which is the reflect.Type receiver; intercept exits
// early when not a reflect.Type.
// Takes methodName (string) which is the method being invoked on the receiver.
//
// Returns OpResult which is the dispatch outcome when intercepted.
// Returns bool which is true when the call was handled.
func tryInterceptPipitReflectTypeMethod(vm *VM, registers *Registers, site *program.CallSite, receiverValue reflect.Value, methodName string) (OpResult, bool) {
	if !receiverValue.IsValid() {
		return opContinue, false
	}
	if !receiverValue.Type().Implements(reflectTypeReflectType) || !receiverValue.CanInterface() {
		return opContinue, false
	}
	rt, ok := reflect.TypeAssert[reflect.Type](receiverValue)
	if !ok {
		return opContinue, false
	}
	switch methodName {
	case "NumField":
		return interceptReflectTypeNumField(registers, site, rt)
	case reflectFieldMethod:
		return interceptReflectTypeField(vm, registers, site, rt)
	case reflectFieldByNameMethod:
		return interceptReflectTypeFieldByName(vm, registers, site, rt)
	case "Name":
		return interceptReflectTypeName(registers, site, rt)
	case "PkgPath":
		return interceptReflectTypePkgPath(registers, site, rt)
	case "String":
		return interceptReflectTypeString(registers, site, rt)
	case "NumMethod", "Method", "MethodByName":
		return interceptReflectTypeMethods(vm, registers, site, rt, methodName)
	}
	return opContinue, false
}

// interceptReflectTypeNumField handles NumField() on a pipit struct.
//
// Reports the user-visible field count that excludes pipit's internal sentinel fields.
//
// Takes registers (*Registers) which receives the call result.
// Takes site (*CallSite) which describes the return slot.
// Takes rt (reflect.Type) which is the receiver reflect.Type.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptReflectTypeNumField(registers *Registers, site *program.CallSite, rt reflect.Type) (OpResult, bool) {
	if rt.Kind() != reflect.Struct {
		return opContinue, false
	}
	userFieldCount := pipitUserFieldCount(rt)
	if userFieldCount == rt.NumField() {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(userFieldCount)})
	return opContinue, true
}

// interceptReflectTypeField handles Field(i) on a pipit struct.
//
// Bounds-checks the index against the user-visible field count and raises an interpreted
// panic when out of range.
//
// Takes vm (*VM) which owns the dispatch arena.
// Takes registers (*Registers) which receives the call result.
// Takes site (*CallSite) which describes the return and argument slots.
// Takes rt (reflect.Type) which is the receiver reflect.Type.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptReflectTypeField(vm *VM, registers *Registers, site *program.CallSite, rt reflect.Type) (OpResult, bool) {
	if rt.Kind() != reflect.Struct {
		return opContinue, false
	}
	userFieldCount := pipitUserFieldCount(rt)
	if !pipitStructFieldsNeedRewrite(rt, userFieldCount) {
		return opContinue, false
	}
	if len(site.Arguments) < 2 {
		return opContinue, false
	}
	indexValue := registerToReflectValue(vm.Arena, registers, site.Arguments[1].Kind, site.Arguments[1].Register)
	if !indexValue.IsValid() || !indexValue.CanInt() {
		return opContinue, false
	}
	i := int(indexValue.Int())
	if i < 0 || i >= userFieldCount {
		return raiseNativePanicAsInterpreted(vm, "reflect: Field index out of range"), true
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(restorePipitStructField(rt.Field(i)))})
	return opContinue, true
}

// interceptReflectTypeFieldByName handles FieldByName(name) on a pipit struct.
//
// Answers a direct field from the source-level names, so a renamed embedded field is
// found under the name the program declared and never under the marker-prefixed one. A
// name that matches nothing directly is left to native reflect, which still promotes it
// through genuinely anonymous fields.
//
// Takes vm (*VM) which owns the dispatch arena.
// Takes registers (*Registers) which receives the call results.
// Takes site (*CallSite) which describes the return and argument slots.
// Takes rt (reflect.Type) which is the receiver reflect.Type.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptReflectTypeFieldByName(vm *VM, registers *Registers, site *program.CallSite, rt reflect.Type) (OpResult, bool) {
	if rt.Kind() != reflect.Struct {
		return opContinue, false
	}
	userFieldCount := pipitUserFieldCount(rt)
	if !pipitStructFieldsNeedRewrite(rt, userFieldCount) {
		return opContinue, false
	}
	if len(site.Arguments) < 2 {
		return opContinue, false
	}
	nameValue := registerToReflectValue(vm.Arena, registers, site.Arguments[1].Kind, site.Arguments[1].Register)
	if !nameValue.IsValid() || nameValue.Kind() != reflect.String {
		return opContinue, false
	}
	name := nameValue.String()
	for i := range userFieldCount {
		if pipitStructFieldMatches(rt.Field(i), name) {
			field := restorePipitStructField(rt.Field(i))
			storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(field), reflect.ValueOf(true)})
			return opContinue, true
		}
	}
	if !strings.HasPrefix(name, isa.EmbeddedUnexportedPrefix) {
		return opContinue, false
	}
	var missing reflect.StructField
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(missing), reflect.ValueOf(false)})
	return opContinue, true
}

// interceptReflectTypePkgPath answers reflect.Type.PkgPath for a script-declared named
// type.
//
// The runtime type carries no Go package, so the qualifier of its rendered name (`main`
// for `main.User`) is returned. Unnamed types report the empty string as Go does.
//
// Takes registers (*Registers) which receive the result.
// Takes site (*program.CallSite) which names the result register.
// Takes rt (reflect.Type) which is the receiver type.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the type is script-declared.
func interceptReflectTypePkgPath(registers *Registers, site *program.CallSite, rt reflect.Type) (OpResult, bool) {
	if rt.Name() != "" {
		return opContinue, false
	}
	rendered := renderPipitTypeString(rt)
	if rendered == "" || strings.HasPrefix(rendered, "*") || strings.HasPrefix(rendered, "[") || strings.HasPrefix(rendered, "map[") {
		return opContinue, false
	}
	qualifier := ""
	if dot := strings.LastIndex(rendered, "."); dot > 0 {
		qualifier = rendered[:dot]
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(qualifier)})
	return opContinue, true
}

// interceptReflectTypeName handles Name() on a pipit struct.
//
// Substitutes the source-level name recovered from the type's pipit sentinel field when
// the reflect-level Name() is empty.
//
// Takes registers (*Registers) which receives the call result.
// Takes site (*CallSite) which describes the return slot.
// Takes rt (reflect.Type) which is the receiver reflect.Type.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptReflectTypeName(registers *Registers, site *program.CallSite, rt reflect.Type) (OpResult, bool) {
	if info, ok := typemodel.LookupNamedScalarPoolInfo(rt); ok {
		storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(info.BareName)})
		return opContinue, true
	}
	if rt.Name() != "" {
		return opContinue, false
	}
	sentinelName := bareSentinelName(rt)
	if sentinelName == "" {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(sentinelName + sentinelTypeArgsSuffix(rt))})
	return opContinue, true
}

// interceptReflectTypeString handles String() on a pipit struct, recovering the
// package-qualified name from the sentinel field's metadata.
//
// Takes registers (*Registers) which receives the call result.
// Takes site (*CallSite) which describes the return slot.
// Takes rt (reflect.Type) which is the receiver reflect.Type.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptReflectTypeString(registers *Registers, site *program.CallSite, rt reflect.Type) (OpResult, bool) {
	if info, ok := typemodel.LookupNamedScalarPoolInfo(rt); ok {
		storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(info.QualifiedName)})
		return opContinue, true
	}
	qualified := renderPipitTypeString(rt)
	if qualified == "" {
		qualified = qualifiedNameFromSentinel(rt)
	}
	if qualified == "" {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(qualified)})
	return opContinue, true
}

// qualifiedNameFromSentinel reconstructs the source-level "<pkg>.<Name>"
// reflect.Type.String() form for a pipit-synthesised struct type (or a pointer / slice /
// map / array / channel built on one). Returns "" when no sentinel is present so the
// caller can fall through to native rendering.
//
// Pointer wrappers contribute a leading "*"; arrays a "[N]"; slices a "[]"; maps
// "map[K]V"; channels "chan ". Composite forms recurse so `*[]spew.ConfigState` renders
// correctly without sentinel leak.
//
// Takes rt (reflect.Type) which is the type to render.
//
// Returns the rendered form or the empty string.
func qualifiedNameFromSentinel(rt reflect.Type) string {
	if rt == nil {
		return ""
	}
	if prefix, inner, ok := qualifiedNameRecurseInner(rt); ok {
		if inner == "" {
			return ""
		}
		return prefix + inner
	}
	if rt.Kind() != reflect.Struct {
		return ""
	}
	return qualifiedNameFromStructSentinel(rt)
}

// qualifiedNameRecurseInner peels wrapper kinds off a type.
//
// Produces the prefix string plus the inner-name lookup result. ok is false when rt was
// not a wrapper kind, so the caller should fall through to the struct sentinel handler.
//
// Takes rt (reflect.Type) which is the type to inspect.
//
// Returns prefix (string) which is the wrapper rendering.
// Returns inner (string) which is the resolved inner name.
// Returns ok (bool) which is true when rt was a wrapper kind.
func qualifiedNameRecurseInner(rt reflect.Type) (prefix, inner string, ok bool) {
	switch rt.Kind() {
	case reflect.Pointer:
		return "*", qualifiedNameFromSentinel(rt.Elem()), true
	case reflect.Slice:
		return "[]", qualifiedNameFromSentinel(rt.Elem()), true
	case reflect.Array:
		return fmt.Sprintf("[%d]", rt.Len()), qualifiedNameFromSentinel(rt.Elem()), true
	default:
	}
	return "", "", false
}

// qualifiedNameFromStructSentinel rebuilds the qualified name.
//
// Pulls the pipit-id sentinel out of the final field of a struct type and reconstructs
// the "pkgShortName.TypeName" identifier. Returns "" when the struct lacks the sentinel
// suffix that pipit emits via compileStructLiteral.
//
// Takes rt (reflect.Type) which is the struct type to inspect.
//
// Returns string which is the qualified name or empty.
func qualifiedNameFromStructSentinel(rt reflect.Type) string {
	if rt.NumField() == 0 {
		return ""
	}
	last := rt.Field(rt.NumField() - 1)
	if !strings.HasPrefix(last.Name, pipitIDFieldPrefix) {
		return ""
	}
	typeName := last.Name[len(pipitIDFieldPrefix):]
	pkgPath := last.PkgPath
	if suffix := "." + typeName; strings.HasSuffix(pkgPath, suffix) {
		pkgPath = pkgPath[:len(pkgPath)-len(suffix)]
	}
	pkgShortName := pkgPath
	if _, short, found := strings.CutLast(pkgPath, "/"); found {
		pkgShortName = short
	}
	typeName += sentinelTypeArgsSuffix(rt)
	if pkgShortName == "" {
		return typeName
	}
	return pkgShortName + "." + typeName
}

// pipitUserFieldCount returns the number of non-sentinel fields on a pipit-synthesised
// struct type. A type is considered pipit-synthesised when its trailing field name starts
// with the `_pipitID_` sentinel prefix; in that case the count excludes the sentinel.
//
// Takes rt (reflect.Type) which is the struct type to inspect.
//
// Returns the user-visible field count.
func pipitUserFieldCount(rt reflect.Type) int {
	total := rt.NumField()
	if total == 0 {
		return 0
	}
	if strings.HasPrefix(rt.Field(total-1).Name, pipitIDFieldPrefix) {
		return total - 1
	}
	return total
}

// pipitStructFieldsNeedRewrite reports whether pipit must answer the struct-field reflect
// calls for rt rather than leave them to native reflect.
//
// Takes rt (reflect.Type) which is the struct type to inspect.
// Takes userFieldCount (int) which is pipitUserFieldCount(rt).
//
// Returns bool which is true when the field intercepts must answer the call.
func pipitStructFieldsNeedRewrite(rt reflect.Type, userFieldCount int) bool {
	return userFieldCount != rt.NumField() || hasRenamedEmbeddedField(rt)
}

// isRenamedEmbeddedField reports whether a synthesised field is an embedded field carried
// under the marker prefix because reflect.StructOf could not declare it directly.
//
// Takes field (reflect.StructField) which is the synthesised field.
//
// Returns bool which is true when the field is a renamed embedded field.
func isRenamedEmbeddedField(field reflect.StructField) bool {
	return strings.HasPrefix(field.Name, isa.EmbeddedUnexportedPrefix)
}

// pipitSourceFieldName returns the source-level name of a synthesised struct field.
//
// Takes name (string) which is the field name as the synthesised struct carries it.
//
// Returns string which is the name the program being run declared.
func pipitSourceFieldName(name string) string {
	return strings.TrimPrefix(name, isa.EmbeddedUnexportedPrefix)
}

// restorePipitStructField returns field under the identity Go would report for it,
// reversing the EmbeddedUnexportedPrefix rename and removing the CycleBrokenTagKey pair.
//
// Takes field (reflect.StructField) which is the synthesised field.
//
// Returns reflect.StructField which is the field under its source-level identity.
func restorePipitStructField(field reflect.StructField) reflect.StructField {
	field.Tag = reflect.StructTag(stripStructTagKey(string(field.Tag), isa.CycleBrokenTagKey))
	if !strings.HasPrefix(field.Name, isa.EmbeddedUnexportedPrefix) {
		return field
	}
	field.Name = field.Name[len(isa.EmbeddedUnexportedPrefix):]
	field.Anonymous = true
	return field
}

// stripStructTagKey returns tag without the `key:"..."` pair, leaving every other pair in
// its original order.
//
// Takes tag (string) which is the raw struct tag.
// Takes key (string) which is the pair to remove.
//
// Returns string which is the tag without that pair.
func stripStructTagKey(tag, key string) string {
	var rebuilt strings.Builder
	remaining := tag
	for remaining != "" {
		name, pair, rest, ok := nextStructTagPair(remaining)
		if !ok {
			break
		}
		remaining = rest
		if name == key {
			continue
		}
		appendStructTagPair(&rebuilt, pair)
	}

	appendStructTagPair(&rebuilt, strings.TrimLeft(remaining, " "))
	return rebuilt.String()
}

// appendStructTagPair writes pair into rebuilt, separated from anything already there.
//
// Takes rebuilt (*strings.Builder) which accumulates the surviving tag.
// Takes pair (string) which is the text to append; empty text is ignored.
func appendStructTagPair(rebuilt *strings.Builder, pair string) {
	if pair == "" {
		return
	}
	if rebuilt.Len() > 0 {
		rebuilt.WriteByte(' ')
	}
	rebuilt.WriteString(pair)
}

// nextStructTagPair splits the leading `key:"value"` pair off a struct tag.
//
// Mirrors the scan in reflect.StructTag.Lookup: leading spaces are skipped, the key runs
// to the colon, and the value is a quoted string in which a backslash escapes the next
// byte.
//
// Takes tag (string) which is the remaining raw struct tag.
//
// Returns name (string) which is the pair's key.
// Returns pair (string) which is the whole `key:"value"` text.
// Returns rest (string) which is the tag after the pair.
// Returns ok (bool) which is false when no well-formed pair leads the tag.
func nextStructTagPair(tag string) (name, pair, rest string, ok bool) {
	trimmed := strings.TrimLeft(tag, " ")
	if trimmed == "" {
		return "", "", "", false
	}

	colon := structTagKeyEnd(trimmed)
	if colon == 0 || colon+1 >= len(trimmed) || trimmed[colon] != ':' || trimmed[colon+1] != '"' {
		return "", "", "", false
	}

	quoted := trimmed[colon+1:]
	closing := structTagValueEnd(quoted)
	if closing < 0 {
		return "", "", "", false
	}

	return trimmed[:colon], trimmed[:colon+1+closing+1], quoted[closing+1:], true
}

// structTagKeyEnd returns the offset of the colon ending a struct tag's key.
//
// Takes tag (string) which starts at the key.
//
// Returns int which is the offset of the first byte the key may not contain.
func structTagKeyEnd(tag string) int {
	for index := range len(tag) {
		character := tag[index]
		if character <= ' ' || character == ':' || character == '"' || character == structTagDelete {
			return index
		}
	}
	return len(tag)
}

// structTagValueEnd returns the offset of the quote closing a struct tag's value.
//
// Takes quoted (string) which starts at the opening quote.
//
// Returns int which is the offset of the closing quote, or -1 when there is none.
func structTagValueEnd(quoted string) int {
	for index := 1; index < len(quoted); index++ {
		switch quoted[index] {
		case '\\':
			index++
		case '"':
			return index
		}
	}
	return -1
}

// tryInterceptPipitReflectValueMethod handles pipit reflect.Value calls.
//
// A pipit-synthesised struct carries an empty Go-level method set (pipit keeps methods in
// its own method table), so native reflect.Value.MethodByName / NumMethod return nothing.
// Resolves those against pipit's method table and, for MethodByName, hands back a
// callable reflect.Value bound to the pipit method.
//
// Takes vm (*VM) which owns the method table and dispatch context.
// Takes registers (*Registers) which receives the result.
// Takes site (*CallSite) which describes the argument/return slots.
// Takes receiverValue (reflect.Value) which is the reflect.Value receiver wrapping a
// pipit value.
// Takes methodName (string) which is the method invoked on it.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func tryInterceptPipitReflectValueMethod(vm *VM, registers *Registers, site *program.CallSite, receiverValue reflect.Value, methodName string) (OpResult, bool) {
	if !receiverValue.IsValid() || receiverValue.Type() != reflectValueReflectType || !receiverValue.CanInterface() {
		return opContinue, false
	}
	inner, ok := reflect.TypeAssert[reflect.Value](receiverValue)
	if !ok || !inner.IsValid() {
		return opContinue, false
	}
	if methodName == reflectFieldByNameMethod && hasRenamedEmbeddedField(inner.Type()) {
		return interceptPipitReflectFieldByName(vm, registers, site, inner)
	}
	typeName := pipitReflectValueTypeName(vm, inner)
	if typeName == "" {
		return opContinue, false
	}
	switch methodName {
	case "MethodByName":
		return interceptPipitReflectMethodByName(vm, registers, site, inner, typeName)
	case "Method":
		return interceptReflectValueMethodIndex(vm, registers, site, inner, typeName)
	case "NumMethod":
		storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(len(pipitMethodSet(vm, typeName, inner.Kind() == reflect.Pointer)))})
		return opContinue, true
	case "NumField":
		return interceptPipitReflectNumField(registers, site, inner)
	case reflectFieldMethod:
		return interceptPipitReflectField(vm, registers, site, inner)
	case reflectFieldByNameMethod:
		return interceptPipitReflectFieldByName(vm, registers, site, inner)
	}
	return opContinue, false
}

// interceptPipitReflectFieldByName answers reflect.Value.FieldByName on a synthesised
// struct by the field's source name, so a renamed unexported embedded field is found and
// carries the embedded read-only flag Go gives it.
//
// Takes vm (*VM) which owns the registers.
// Takes registers (*Registers) which hold the name argument and receive the result.
// Takes site (*program.CallSite) which describes the call.
// Takes inner (reflect.Value) which is the receiver reflect.Value.
//
// Returns the dispatch result and true when the call was answered.
func interceptPipitReflectFieldByName(vm *VM, registers *Registers, site *program.CallSite, inner reflect.Value) (OpResult, bool) {
	if len(site.Arguments) < 2 {
		return opContinue, false
	}
	nameValue := registerToReflectValue(vm.Arena, registers, site.Arguments[1].Kind, site.Arguments[1].Register)
	if !nameValue.IsValid() || nameValue.Kind() != reflect.String {
		return opContinue, false
	}
	structValue := inner
	for structValue.Kind() == reflect.Pointer {
		if structValue.IsNil() {
			return raiseNativePanicAsInterpreted(vm, "reflect: indirection through nil pointer to embedded struct"), true
		}
		structValue = structValue.Elem()
	}
	if structValue.Kind() != reflect.Struct {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(pipitFieldByName(structValue, nameValue.String()))})
	return opContinue, true
}

// installPipitReflectFieldByNameOverride binds reflect.Value.FieldByName as a method
// value on a synthesised struct, resolving fields by their source names.
//
// Takes registers (*Registers) which receive the bound function.
// Takes destinationRegister (uint8) which is the general register to write.
// Takes inner (reflect.Value) which is the struct or a pointer to it.
//
// Returns bool which is true when the method value was installed.
func installPipitReflectFieldByNameOverride(registers *Registers, destinationRegister uint8, inner reflect.Value) bool {
	structValue := inner
	for structValue.Kind() == reflect.Pointer && !structValue.IsNil() {
		structValue = structValue.Elem()
	}
	if structValue.Kind() != reflect.Struct {
		return false
	}
	registers.General[destinationRegister] = reflect.ValueOf(func(name string) reflect.Value { return pipitFieldByName(structValue, name) })
	return true
}

// hasRenamedEmbeddedField reports whether a synthesised struct type (behind any pointers)
// carries an unexported embedded field under the rename prefix.
//
// Takes t (reflect.Type) which is the value's type.
//
// Returns bool which is true when FieldByName must consult the source names.
func hasRenamedEmbeddedField(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	for field := range t.Fields() {
		if strings.HasPrefix(field.Name, isa.EmbeddedUnexportedPrefix) {
			return true
		}
	}
	return false
}

// pipitStructFieldMatches reports whether a synthesised field answers to a source-level
// name.
//
// Takes field (reflect.StructField) which is the synthesised field.
// Takes name (string) which is the source-level field name being looked up.
//
// Returns bool which is true when field is that source-level field.
func pipitStructFieldMatches(field reflect.StructField, name string) bool {
	if strings.HasPrefix(field.Name, isa.EmbeddedUnexportedPrefix) {
		return field.Name[len(isa.EmbeddedUnexportedPrefix):] == name
	}
	return field.Name == name
}

// pipitFieldValue returns a synthesised struct's i'th field with Go's embedded read-only
// bits restored.
//
// Takes structValue (reflect.Value) which is the struct.
// Takes i (int) which is the field index.
//
// Returns reflect.Value which is the field value.
func pipitFieldValue(structValue reflect.Value, i int) reflect.Value {
	value := structValue.Field(i)
	if strings.HasPrefix(structValue.Type().Field(i).Name, isa.EmbeddedUnexportedPrefix) {
		return stampEmbeddedReadOnly(value)
	}
	return value
}

// pipitFieldByName finds a synthesised struct's field by its source name.
//
// Takes structValue (reflect.Value) which is the struct.
// Takes name (string) which is the source-level field name.
//
// Returns reflect.Value which is the zero Value when no field has that name; a renamed
// unexported embedded field is returned with Go's embedded read-only flag.
func pipitFieldByName(structValue reflect.Value, name string) reflect.Value {
	rt := structValue.Type()
	for i := range pipitUserFieldCount(rt) {
		if pipitStructFieldMatches(rt.Field(i), name) {
			return pipitFieldValue(structValue, i)
		}
	}
	return reflect.Value{}
}

// interceptPipitReflectMethodByName resolves a pipit method by name.
//
// Resolves a method name argument against the pipit method table and stores the callable,
// or the zero Value, into the site's return slot.
//
// Takes vm (*VM) which owns the method table and dispatch context.
// Takes registers (*Registers) which receives the result.
// Takes site (*CallSite) which describes the argument/return slots.
// Takes inner (reflect.Value) which is the unwrapped pipit receiver.
// Takes typeName (string) which is the source-level type name.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptPipitReflectMethodByName(vm *VM, registers *Registers, site *program.CallSite, inner reflect.Value, typeName string) (OpResult, bool) {
	if len(site.Arguments) < 2 {
		return opContinue, false
	}
	nameArgument := registerToReflectValue(vm.Arena, registers, site.Arguments[1].Kind, site.Arguments[1].Register)
	if !nameArgument.IsValid() || nameArgument.Kind() != reflect.String {
		return opContinue, false
	}
	callable, found := buildPipitBoundMethodCallable(vm, inner, typeName, nameArgument.String())
	if !found {
		storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(reflect.Value{})})
		return opContinue, true
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(callable)})
	return opContinue, true
}

// interceptPipitReflectNumField reports the user-visible field count.
//
// Matches interceptReflectTypeNumField on the Type side. Without it, code that iterates
// `for i := 0; i < v.NumField(); i++ { vt.Field(i) }` (go-spew's dump.go) walks past the
// type-side last index and panics with "Field index out of range".
//
// Takes registers (*Registers) which receives the result.
// Takes site (*CallSite) which describes the return slot.
// Takes inner (reflect.Value) which is the unwrapped pipit receiver.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptPipitReflectNumField(registers *Registers, site *program.CallSite, inner reflect.Value) (OpResult, bool) {
	rt := inner.Type()
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return opContinue, false
	}
	userFieldCount := pipitUserFieldCount(rt)
	if userFieldCount == rt.NumField() {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(userFieldCount)})
	return opContinue, true
}

// interceptPipitReflectField bounds-checks Field(i) on a pipit struct.
//
// Compares the index against the user-visible field count so the sentinel index raises
// the canonical "reflect: Field index out of range" panic instead of returning the
// sentinel zero-struct.
//
// Takes vm (*VM) which owns the dispatch arena.
// Takes registers (*Registers) which receives the result.
// Takes site (*CallSite) which describes the argument/return slots.
// Takes inner (reflect.Value) which is the unwrapped pipit receiver.
//
// Returns OpResult which indicates the next execution step.
// Returns bool which is true when the call was handled.
func interceptPipitReflectField(vm *VM, registers *Registers, site *program.CallSite, inner reflect.Value) (OpResult, bool) {
	if len(site.Arguments) < 2 {
		return opContinue, false
	}
	rt := inner.Type()
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return opContinue, false
	}
	userFieldCount := pipitUserFieldCount(rt)
	if !pipitStructFieldsNeedRewrite(rt, userFieldCount) {
		return opContinue, false
	}
	indexValue := registerToReflectValue(vm.Arena, registers, site.Arguments[1].Kind, site.Arguments[1].Register)
	if !indexValue.IsValid() || !indexValue.CanInt() {
		return opContinue, false
	}
	i := int(indexValue.Int())
	if i < 0 || i >= userFieldCount {
		return raiseNativePanicAsInterpreted(vm, "reflect: Field index out of range"), true
	}
	structValue := inner
	for structValue.Kind() == reflect.Pointer {
		if structValue.IsNil() {
			return raiseNativePanicAsInterpreted(vm, "reflect: indirection through nil pointer to embedded struct"), true
		}
		structValue = structValue.Elem()
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(pipitFieldValue(structValue, i))})
	return opContinue, true
}

// pipitReflectValueTypeName resolves the pipit source-level type name.
//
// Unwraps one pointer layer when resolving the wrapped pipit value.
//
// Takes vm (*VM) which provides the typeNames registry.
// Takes inner (reflect.Value) which is the wrapped pipit value.
//
// Returns string which is the source-level name or empty when not pipit-defined.
func pipitReflectValueTypeName(vm *VM, inner reflect.Value) string {
	rt := inner.Type()
	if name, _ := pipitReflectTypeName(vm, rt); name != "" {
		return name
	}
	if name, ok := pipitTypeName(vm, inner); ok {
		return name
	}
	return ""
}

// tryPipitReflectValueMethodGet handles the method-VALUE selector.
//
// Handles isa.SubOpGetMethod / isa.SubOpGetMethod for `reflect.Value` receivers whose
// wrapped value is a pipit type. Native MethodByName / NumMethod cannot see pipit's
// method set (it lives in the VM method table), so produces a native Go func value that
// handleCallNative invokes.
//
// For `v.MethodByName("Add")` stores a `func(string) reflect.Value` that, given the
// method name, returns a callable reflect.Value bound to the pipit method via
// boundMethodVM. The follow-up `.Call(...)` dispatches through native reflect because the
// callable is a genuine reflect.MakeFunc value.
//
// Takes vm (*VM) which owns the method table.
// Takes registers (*Registers) which receives the func value.
// Takes destinationRegister (uint8) which is the general-bank slot.
// Takes receiverValue (reflect.Value) which is the reflect.Value receiver wrapping a
// pipit value.
// Takes methodName (string) which is the reflect.Value method being selected
// (MethodByName / Method / NumMethod).
//
// Returns bool which is true when the selector was handled; false to fall through to
// native reflect.Value method dispatch.
func tryPipitReflectValueMethodGet(vm *VM, registers *Registers, destinationRegister uint8, receiverValue reflect.Value, methodName string) bool {
	if !receiverValue.CanInterface() {
		return false
	}
	inner, ok := reflect.TypeAssert[reflect.Value](receiverValue)
	if !ok || !inner.IsValid() {
		return false
	}
	if methodName == reflectFieldByNameMethod && hasRenamedEmbeddedField(inner.Type()) {
		return installPipitReflectFieldByNameOverride(registers, destinationRegister, inner)
	}

	if methodName == reflectFieldMethod && hasRenamedEmbeddedField(inner.Type()) {
		return installPipitReflectFieldOverride(registers, destinationRegister, inner)
	}
	typeName := pipitReflectValueTypeName(vm, inner)
	if typeName == "" {
		return false
	}
	return installPipitReflectMethodOverride(vm, registers, destinationRegister, inner, typeName, methodName)
}

// installPipitReflectMethodOverride binds one reflect.Value method of a named synthesised
// type as a method value.
//
// Takes vm (*VM) which owns the method tables.
// Takes registers (*Registers) which receive the bound function.
// Takes destinationRegister (uint8) which is the general register to write.
// Takes inner (reflect.Value) which is the receiver reflect.Value.
// Takes typeName (string) which is the synthesised type's name.
// Takes methodName (string) which is the reflect.Value method.
//
// Returns bool which is true when the method value was installed.
//
// Panics with reflectMethodOutOfRange when the installed Method(i) closure is called with
// an out-of-range index.
func installPipitReflectMethodOverride(vm *VM, registers *Registers, destinationRegister uint8, inner reflect.Value, typeName, methodName string) bool {
	switch methodName {
	case "MethodByName":
		registers.General[destinationRegister] = reflect.ValueOf(func(name string) reflect.Value {
			callable, found := buildPipitBoundMethodCallable(vm, inner, typeName, name)
			if !found {
				return reflect.Value{}
			}
			return callable
		})
		return true
	case "Method":
		set := pipitMethodSet(vm, typeName, inner.Kind() == reflect.Pointer)
		registers.General[destinationRegister] = reflect.ValueOf(func(index int) reflect.Value {
			if index < 0 || index >= len(set) {
				panic(reflectMethodOutOfRange)
			}
			return pipitBoundMethodValue(vm, set[index], inner)
		})
		return true
	case "NumMethod":
		count := len(pipitMethodSet(vm, typeName, inner.Kind() == reflect.Pointer))
		registers.General[destinationRegister] = reflect.ValueOf(func() int { return count })
		return true
	case "NumField":
		return installPipitReflectNumFieldOverride(registers, destinationRegister, inner)
	case reflectFieldMethod:
		return installPipitReflectFieldOverride(registers, destinationRegister, inner)
	case reflectFieldByNameMethod:
		return installPipitReflectFieldByNameOverride(registers, destinationRegister, inner)
	}
	return false
}

// installPipitReflectNumFieldOverride hides the pipit sentinel field from the
// reflect.Value side so NumField matches the Type intercept's count.
//
// Takes registers (*Registers) which receives the func value.
// Takes destinationRegister (uint8) which is the general-bank slot.
// Takes inner (reflect.Value) which is the unwrapped pipit receiver.
//
// Returns bool which is true when the override was installed.
func installPipitReflectNumFieldOverride(registers *Registers, destinationRegister uint8, inner reflect.Value) bool {
	structType, ok := pipitStructTypeForValue(inner)
	if !ok {
		return false
	}
	userFieldCount := pipitUserFieldCount(structType)
	if userFieldCount == structType.NumField() {
		return false
	}
	registers.General[destinationRegister] = reflect.ValueOf(func() int { return userFieldCount })
	return true
}

// installPipitReflectFieldOverride installs a bounds-checked Field(i).
//
// Installs a Field(i) closure that bounds-checks against the user-visible field count so
// the sentinel index raises the canonical "reflect: Field index out of range" panic
// instead of exposing the synthetic identity field. The captured structValue preserves
// addressability semantics on the original receiver - reflect.Value. Field on a
// non-pointer Struct returns a Value whose addressability matches the receiver's.
//
// Takes registers (*Registers) which receives the func value.
// Takes destinationRegister (uint8) which is the general-bank slot.
// Takes inner (reflect.Value) which is the unwrapped pipit receiver.
//
// Returns bool which is true when the override was installed.
//
// Panics when the installed Field(i) closure is invoked with an index outside the
// user-visible field range, matching the canonical reflect.Value.Field bounds-check
// panic.
func installPipitReflectFieldOverride(registers *Registers, destinationRegister uint8, inner reflect.Value) bool {
	structType, ok := pipitStructTypeForValue(inner)
	if !ok {
		return false
	}
	userFieldCount := pipitUserFieldCount(structType)
	if !pipitStructFieldsNeedRewrite(structType, userFieldCount) {
		return false
	}
	structValue := inner
	for structValue.Kind() == reflect.Pointer {
		if structValue.IsNil() {
			return false
		}
		structValue = structValue.Elem()
	}
	if structValue.Kind() != reflect.Struct {
		return false
	}
	registers.General[destinationRegister] = reflect.ValueOf(func(i int) reflect.Value {
		if i < 0 || i >= userFieldCount {
			panic("reflect: Field index out of range")
		}
		return pipitFieldValue(structValue, i)
	})
	return true
}

// safeMethodByName invokes MethodByName under a recover guard so that a panicking reflect
// call surfaces as an error instead of crashing the host.
//
// Takes receiverValue (reflect.Value) which is the method receiver.
// Takes methodName (string) which is the method name being resolved.
//
// Returns method (reflect.Value) which is the bound method, or the zero Value.
// Returns lookupErr (error) when the reflect call itself panicked.
func safeMethodByName(receiverValue reflect.Value, methodName string) (method reflect.Value, lookupErr error) {
	defer func() {
		if r := recover(); r != nil {
			method = reflect.Value{}
			lookupErr = fmt.Errorf("reflect MethodByName panic: %v", r)
		}
	}()
	if !receiverValue.IsValid() {
		return reflect.Value{}, errors.New("invalid receiver")
	}
	return receiverValue.MethodByName(methodName), nil
}

// pipitStructTypeForValue returns the underlying struct reflect.Type.
//
// Transparently dereferences one pointer layer. Used by the reflect.Value NumField/Field
// interceptors so the sentinel-stripping logic that runs on the Type side can be mirrored
// on the Value side without duplicating receiver-shape handling.
//
// Takes v (reflect.Value) which is the receiver wrapped by reflect.Value.
//
// Returns reflect.Type which is the struct type on success.
// Returns bool which is true on success; false when v is not a struct or
// pointer-to-struct value.
func pipitStructTypeForValue(v reflect.Value) (reflect.Type, bool) {
	rt := v.Type()
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil, false
	}
	return rt, true
}

// buildPipitBoundMethodCallable synthesises a pipit-bound callable.
//
// Produces a callable reflect.Value for a pipit method bound to receiver, so
// reflect.Value.MethodByName followed by .Call works on pipit types. The returned func
// has interface-typed parameters (so exact-typed reflect args are accepted without an
// assignability panic) and dispatches into the interpreter through boundMethodVM.
//
// Takes vm (*VM) which provides the function table and dispatch.
// Takes receiver (reflect.Value) which is the bound pipit value.
// Takes typeName (string) which is the source-level type name.
// Takes methodName (string) which is the method to resolve.
//
// Returns reflect.Value which is the callable on success.
// Returns bool which is true when the method exists; false and the zero Value otherwise.
func buildPipitBoundMethodCallable(vm *VM, receiver reflect.Value, typeName, methodName string) (reflect.Value, bool) {
	callee, methodRoot, ok := resolvePipitMethodCallee(vm, typeName, methodName)
	if !ok {
		return reflect.Value{}, false
	}
	inTypes, outTypes := pipitMethodInOutTypes(callee)
	functionType := reflect.FuncOf(callee.VariadicSafeInTypes(inTypes), outTypes, callee.IsVariadic)
	callable := reflect.MakeFunc(functionType, func(arguments []reflect.Value) []reflect.Value {
		bound := newCrossPackageBoundMethod(vm, methodRoot, callee)
		boundReceiver := receiverValueFor(callee, receiver)
		results := bound.invoke(boundReceiver, unwrapInterfaceArguments(arguments), identityArg)
		return shapeBoundMethodResults(results, outTypes)
	})
	return callable, true
}

// resolvePipitMethodCallee finds a pipit method's CompiledFunction.
//
// Checks the local methodTable first and falls back to GlobalStore.externalMethods so
// cross-package method values emitted by compileSelectorMethodValue resolve to the
// foreign package's body. The returned methodRoot is nil for local methods (callee lives
// in vm.functions) and non-nil for cross-package methods (callee lives in the foreign
// rootFunction's functions slice). Callers feed methodRoot to newCrossPackageBoundMethod
// so the boundMethodVM dispatches against the correct functions slice.
//
// Takes vm (*VM) which provides both lookup spaces.
// Takes typeName (string) which is the source-level receiver type name.
// Takes methodName (string) which is the method identifier without qualifier.
//
// Returns callee (*CompiledFunction) which is the resolved function.
// Returns methodRoot (*CompiledFunction) which is nil for in-package methods.
// Returns ok (bool) which is true on success.
func resolvePipitMethodCallee(vm *VM, typeName, methodName string) (callee, methodRoot *program.CompiledFunction, ok bool) {
	if vm == nil {
		return nil, nil, false
	}
	key := typeName + "." + methodName
	if vm.rootFunction != nil {
		if functionIndex, ok := vm.rootFunction.MethodTable()[key]; ok && int(functionIndex) < len(vm.functions) {
			callee := vm.functions[functionIndex]
			if callee != nil && len(callee.ParameterKinds) > 0 {
				return callee, nil, true
			}
		}
	}
	if vm.Globals == nil {
		return nil, nil, false
	}
	entry, ok := vm.Globals.lookupExternalMethod(key)
	if !ok || entry.rootFunction == nil {
		return nil, nil, false
	}
	if int(entry.methodIndex) >= len(entry.rootFunction.Functions) {
		return nil, nil, false
	}
	callee = entry.rootFunction.Functions[entry.methodIndex]
	if callee == nil || len(callee.ParameterKinds) == 0 {
		return nil, nil, false
	}
	return callee, entry.rootFunction, true
}

// unwrapInterfaceArguments unwraps interface-typed arguments.
//
// Unwraps each non-nil interface-typed argument down to its concrete value so the
// interpreter receives exact-typed values rather than interface wrappers.
//
// Takes arguments ([]reflect.Value) which holds the reflect.MakeFunc arguments.
//
// Returns []reflect.Value which is a fresh slice with interface arguments unwrapped.
func unwrapInterfaceArguments(arguments []reflect.Value) []reflect.Value {
	unwrapped := make([]reflect.Value, len(arguments))
	for i := range arguments {
		unwrapped[i] = arguments[i]
		if unwrapped[i].Kind() == reflect.Interface && !unwrapped[i].IsNil() {
			unwrapped[i] = unwrapped[i].Elem()
		}
	}
	return unwrapped
}

// shapeBoundMethodResults coerces results to the declared output types.
//
// Converts where assignable and substitutes zero values for missing or invalid results.
//
// Takes results ([]reflect.Value) which holds the interpreter results.
// Takes outTypes ([]reflect.Type) which are the declared output types.
//
// Returns []reflect.Value which is shaped to the output types.
func shapeBoundMethodResults(results []reflect.Value, outTypes []reflect.Type) []reflect.Value {
	shaped := make([]reflect.Value, len(outTypes))
	for i := range outTypes {
		if i < len(results) && results[i].IsValid() {
			shaped[i] = shapeBoundMethodResult(results[i], outTypes[i])
			continue
		}
		shaped[i] = reflect.Zero(outTypes[i])
	}
	return shaped
}

// shapeBoundMethodResult fits one compiled result to the Go type a wrapped method
// promises.
//
// A result that left the general bank as an interface value is unwrapped first: a nil
// interface becomes the zero of the promised type, and a dynamic value is passed through
// or converted. Anything that still does not fit falls back to the zero value.
//
// Takes result (reflect.Value) which is the compiled result.
// Takes outType (reflect.Type) which is the promised type.
//
// Returns reflect.Value which is assignable to outType.
func shapeBoundMethodResult(result reflect.Value, outType reflect.Type) reflect.Value {
	if result.Kind() == reflect.Interface && result.Type() != outType {
		if result.IsNil() {
			return reflect.Zero(outType)
		}
		result = result.Elem()
	}
	switch {
	case result.Type() == outType, result.Type().AssignableTo(outType):
		return result
	case result.Type().ConvertibleTo(outType):
		return result.Convert(outType)
	default:
		return reflect.Zero(outType)
	}
}
