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
	"reflect"

	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// marshalShadowLeafCap bounds how many leaves one marshal shadow walk may wrap, so a
	// pathological value cannot turn one json.Marshal into unbounded work.
	marshalShadowLeafCap = 4096

	// marshalShadowDepthCap bounds the nesting a shadow walk follows.
	marshalShadowDepthCap = 32
)

// marshalShadowWalk carries the state of one shadow walk.
type marshalShadowWalk struct {
	// vm is the virtual machine whose method tables are consulted.
	vm *VM

	// budget is the remaining leaf-wrap allowance for this walk.
	budget int
}

// shadow returns the shadow of value, or false when value needs none.
//
// Takes value (reflect.Value) which is the value to shadow.
// Takes depth (int) which is the current nesting depth.
//
// Returns the shadow and true, or false when the value is returned as it is.
func (w *marshalShadowWalk) shadow(value reflect.Value, depth int) (reflect.Value, bool) {
	if !value.IsValid() || depth > marshalShadowDepthCap || w.budget <= 0 {
		return value, false
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return value, false
		}
		return w.shadow(value.Elem(), depth)
	}
	if !w.typeNeedsShadow(value.Type()) {
		return value, false
	}
	if adapter := w.leafAdapter(value); adapter.IsValid() {
		w.budget--
		return adapter, true
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return value, false
		}
		return w.shadow(value.Elem(), depth+1)
	case reflect.Slice, reflect.Array:
		return w.shadowSequence(value, depth)
	case reflect.Map:
		return w.shadowMap(value, depth)
	case reflect.Struct:
		return w.shadowStruct(value, depth)
	default:
		return value, false
	}
}

// shadowSequence rebuilds a slice or array as []any whose elements are shadowed.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which is the current nesting depth.
//
// Returns the shadow and true, or the nil slice itself and false.
func (w *marshalShadowWalk) shadowSequence(value reflect.Value, depth int) (reflect.Value, bool) {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return value, false
	}
	shadow := reflect.MakeSlice(reflect.TypeFor[[]any](), value.Len(), value.Len())
	for i := range value.Len() {
		element, _ := w.shadow(value.Index(i), depth+1)
		setAnySlot(shadow.Index(i), element)
	}
	return shadow, true
}

// shadowMap rebuilds a map as map[K]any whose values are shadowed.
//
// Takes value (reflect.Value) which is the map.
// Takes depth (int) which is the current nesting depth.
//
// Returns the shadow and true, or the nil map itself and false.
func (w *marshalShadowWalk) shadowMap(value reflect.Value, depth int) (reflect.Value, bool) {
	if value.IsNil() {
		return value, false
	}
	shadow := reflect.MakeMapWithSize(reflect.MapOf(value.Type().Key(), reflect.TypeFor[any]()), value.Len())
	for iter := value.MapRange(); iter.Next(); {
		element, _ := w.shadow(iter.Value(), depth+1)
		slot := reflect.New(reflect.TypeFor[any]()).Elem()
		setAnySlot(slot, element)
		shadow.SetMapIndex(iter.Key(), slot)
	}
	return shadow, true
}

// shadowStruct rebuilds a struct with its exported fields, keeping names and tags and
// widening a field to `any` when its value needs shadowing.
//
// Takes value (reflect.Value) which is the struct.
// Takes depth (int) which is the current nesting depth.
//
// Returns the shadow struct and true.
func (w *marshalShadowWalk) shadowStruct(value reflect.Value, depth int) (reflect.Value, bool) {
	fields, values := w.appendShadowFields(nil, nil, value, depth)
	shadow := reflect.New(reflect.StructOf(fields)).Elem()
	for i, fieldValue := range values {
		setAnySlot(shadow.Field(i), fieldValue)
	}
	return shadow, true
}

// appendShadowFields appends one struct level's shadow fields and their values.
//
// A renamed embedded field is unexported in the synthesised struct, so encoding/json
// would drop it and everything under it. Splices its exported fields into this level
// instead, which is what Go does with an embedded field it can declare directly; an
// embedded field of unexported non-struct type, which Go ignores, is ignored here too.
//
// Takes fields ([]reflect.StructField) which is the shadow's fields so far.
// Takes values ([]reflect.Value) which is the matching value for each of those fields.
// Takes value (reflect.Value) which is the struct level to append.
// Takes depth (int) which is the current nesting depth.
//
// Returns the extended field and value slices.
func (w *marshalShadowWalk) appendShadowFields(fields []reflect.StructField, values []reflect.Value, value reflect.Value, depth int) ([]reflect.StructField, []reflect.Value) {
	structType := value.Type()
	for i := range structType.NumField() {
		field := structType.Field(i)
		if isRenamedEmbeddedField(field) {
			embedded := value.Field(i)
			for embedded.Kind() == reflect.Pointer && !embedded.IsNil() {
				embedded = embedded.Elem()
			}
			if embedded.Kind() != reflect.Struct {
				continue
			}

			fields, values = w.appendShadowFields(fields, values, launderReadOnlyFieldValue(embedded), depth)
			continue
		}
		if !field.IsExported() {
			continue
		}
		fieldValue := value.Field(i)
		shadowed, changed := w.shadow(fieldValue, depth+1)
		fieldType := field.Type
		if changed {
			fieldType = reflect.TypeFor[any]()
		}
		fields = append(fields, reflect.StructField{Name: field.Name, Type: fieldType, Tag: field.Tag, Anonymous: field.Anonymous && !changed})
		values = append(values, shadowed)
	}
	return fields, values
}

