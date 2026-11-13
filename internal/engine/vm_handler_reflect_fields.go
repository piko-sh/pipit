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
	"iter"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
)

const (
	// reflectFieldsMethod is the reflect.Type and reflect.Value method iterating over a
	// struct's fields.
	reflectFieldsMethod = "Fields"

	// reflectFieldByIndexMethod is the reflect.Type and reflect.Value method following an
	// index path.
	reflectFieldByIndexMethod = "FieldByIndex"
)

const (
	// reflectFieldIndexOutOfRange is the panic reflect.Value raises for a field index past
	// the last field.
	reflectFieldIndexOutOfRange = "reflect: Field index out of range"

	// reflectTypeFieldIndexOutOfBounds is the panic reflect.Type raises for a field index
	// past the last field.
	reflectTypeFieldIndexOutOfBounds = "reflect: Field index out of bounds"
)

// reflectVisibleFieldsPointer identifies reflect.VisibleFields among native callees.
var reflectVisibleFieldsPointer = reflect.ValueOf(reflect.VisibleFields).Pointer()

// visibleFieldsWalker accumulates the fields reflect.VisibleFields reports for a script
// struct, seen through restoredStructFields.
type visibleFieldsWalker struct {
	// byName maps a field name to its position in fields.
	byName map[string]int

	// visiting holds the struct types on the current path, so a recursive embedding stops.
	visiting map[reflect.Type]bool

	// fields are the fields found so far. A hidden field has its name cleared.
	fields []reflect.StructField

	// index is the index path of the field being visited.
	index []int
}

// walk visits the declared fields of structType and, depth first, those of each embedded
// struct, applying Go's rule that a shallower field hides a deeper one of the same name
// and two at the same depth hide each other.
//
// Takes structType (reflect.Type) which is the struct visited.
func (w *visibleFieldsWalker) walk(structType reflect.Type) {
	if w.visiting[structType] {
		return
	}
	w.visiting[structType] = true
	defer delete(w.visiting, structType)
	for position, field := range restoredStructFields(structType) {
		w.index = append(w.index, position)
		w.add(field)
		if field.Anonymous {
			embedded := field.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				w.walk(embedded)
			}
		}
		w.index = w.index[:len(w.index)-1]
	}
}

// add records field at the current index path unless a field of the same name hides it,
// and clears any field it hides.
//
// Takes field (reflect.StructField) which is the field visited.
func (w *visibleFieldsWalker) add(field reflect.StructField) {
	if previous, seen := w.byName[field.Name]; seen {
		old := &w.fields[previous]
		switch {
		case len(w.index) == len(old.Index):
			old.Name = ""
			return
		case len(w.index) < len(old.Index):
			old.Name = ""
		default:
			return
		}
	}
	field.Index = append([]int(nil), w.index...)
	w.byName[field.Name] = len(w.fields)
	w.fields = append(w.fields, field)
}

// restoredStructFields returns an iterator over the fields of a struct type as the
// program declared them. The identity sentinel a script struct's runtime type ends with
// is left out, and a renamed embedded field appears under its own name, marked embedded.
//
// Takes structType (reflect.Type) which is a struct type.
//
// Returns iter.Seq2[int, reflect.StructField] which yields each field with its index.
func restoredStructFields(structType reflect.Type) iter.Seq2[int, reflect.StructField] {
	return func(yield func(int, reflect.StructField) bool) {
		for index := range pipitUserFieldCount(structType) {
			if !yield(index, restorePipitStructField(structType.Field(index))) {
				return
			}
		}
	}
}

// pipitStructTypeFields is reflect.Type.Fields for a script struct type.
//
// Takes structType (reflect.Type) which is the struct type.
//
// Returns iter.Seq[reflect.StructField] which yields each declared field.
func pipitStructTypeFields(structType reflect.Type) iter.Seq[reflect.StructField] {
	return func(yield func(reflect.StructField) bool) {
		for _, field := range restoredStructFields(structType) {
			if !yield(field) {
				return
			}
		}
	}
}

