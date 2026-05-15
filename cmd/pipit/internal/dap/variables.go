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

package dap

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/google/go-dap"

	"pipit.sh/pipit/internal/debug"
)

// mapEntry pairs a map key's display name with the value it addresses, so entries can be
// sorted by name before they are turned into DAP variables.
type mapEntry struct {
	// name is the stringified map key shown to the IDE.
	name string

	// value is the reflect.Value the key addresses.
	value reflect.Value
}

// handleVariables expands the container referenced by VariablesReference and returns its
// child variables.
//
// Takes request (*dap.VariablesRequest) which names the container to expand.
func (s *server) handleVariables(request *dap.VariablesRequest) {
	stop := s.currentStop()
	if stop == nil {
		s.writeMessage(&dap.VariablesResponse{
			Response: s.newResponse(request.Request, true),
			Body:     dap.VariablesResponseBody{Variables: nil},
		})
		return
	}
	container, ok := stop.lookupContainer(request.Arguments.VariablesReference)
	if !ok {
		s.writeError(&request.Request, "pipit dap: unknown variablesReference")
		return
	}

	var variables []dap.Variable
	switch container.kind {
	case containerKindScope:
		var err error
		variables, err = s.expandScope(stop, container)
		if err != nil {
			s.writeError(&request.Request, "pipit dap: "+err.Error())
			return
		}
	case containerKindReflectValue:
		variables = s.expandReflectValue(stop, container.value)
	default:
		s.writeError(&request.Request, "pipit dap: empty variablesReference")
		return
	}
	variables = pageVariables(variables, request.Arguments.Start, request.Arguments.Count)

	s.writeMessage(&dap.VariablesResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.VariablesResponseBody{
			Variables: variables,
		},
	})
}

// expandScope converts one variable scope of a frame into DAP Variables.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes container (variableContainer) which names the frame and scope.
//
// Returns []Variable which lists the scope's variables.
// Returns error which is set when the debugger query fails.
func (s *server) expandScope(stop *stopState, container variableContainer) ([]dap.Variable, error) {
	scope := container.scope
	if scope == 0 {
		scope = debug.ScopeLocals
	}
	infos, err := s.debugger.Variables(container.frame.threadID, container.frame.frameIndex, scope)
	if err != nil {
		return nil, err
	}
	out := make([]dap.Variable, 0, len(infos))
	for _, info := range infos {
		typeName := info.Type
		if typeName == "" {
			typeName = info.Kind
		}
		variable := s.makeVariable(stop, info.Name, typeName, reflect.ValueOf(info.Value))
		variable.EvaluateName = info.Name
		out = append(out, variable)
	}
	return out, nil
}

// pageVariables applies DAP start/count paging to a variable list.
//
// Takes variables ([]dap.Variable) which is the full list.
// Takes start (int) which is the zero-based offset into the list.
// Takes count (int) which selects the window size; a zero count means to the end.
//
// Returns []dap.Variable which is the window.
func pageVariables(variables []dap.Variable, start, count int) []dap.Variable {
	start = max(start, 0)
	start = min(start, len(variables))
	end := len(variables)
	if count > 0 {
		end = min(end, start+count)
	}
	return variables[start:end]
}

// expandReflectValue converts one composite value into its child variables.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes value (reflect.Value) which holds the composite to expand.
//
// Returns []Variable which lists the composite's child variables.
func (s *server) expandReflectValue(stop *stopState, value reflect.Value) []dap.Variable {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface && !value.IsNil() {
		value = value.Elem()
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		return s.expandStruct(stop, value)
	case reflect.Map:
		return s.expandMap(stop, value)
	case reflect.Slice, reflect.Array:
		return s.expandIndexed(stop, value)
	default:

		return nil
	}
}

// expandStruct returns one Variable per struct field.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes value (reflect.Value) which holds the struct to expand.
//
// Returns []Variable which lists one entry per struct field.
func (s *server) expandStruct(stop *stopState, value reflect.Value) []dap.Variable {
	typ := value.Type()
	out := make([]dap.Variable, 0, value.NumField())
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		name := typ.Field(index).Name
		out = append(out, s.makeVariable(stop, name, fieldTypeName(typ.Field(index).Type), field))
	}
	return out
}