// leafAdapter returns the marshaler adapter for a script value whose type declares
// MarshalJSON, or an invalid value.
//
// Takes value (reflect.Value) which is the candidate leaf.
//
// Returns reflect.Value which is the adapter or invalid.
func (w *marshalShadowWalk) leafAdapter(value reflect.Value) reflect.Value {
	typeName := marshalLeafTypeName(value.Type())
	if typeName == "" {
		return reflect.Value{}
	}
	return buildMarshalerAdapterIfRegistered(w.vm, value, typeName)
}

// typeNeedsShadow reports whether values of t can contain a script marshaler leaf,
// memoised on the program root so the memo dies with the program.
//
// Takes t (reflect.Type) which is the type to inspect.
//
// Returns true when a walk over a value of t may find something to wrap.
func (w *marshalShadowWalk) typeNeedsShadow(t reflect.Type) bool {
	root := w.vm.rootFunction
	if root == nil {
		return w.typeNeedsShadowWalk(t, map[reflect.Type]struct{}{})
	}
	if cached, ok := root.MarshalShadowMemo.Load(t); ok {
		if needs, isBool := cached.(bool); isBool {
			return needs
		}
	}
	needs := w.typeNeedsShadowWalk(t, map[reflect.Type]struct{}{})
	root.MarshalShadowMemo.Store(t, needs)
	return needs
}

// typeNeedsShadowWalk is the uncached body of typeNeedsShadow.
//
// Takes t (reflect.Type) which is the type to inspect.
// Takes inProgress (map[reflect.Type]struct{}) which breaks type cycles.
//
// Returns true when t or a type nested in it is a script marshaler type.
func (w *marshalShadowWalk) typeNeedsShadowWalk(t reflect.Type, inProgress map[reflect.Type]struct{}) bool {
	if t == nil {
		return false
	}
	if _, cycling := inProgress[t]; cycling {
		return false
	}
	inProgress[t] = struct{}{}
	defer delete(inProgress, t)
	if typeName := marshalLeafTypeName(t); typeName != "" {
		if _, _, ok := lookupAdapterMethod(w.vm, typeName+".MarshalJSON"); ok {
			return true
		}
	}
	switch t.Kind() {
	case reflect.Interface:
		return t.NumMethod() == 0
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return w.typeNeedsShadowWalk(t.Elem(), inProgress)
	case reflect.Struct:
		for field := range t.Fields() {
			if isRenamedEmbeddedField(field) {
				return true
			}
			if field.IsExported() && w.typeNeedsShadowWalk(field.Type, inProgress) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// shadowForMarshal rebuilds a container so every nested script value that declares
// MarshalJSON reaches the encoder as a marshaler adapter.
//
// Covers elements, fields and map values under a top-level value that the encoder would
// otherwise see as bare structs and numbers. Values without such leaves are returned
// unchanged.
//
// Takes vm (*VM) which owns the method tables.
// Takes value (reflect.Value) which is the coerced argument.
//
// Returns reflect.Value which is the shadow, or value itself when no leaf needs it.
func shadowForMarshal(vm *VM, value reflect.Value) reflect.Value {
	if vm == nil || vm.rootFunction == nil || !value.IsValid() {
		return value
	}
	walk := marshalShadowWalk{vm: vm, budget: marshalShadowLeafCap}
	if shadow, ok := walk.shadow(value, 0); ok {
		return shadow
	}
	return value
}

// marshalLeafTypeName returns the bare script type name of a pool-backed named basic type
// or a synthesised struct, or "" for anything else.
//
// Takes t (reflect.Type) which is the candidate type.
//
// Returns string which is the bare name or "".
func marshalLeafTypeName(t reflect.Type) string {
	if info, ok := typemodel.LookupNamedScalarPoolInfo(t); ok {
		return info.BareName
	}
	if isPipitSynthesisedReflectType(t) {
		return bareSentinelName(t)
	}
	return ""
}

// setAnySlot stores value into an `any`-typed or same-typed slot, leaving the slot's zero
// value for an invalid value.
//
// Takes slot (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the value to store.
func setAnySlot(slot, value reflect.Value) {
	if !value.IsValid() {
		return
	}
	if value.Type().AssignableTo(slot.Type()) {
		slot.Set(value)
		return
	}
	if value.CanInterface() {
		slot.Set(reflect.ValueOf(value.Interface()))
	}
}

// isEmptyInterfaceType reports whether t is an interface with no methods.
//
// Takes t (reflect.Type) which is the type to inspect.
//
// Returns true for `any`.
func isEmptyInterfaceType(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Interface && t.NumMethod() == 0
}