// pipitStructValueFields is reflect.Value.Fields for a value of a script struct type.
//
// Takes structValue (reflect.Value) which is a struct value.
//
// Returns iter.Seq2[reflect.StructField, reflect.Value] which yields each declared field
// with its value.
func pipitStructValueFields(structValue reflect.Value) iter.Seq2[reflect.StructField, reflect.Value] {
	return func(yield func(reflect.StructField, reflect.Value) bool) {
		for index, field := range restoredStructFields(structValue.Type()) {
			if !yield(field, pipitFieldValue(structValue, index)) {
				return
			}
		}
	}
}

// pipitStructFieldByIndex is reflect.Type.FieldByIndex for a script struct type. Each
// step is checked against the declared fields, so an index naming a sentinel panics as an
// index past the last field does.
//
// Takes structType (reflect.Type) which is the struct type.
// Takes index ([]int) which is the index path.
//
// Returns reflect.StructField which is the nested field, its Index the full path.
//
// Panics as reflect does for an index out of range or a step through a non-struct.
func pipitStructFieldByIndex(structType reflect.Type, index []int) reflect.StructField {
	var field reflect.StructField
	current := structType
	for step, position := range index {
		if step > 0 {
			current = field.Type
			if current.Kind() == reflect.Pointer && current.Elem().Kind() == reflect.Struct {
				current = current.Elem()
			}
		}
		if current.Kind() != reflect.Struct {
			panic("reflect: FieldByIndex of non-struct type " + current.String())
		}
		if position < 0 || position >= pipitUserFieldCount(current) {
			panic(reflectTypeFieldIndexOutOfBounds)
		}
		field = restorePipitStructField(current.Field(position))
	}
	field.Index = append([]int(nil), index...)
	return field
}

// pipitStructValueFieldByIndex is reflect.Value.FieldByIndex for a value of a script
// struct type, checking each step against the declared fields.
//
// Takes structValue (reflect.Value) which is the struct value.
// Takes index ([]int) which is the index path.
//
// Returns reflect.Value which is the nested field's value.
//
// Panics as reflect does for an index out of range, a step through a non-struct or a nil
// embedded pointer.
func pipitStructValueFieldByIndex(structValue reflect.Value, index []int) reflect.Value {
	current := structValue
	for step, position := range index {
		if step > 0 && current.Kind() == reflect.Pointer && current.Type().Elem().Kind() == reflect.Struct {
			if current.IsNil() {
				panic("reflect: indirection through nil pointer to embedded struct")
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			panic("reflect: call of reflect.Value.FieldByIndex on " + current.Kind().String() + " Value")
		}
		if position < 0 || position >= pipitUserFieldCount(current.Type()) {
			panic(reflectFieldIndexOutOfRange)
		}
		current = pipitFieldValue(current, position)
	}
	return current
}

// pipitVisibleFields is reflect.VisibleFields over the fields a script struct declares.
// It yields every field, including those promoted through embedded fields, that a
// selector can reach, with the identity sentinels left out and renamed embedded fields
// followed under their own names.
//
// Takes structType (reflect.Type) which is the struct type.
//
// Returns []reflect.StructField which lists the visible fields in reflect's order.
func pipitVisibleFields(structType reflect.Type) []reflect.StructField {
	walker := &visibleFieldsWalker{
		byName:   make(map[string]int),
		visiting: make(map[reflect.Type]bool),
		fields:   make([]reflect.StructField, 0, structType.NumField()),
		index:    make([]int, 0, 2),
	}
	walker.walk(structType)
	kept := walker.fields[:0]
	for _, field := range walker.fields {
		if field.Name != "" {
			kept = append(kept, field)
		}
	}
	return kept
}

// structNeedsFieldRewrite reports whether reflect's own field routes would show a script
// struct type's sentinel or renamed embedded fields, anywhere among its embedded structs.
//
// Takes structType (reflect.Type) which is the struct type.
//
// Returns bool which is true when the program's view differs from reflect's.
func structNeedsFieldRewrite(structType reflect.Type) bool {
	return structNeedsFieldRewriteAt(structType, make(map[reflect.Type]bool))
}

// structNeedsFieldRewriteAt is structNeedsFieldRewrite with the struct types already on
// the path.
//
// Takes structType (reflect.Type) which is the struct type.
// Takes visiting (map[reflect.Type]bool) which holds the types on the path.
//
// Returns bool which is true when a rewrite is needed.
func structNeedsFieldRewriteAt(structType reflect.Type, visiting map[reflect.Type]bool) bool {
	if structType.Kind() != reflect.Struct || visiting[structType] {
		return false
	}
	visiting[structType] = true
	if pipitStructFieldsNeedRewrite(structType, pipitUserFieldCount(structType)) {
		return true
	}
	for field := range structType.Fields() {
		embedded := field.Type
		if embedded.Kind() == reflect.Pointer {
			embedded = embedded.Elem()
		}
		if (field.Anonymous || strings.HasPrefix(field.Name, pipitIDFieldPrefix)) && structNeedsFieldRewriteAt(embedded, visiting) {
			return true
		}
	}
	return false
}

// dispatchReflectVisibleFieldsIntrinsic answers reflect.VisibleFields for a script struct
// type, whose runtime type reflect would report with its sentinels.
//
// Takes registers (*Registers) which receive the result.
// Takes site (*program.CallSite) which describes the return slot.
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the prepared arguments.
//
// Returns the dispatch result and true when the call was answered.
func dispatchReflectVisibleFieldsIntrinsic(registers *Registers, site *program.CallSite, reflectedFunction reflect.Value, arguments []reflect.Value) (OpResult, bool) {
	if reflectedFunction.Pointer() != reflectVisibleFieldsPointer || len(arguments) != 1 {
		return opContinue, false
	}
	structType, ok := reflect.TypeAssert[reflect.Type](arguments[0])
	if !ok || structType == nil {
		return opContinue, false
	}
	if named, isNamed := structType.(pipitNamedType); isNamed {
		structType = named.Type
	}
	if structType.Kind() != reflect.Struct || !structNeedsFieldRewrite(structType) {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(pipitVisibleFields(structType))})
	return opContinue, true
}

