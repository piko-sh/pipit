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
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestTypedNativeRowsStoreWhatTheReflectiveCallStores(t *testing.T) {
	t.Parallel()

	variable := types.NewVar(token.NoPos, nil, "v", types.Typ[types.Int])
	tuple := types.NewTuple(variable)
	signature := types.NewSignatureType(nil, nil, nil, tuple, nil, false)
	named := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("p", "p"), "N", nil), types.Typ[types.String], nil)
	identifier := ast.NewIdent("x")
	inGeneral := []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}}

	tests := []struct {
		name      string
		function  any
		arguments []reflect.Value
		argument  []program.VarLocation
		result    isa.RegisterKind
	}{
		{name: "a type's underlying type", function: types.Type.Underlying, arguments: []reflect.Value{reflect.ValueOf(types.Type(named))}, argument: inGeneral, result: isa.RegisterGeneral},
		{name: "a nil type", function: func(given types.Type) types.Type { return given }, arguments: []reflect.Value{reflect.Zero(reflect.TypeFor[types.Type]())}, argument: inGeneral, result: isa.RegisterGeneral},
		{name: "a type without arguments", function: variable.Type, result: isa.RegisterGeneral},
		{name: "a nil type without arguments", function: func() types.Type { return nil }, result: isa.RegisterGeneral},
		{name: "a predicate over a type", function: types.IsInterface, arguments: []reflect.Value{reflect.ValueOf(types.Type(named))}, argument: inGeneral, result: isa.RegisterBool},
		{name: "a basic kind", function: types.Typ[types.Int].Kind, result: isa.RegisterInt64},
		{name: "a tuple", function: signature.Results, result: isa.RegisterGeneral},
		{name: "a nil tuple", function: signature.Params, result: isa.RegisterGeneral},
		{name: "a type name", function: named.Obj, result: isa.RegisterGeneral},
		{name: "a package", function: named.Obj().Pkg, result: isa.RegisterGeneral},
		{name: "a variable", function: func() *types.Var { return variable }, result: isa.RegisterGeneral},
		{name: "a variable by index", function: tuple.At, arguments: []reflect.Value{reflect.ValueOf(0)}, argument: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}}, result: isa.RegisterGeneral},
		{name: "an expression unwrapped", function: ast.Unparen, arguments: []reflect.Value{reflect.ValueOf(ast.Expr(&ast.ParenExpr{X: identifier}))}, argument: inGeneral, result: isa.RegisterGeneral},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			site := &program.CallSite{Arguments: test.argument, Returns: []program.VarLocation{{Register: 1, Kind: test.result}}}
			load := func(registers *Registers) {
				for index, value := range test.arguments {
					if test.argument[index].Kind == isa.RegisterInt64 {
						registers.Ints[0] = value.Int()
						continue
					}
					registers.General[0] = value
				}
			}

			reflective := makeRegisters()
			load(&reflective)
			storeReflectResults(&reflective, site.Returns, reflect.ValueOf(test.function).Call(test.arguments))

			typed := makeRegisters()
			load(&typed)
			taken, tag, panicValue := tryNativeFastPath(nil, site, test.function, &typed)
			require.True(t, taken, "the signature has a typed row")
			require.NotEqual(t, fastPathTagNone, tag)
			require.Nil(t, panicValue)

			require.Equal(t, reflective.Ints[1], typed.Ints[1])
			require.Equal(t, reflective.Bools[1], typed.Bools[1])
			want, got := reflective.General[1], typed.General[1]
			require.Equal(t, want.IsValid(), got.IsValid())
			if want.IsValid() {
				require.Equal(t, want.Type(), got.Type(), "the register holds a value of the same type")
				require.Equal(t, want.Kind() == reflect.Interface && want.IsNil(), got.Kind() == reflect.Interface && got.IsNil())
				require.Equal(t, want.Interface(), got.Interface())
			}
		})
	}
}

func TestNativeArgumentTakesTheZeroValueOfANilInterface(t *testing.T) {
	t.Parallel()
	require.Nil(t, nativeArgument[types.Type](nil))
	require.Equal(t, types.Typ[types.Int], nativeArgument[types.Type](types.Typ[types.Int]))
}

func TestTypedNativeRowsLeaveAnArgumentOfAnotherTypeToTheReflectiveCall(t *testing.T) {
	t.Parallel()
	registers := makeRegisters()
	registers.General[0] = reflect.ValueOf(struct{ name string }{name: "not a types.Type"})
	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}},
		Returns:   []program.VarLocation{{Register: 1, Kind: isa.RegisterBool}},
	}

	taken, _, _ := tryNativeFastPath(nil, site, types.IsInterface, &registers)
	require.False(t, taken, "a value that does not hold the parameter's interface goes through the reflective call")
	require.False(t, fastPathArgumentsFit(fastPathTagTypesTypeBool, site, &registers))

	registers.General[0] = reflect.ValueOf(types.Type(types.Typ[types.Int]))
	require.True(t, fastPathArgumentsFit(fastPathTagTypesTypeBool, site, &registers))
	registers.General[0] = reflect.Zero(reflect.TypeFor[types.Type]())
	require.True(t, fastPathArgumentsFit(fastPathTagTypesTypeBool, site, &registers), "a nil interface is passed as nil")
	require.True(t, fastPathArgumentsFit(fastPathTagStringString, site, &registers), "a tag without interface parameters needs no check")
}