// expandMap returns one Variable per map entry, sorted by the stringified key for stable
// IDE ordering.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes value (reflect.Value) which holds the map to expand.
//
// Returns []Variable which lists the map entries in key order.
func (s *server) expandMap(stop *stopState, value reflect.Value) []dap.Variable {
	keys := value.MapKeys()
	entries := make([]mapEntry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, mapEntry{
			name:  fmt.Sprintf("%v", key.Interface()),
			value: value.MapIndex(key),
		})
	}
	slices.SortFunc(entries, func(a, b mapEntry) int {
		return cmp.Compare(a.name, b.name)
	})

	elemType := fieldTypeName(value.Type().Elem())
	out := make([]dap.Variable, 0, len(entries))
	for _, entry := range entries {
		out = append(out, s.makeVariable(stop, entry.name, elemType, entry.value))
	}
	return out
}

// expandIndexed returns one Variable per element of a slice or array, with names "[0]",
// "[1]", and so on.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes value (reflect.Value) which holds the slice or array.
//
// Returns []Variable which lists one entry per element.
func (s *server) expandIndexed(stop *stopState, value reflect.Value) []dap.Variable {
	count := value.Len()
	elemType := fieldTypeName(value.Type().Elem())
	out := make([]dap.Variable, 0, count)
	for index := range count {
		out = append(out, s.makeVariable(stop, "["+strconv.Itoa(index)+"]", elemType, value.Index(index)))
	}
	return out
}

// makeVariable builds a single DAP Variable from a name and a reflect.Value.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes name (string) which labels the variable in the IDE.
// Takes typeHint (string) which overrides the displayed type name.
// Takes value (reflect.Value) which holds the variable's value.
//
// Returns Variable which describes the value for the IDE.
func (*server) makeVariable(stop *stopState, name, typeHint string, value reflect.Value) dap.Variable {
	displayType := typeHint
	if displayType == "" && value.IsValid() {
		displayType = fieldTypeName(value.Type())
	}

	variable := dap.Variable{
		Name:  name,
		Value: formatLeaf(value),
		Type:  displayType,
	}

	if isExpandable(value) {
		walked := value
		for walked.Kind() == reflect.Interface && !walked.IsNil() {
			walked = walked.Elem()
		}
		for walked.Kind() == reflect.Pointer && !walked.IsNil() {
			walked = walked.Elem()
		}
		variable.VariablesReference = stop.allocateContainer(variableContainer{
			kind:  containerKindReflectValue,
			value: walked,
			frame: frameRef{threadID: 0, frameIndex: 0},
			scope: 0})
		switch walked.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map:
			variable.IndexedVariables = walked.Len()
		case reflect.Struct:
			variable.NamedVariables = walked.NumField()
		default:
		}
	}
	return variable
}

// isExpandable reports whether value has at least one child the IDE could drill into.
//
// Takes value (reflect.Value) which is inspected for children.
//
// Returns bool which is true when the value has expandable children.
func isExpandable(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	current := value
	for current.Kind() == reflect.Interface {
		if current.IsNil() {
			return false
		}
		current = current.Elem()
	}
	for current.Kind() == reflect.Pointer {
		if current.IsNil() {
			return false
		}
		current = current.Elem()
	}
	switch current.Kind() {
	case reflect.Struct:
		return current.NumField() > 0
	case reflect.Map, reflect.Slice, reflect.Array:
		return current.Len() > 0
	default:
		return false
	}
}

// formatLeaf returns the IDE-visible value string for a reflect.Value.
//
// Takes value (reflect.Value) which holds the value to render.
//
// Returns string which is the display text for the value.
func formatLeaf(value reflect.Value) string {
	if !value.IsValid() {
		return "<invalid>"
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return "nil"
		}
		return formatLeaf(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return "nil"
		}
		return "&" + formatLeaf(value.Elem())
	case reflect.Struct:
		return fmt.Sprintf("%s{...}", fieldTypeName(value.Type()))
	case reflect.Map:
		return fmt.Sprintf("%s (%d entries)", fieldTypeName(value.Type()), value.Len())
	case reflect.Slice:
		if value.IsNil() {
			return "nil"
		}
		return fmt.Sprintf("%s (len=%d)", fieldTypeName(value.Type()), value.Len())
	case reflect.Array:
		return fmt.Sprintf("%s (len=%d)", fieldTypeName(value.Type()), value.Len())
	case reflect.Chan, reflect.Func:
		return fieldTypeName(value.Type())
	case reflect.String:
		return strconv.Quote(value.String())
	default:
		return fmt.Sprintf("%v", value.Interface())
	}
}

// fieldTypeName returns a short, human-friendly type name.
//
// Takes typ (reflect.Type) which is the type to name.
//
// Returns string which is the display name for the type.
func fieldTypeName(typ reflect.Type) string {
	if typ == nil {
		return ""
	}
	if typ.Name() != "" {
		return typ.Name()
	}
	return typ.String()
}

// reflectZero is the invalid reflect.Value scope containers carry.
var reflectZero = reflect.Value{}