// interceptReflectTypeFieldRoutes answers Fields and FieldByIndex on a script struct
// type.
//
// Takes vm (*VM) which owns the registers.
// Takes registers (*Registers) which hold the arguments and receive the result.
// Takes site (*program.CallSite) which describes the call.
// Takes structType (reflect.Type) which is the receiver.
// Takes methodName (string) which is Fields or FieldByIndex.
//
// Returns the dispatch result and true when the call was answered.
func interceptReflectTypeFieldRoutes(vm *VM, registers *Registers, site *program.CallSite, structType reflect.Type, methodName string) (OpResult, bool) {
	if structType.Kind() != reflect.Struct || !structNeedsFieldRewrite(structType) {
		return opContinue, false
	}
	if methodName == reflectFieldsMethod {
		storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(pipitStructTypeFields(structType))})
		return opContinue, true
	}
	index, ok, err := fieldIndexArgument(vm, registers, site)
	if err != nil {
		vm.evalError = err
		return opPanicError, true
	}
	if !ok {
		return opContinue, false
	}
	field, panicValue := callFieldRoute(func() reflect.StructField { return pipitStructFieldByIndex(structType, index) })
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue), true
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(field)})
	return opContinue, true
}

// interceptReflectValueFieldRoutes answers Fields and FieldByIndex on a value of a script
// struct type.
//
// Takes vm (*VM) which owns the registers.
// Takes registers (*Registers) which hold the arguments and receive the result.
// Takes site (*program.CallSite) which describes the call.
// Takes inner (reflect.Value) which is the receiver reflect.Value.
// Takes methodName (string) which is Fields or FieldByIndex.
//
// Returns the dispatch result and true when the call was answered.
func interceptReflectValueFieldRoutes(vm *VM, registers *Registers, site *program.CallSite, inner reflect.Value, methodName string) (OpResult, bool) {
	if inner.Kind() != reflect.Struct || !structNeedsFieldRewrite(inner.Type()) {
		return opContinue, false
	}
	if methodName == reflectFieldsMethod {
		storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(pipitStructValueFields(inner))})
		return opContinue, true
	}
	index, ok, err := fieldIndexArgument(vm, registers, site)
	if err != nil {
		vm.evalError = err
		return opPanicError, true
	}
	if !ok {
		return opContinue, false
	}
	value, panicValue := callFieldRoute(func() reflect.Value { return pipitStructValueFieldByIndex(inner, index) })
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue), true
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(value)})
	return opContinue, true
}

