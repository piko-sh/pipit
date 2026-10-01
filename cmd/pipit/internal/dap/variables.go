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
	"reflect"

	"github.com/google/go-dap"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/debug/debugview"
)

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
		variables = pageVariables(variables, request.Arguments.Start, request.Arguments.Count)
	case containerKindReflectValue:
		variables = s.expandReflectValue(stop, container.value, request.Arguments.Start, request.Arguments.Count)
	default:
		s.writeError(&request.Request, "pipit dap: empty variablesReference")
		return
	}

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

// expandReflectValue converts a window of one composite value's children into variables,
// rendering only the children inside it.
//
// Takes stop (*stopState) which owns the container reference table.
// Takes value (reflect.Value) which holds the composite to expand.
// Takes start (int) which is the zero-based index of the first child.
// Takes count (int) which is the most children to return; zero means to the end.
//
// Returns []Variable which lists the window's child variables.
func (s *server) expandReflectValue(stop *stopState, value reflect.Value, start, count int) []dap.Variable {
	children := debugview.ChildrenRange(value, start, count)
	if children == nil {
		return nil
	}
	out := make([]dap.Variable, 0, len(children))
	for _, child := range children {
		out = append(out, s.makeVariable(stop, child.Name, child.Type, child.Value))
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
		displayType = debugview.TypeName(value.Type())
	}

	variable := dap.Variable{
		Name:  name,
		Value: debugview.Leaf(value),
		Type:  displayType,
	}

	if debugview.Expandable(value) {
		walked := debugview.Deref(value)
		variable.VariablesReference = stop.allocateContainer(variableContainer{
			kind:  containerKindReflectValue,
			value: walked,
			frame: frameRef{threadID: 0, frameIndex: 0},
			scope: 0})
		switch walked.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map:
			variable.IndexedVariables = walked.Len()
		case reflect.Struct:
			variable.NamedVariables = debugview.ChildCount(walked)
		default:
		}
	}
	return variable
}

// reflectZero is the invalid reflect.Value scope containers carry.
var reflectZero = reflect.Value{}