// installReflectTypeFieldRouteOverride binds Fields or FieldByIndex of a script struct
// type as a method value that answers from the declared fields.
//
// Takes registers (*Registers) which receive the bound function.
// Takes destinationRegister (uint8) which is the general register to write.
// Takes receiver (reflect.Value) which is the receiver. Anything but a reflect.Type of a
// struct that needs the rewrite is left to native reflect.
// Takes methodName (string) which is the method selected.
//
// Returns bool which is true when the method value was installed.
func installReflectTypeFieldRouteOverride(registers *Registers, destinationRegister uint8, receiver reflect.Value, methodName string) bool {
	if methodName != reflectFieldsMethod && methodName != reflectFieldByIndexMethod {
		return false
	}
	if !receiver.Type().Implements(reflectTypeReflectType) || !receiver.CanInterface() {
		return false
	}
	structType, ok := reflect.TypeAssert[reflect.Type](receiver)
	if !ok || structType == nil || structType.Kind() != reflect.Struct || !structNeedsFieldRewrite(structType) {
		return false
	}
	if methodName == reflectFieldsMethod {
		registers.General[destinationRegister] = reflect.ValueOf(func() iter.Seq[reflect.StructField] { return pipitStructTypeFields(structType) })
		return true
	}
	registers.General[destinationRegister] = reflect.ValueOf(func(index []int) reflect.StructField { return pipitStructFieldByIndex(structType, index) })
	return true
}

// installReflectValueFieldRouteOverride binds Fields or FieldByIndex of a value of a
// script struct type as a method value that answers from the declared fields.
//
// Takes registers (*Registers) which receive the bound function.
// Takes destinationRegister (uint8) which is the general register to write.
// Takes inner (reflect.Value) which is the receiver reflect.Value.
// Takes methodName (string) which is the method selected.
//
// Returns bool which is true when the method value was installed.
func installReflectValueFieldRouteOverride(registers *Registers, destinationRegister uint8, inner reflect.Value, methodName string) bool {
	if methodName != reflectFieldsMethod && methodName != reflectFieldByIndexMethod {
		return false
	}
	if inner.Kind() != reflect.Struct || !structNeedsFieldRewrite(inner.Type()) {
		return false
	}
	if methodName == reflectFieldsMethod {
		registers.General[destinationRegister] = reflect.ValueOf(func() iter.Seq2[reflect.StructField, reflect.Value] { return pipitStructValueFields(inner) })
		return true
	}
	registers.General[destinationRegister] = reflect.ValueOf(func(index []int) reflect.Value { return pipitStructValueFieldByIndex(inner, index) })
	return true
}

// fieldIndexArgument reads a FieldByIndex call's []int argument.
//
// Takes vm (*VM) which owns the arena.
// Takes registers (*Registers) which hold the argument.
// Takes site (*program.CallSite) which describes the call. The argument follows the
// receiver.
//
// Returns []int which is the index path.
// Returns bool which is false when the argument is not one.
// Returns error which is the cost or cancellation error that stopped the copy.
func fieldIndexArgument(vm *VM, registers *Registers, site *program.CallSite) ([]int, bool, error) {
	if len(site.Arguments) < 2 {
		return nil, false, nil
	}
	argument := registerToReflectValue(vm.Arena, registers, site.Arguments[1].Kind, site.Arguments[1].Register)
	if argument.IsValid() && argument.Kind() == reflect.Interface {
		argument = argument.Elem()
	}
	if !argument.IsValid() || argument.Kind() != reflect.Slice || argument.Type().Elem().Kind() != reflect.Int {
		return nil, false, nil
	}
	if index, ok := reflect.TypeAssert[[]int](argument); ok {
		return index, true, nil
	}
	if err := vm.chargeHostWalk(int64(argument.Len())); err != nil {
		return nil, false, err
	}
	index := make([]int, argument.Len())
	for position := range index {
		index[position] = int(argument.Index(position).Int())
	}
	return index, true, nil
}

// callFieldRoute runs a field route, returning its panic for the program to raise.
//
// Takes route (func() T) which is the route.
//
// Returns the route's result, or the recovered panic value.
func callFieldRoute[T any](route func() T) (result T, panicValue any) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicValue = recovered
		}
	}()
	return route(), nil
}
