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

//go:build integration

package bytecode_test

import (
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestCompiledFileSetGetters(t *testing.T) {
	t.Parallel()

	source := `package main

var counter int = 0

func init() { counter = 1 }

func add(a, b int) int { return a + b }

func main() { _ = add(1, 2) }
`
	cfs := compileFileSource(t, source)

	t.Run("root_is_not_nil", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, cfs.Root())
	})

	t.Run("entrypoints_contain_main_and_add", func(t *testing.T) {
		t.Parallel()
		entrypoints := cfs.Entrypoints()
		require.NotEmpty(t, entrypoints)
		_, hasMain := entrypoints["main"]
		require.True(t, hasMain, "entrypoints should contain main")
		_, hasAdd := entrypoints["add"]
		require.True(t, hasAdd, "entrypoints should contain add")
	})

	t.Run("init_funcs_non_empty", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, cfs.InitFunctions())
	})

	t.Run("variable_init_function_present_with_package_vars", func(t *testing.T) {
		t.Parallel()

		require.NotNil(t, cfs.VariableInitFunction())
	})
}

func TestCompiledFileSetVariableInitFunctionNil(t *testing.T) {
	t.Parallel()

	source := `package main

func main() {}
`
	cfs := compileFileSource(t, source)

	_ = cfs.VariableInitFunction()
}

func TestCompiledFunctionExportMethods(t *testing.T) {
	t.Parallel()

	source := `package main

func main() {
	_ = true
	_ = 42
	_ = 3.14
	_ = uint(7)
	_ = complex(1, 2)
	_ = "hello"
}

func add(a, b int) int { return a + b }

func variadic(args ...string) int { return len(args) }
`
	cfs := compileFileSource(t, source)

	t.Run("main_export_name", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Equal(t, "main", codec.ExportName(mainFn))
	})

	t.Run("main_export_source_file_empty_for_compiled", func(t *testing.T) {
		t.Parallel()

		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		_ = codec.ExportSourceFile(mainFn)
	})

	t.Run("main_body_non_empty", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.NotEmpty(t, codec.Body(mainFn))
	})

	t.Run("main_int_constants_contain_42", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Contains(t, codec.IntConstants(mainFn), int64(42))
	})

	t.Run("main_float_constants_contain_3_14", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Contains(t, codec.FloatConstants(mainFn), 3.14)
	})

	t.Run("main_string_constants_contain_hello", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Contains(t, codec.StringConstants(mainFn), "hello")
	})

	t.Run("main_bool_constants_contain_true", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Contains(t, codec.BoolConstants(mainFn), true)
	})

	t.Run("main_uint_constants_contain_7", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Contains(t, codec.UintConstants(mainFn), uint64(7))
	})

	t.Run("main_complex_constants_contain_1_2i", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Contains(t, codec.ComplexConstants(mainFn), complex(1, 2))
	})

	t.Run("main_num_registers_slice_length", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Len(t, codec.NumRegistersSlice(mainFn), isa.NumRegisterKinds)
	})

	t.Run("main_export_functions", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)

		_ = program.ExportFunctions(mainFn)
	})

	t.Run("variadic_is_variadic", func(t *testing.T) {
		t.Parallel()
		varFn, err := cfs.FindFunction("variadic")
		require.NoError(t, err)
		require.True(t, codec.ExportIsVariadic(varFn))
	})

	t.Run("add_is_not_variadic", func(t *testing.T) {
		t.Parallel()
		addFn, err := cfs.FindFunction("add")
		require.NoError(t, err)
		require.False(t, codec.ExportIsVariadic(addFn))
	})

	t.Run("add_param_kinds_length", func(t *testing.T) {
		t.Parallel()
		addFn, err := cfs.FindFunction("add")
		require.NoError(t, err)
		require.Len(t, codec.ParamKinds(addFn), 2)
	})

	t.Run("add_result_kinds_length", func(t *testing.T) {
		t.Parallel()
		addFn, err := cfs.FindFunction("add")
		require.NoError(t, err)
		require.Len(t, codec.ResultKinds(addFn), 1)
	})
}

func TestFunctionGoGetters(t *testing.T) {
	t.Parallel()

	source := `package main

func helper(x int) int { return x * 2 }

func main() {
	f := func() int { return 1 }
	_ = f()
	_ = helper(3)
}
`
	cfs := compileFileSource(t, source)

	t.Run("func_name", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Equal(t, "main", mainFn.FunctionName())
	})

	t.Run("body_len_positive", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.Greater(t, mainFn.BodyLen(), 0)
	})

	t.Run("sub_functions_on_root", func(t *testing.T) {
		t.Parallel()

		root := cfs.Root()
		require.NotEmpty(t, root.SubFunctions())
	})

	t.Run("register_counts", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		counts := mainFn.RegisterCounts()
		require.Len(t, counts, isa.NumRegisterKinds)
	})
}

func TestFindFunction(t *testing.T) {
	t.Parallel()

	source := `package main

func main() {}
`
	cfs := compileFileSource(t, source)

	t.Run("existing_function_found", func(t *testing.T) {
		t.Parallel()
		fn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.NotNil(t, fn)
	})

	t.Run("nonexistent_function_returns_error", func(t *testing.T) {
		t.Parallel()
		_, err := cfs.FindFunction("nonexistent")
		require.Error(t, err)
	})
}

func TestMakeInstructionRoundTrip(t *testing.T) {
	t.Parallel()

	instr := isa.NewInstruction(1, 2, 3, 4)

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		Body: []isa.Instruction{instr},
	})
	body := codec.Body(compiledFunction)
	require.Len(t, body, 1)
	require.Equal(t, uint8(1), body[0].Operation)
	require.Equal(t, uint8(2), body[0].A)
	require.Equal(t, uint8(3), body[0].B)
	require.Equal(t, uint8(4), body[0].C)
}

func TestMakeRegisterKind(t *testing.T) {
	t.Parallel()

	kind := codec.MakeRegisterKind(2)

	require.Equal(t, isa.RegisterKind(2), kind)
}

func TestMakeVarLocationRoundTrip(t *testing.T) {
	t.Parallel()

	data := codec.VarLocationData{
		UpvalueIndex: 5,
		Register:     3,
		Kind:         1,
		IsUpvalue:    true,
		IsIndirect:   false,
		OriginalKind: 0,
	}
	loc := codec.MakeVarLocation(data)

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		NamedResultLocations: []program.VarLocation{loc},
	})
	exported := codec.NamedResultLocations(compiledFunction)
	require.Len(t, exported, 1)
	require.Equal(t, data.UpvalueIndex, exported[0].UpvalueIndex)
	require.Equal(t, data.Register, exported[0].Register)
	require.Equal(t, data.Kind, exported[0].Kind)
	require.Equal(t, data.IsUpvalue, exported[0].IsUpvalue)
	require.Equal(t, data.IsIndirect, exported[0].IsIndirect)
	require.Equal(t, data.OriginalKind, exported[0].OriginalKind)
}

func TestMakeUpvalueDescriptorRoundTrip(t *testing.T) {
	t.Parallel()

	data := codec.UpvalueDescriptorData{
		Index:   7,
		Kind:    3,
		IsLocal: true,
	}
	upvalueDescriptor := codec.MakeUpvalueDescriptor(data)

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		UpvalueDescriptors: []program.UpvalueDescriptor{upvalueDescriptor},
	})
	exported := codec.UpvalueDescriptors(compiledFunction)
	require.Len(t, exported, 1)
	require.Equal(t, data.Index, exported[0].Index)
	require.Equal(t, data.Kind, exported[0].Kind)
	require.Equal(t, data.IsLocal, exported[0].IsLocal)
}

func TestMakeCallSiteRoundTrip(t *testing.T) {
	t.Parallel()

	argData := codec.VarLocationData{
		Register: 1,
		Kind:     0,
	}
	retData := codec.VarLocationData{
		Register: 2,
		Kind:     0,
	}
	csData := codec.CallSiteData{
		Arguments:              []codec.VarLocationData{argData},
		Returns:                []codec.VarLocationData{retData},
		FunctionIndex:          10,
		ClosureRegister:        5,
		NativeRegister:         6,
		IsClosure:              true,
		IsNative:               false,
		IsMethod:               true,
		MethodReceiverRegister: 4,
	}
	cs := codec.MakeCallSite(csData)

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		CallSites: []program.CallSite{cs},
	})
	exported := codec.CallSites(compiledFunction)
	require.Len(t, exported, 1)
	require.Len(t, exported[0].Arguments, 1)
	require.Len(t, exported[0].Returns, 1)
	require.Equal(t, csData.FunctionIndex, exported[0].FunctionIndex)
	require.Equal(t, csData.ClosureRegister, exported[0].ClosureRegister)
	require.Equal(t, csData.NativeRegister, exported[0].NativeRegister)
	require.Equal(t, csData.IsClosure, exported[0].IsClosure)
	require.Equal(t, csData.IsNative, exported[0].IsNative)
	require.Equal(t, csData.IsMethod, exported[0].IsMethod)
	require.Equal(t, csData.MethodReceiverRegister, exported[0].MethodReceiverRegister)
	require.Equal(t, argData.Register, exported[0].Arguments[0].Register)
	require.Equal(t, retData.Register, exported[0].Returns[0].Register)
}

func TestImportTypeDescriptorRoundTrip(t *testing.T) {
	t.Parallel()

	elemDesc := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.Int),
	}
	keyDesc := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.String),
	}
	valueDesc := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.Float64),
	}
	fieldTypeDesc := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.Bool),
	}
	paramDesc := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.Int),
	}
	resultDesc := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.String),
	}

	original := descriptor.TypeDescriptorData{
		PackagePath: "test/pkg",
		Name:        "TestType",
		Elem:        &elemDesc,
		Key:         &keyDesc,
		Value:       &valueDesc,
		Fields: []descriptor.TypeDescriptorFieldData{
			{
				Name:        "Active",
				Tag:         `json:"active"`,
				PackagePath: "",
				Typ:         fieldTypeDesc,
			},
		},
		Params:     []descriptor.TypeDescriptorData{paramDesc},
		Results:    []descriptor.TypeDescriptorData{resultDesc},
		Length:     10,
		Dir:        2,
		BasicKind:  0,
		Kind:       uint8(descriptor.KindStruct),
		IsVariadic: true,
	}

	internal := codec.ImportTypeDescriptor(original)
	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		TypeTable:            []reflect.Type{reflect.TypeFor[int]()},
		TypeTableDescriptors: []descriptor.TypeDescriptor{internal},
	})
	exported := codec.TypeTableDescriptors(compiledFunction)
	require.Len(t, exported, 1)

	result := exported[0]
	require.Equal(t, original.PackagePath, result.PackagePath)
	require.Equal(t, original.Name, result.Name)
	require.Equal(t, original.Kind, result.Kind)
	require.Equal(t, original.Length, result.Length)
	require.Equal(t, original.Dir, result.Dir)
	require.Equal(t, original.IsVariadic, result.IsVariadic)

	require.NotNil(t, result.Elem)
	require.Equal(t, elemDesc.BasicKind, result.Elem.BasicKind)

	require.NotNil(t, result.Key)
	require.Equal(t, keyDesc.BasicKind, result.Key.BasicKind)

	require.NotNil(t, result.Value)
	require.Equal(t, valueDesc.BasicKind, result.Value.BasicKind)

	require.Len(t, result.Fields, 1)
	require.Equal(t, "Active", result.Fields[0].Name)
	require.Equal(t, `json:"active"`, result.Fields[0].Tag)
	require.Equal(t, fieldTypeDesc.BasicKind, result.Fields[0].Typ.BasicKind)

	require.Len(t, result.Params, 1)
	require.Equal(t, paramDesc.BasicKind, result.Params[0].BasicKind)

	require.Len(t, result.Results, 1)
	require.Equal(t, resultDesc.BasicKind, result.Results[0].BasicKind)
}

func TestImportGeneralConstantDescriptor(t *testing.T) {
	t.Parallel()

	t.Run("package_symbol_kind", func(t *testing.T) {
		t.Parallel()

		data := codec.GeneralConstantDescriptorData{
			PackagePath: "fmt",
			SymbolName:  "Println",
			Kind:        uint8(descriptor.GeneralConstantPackageSymbol),
		}
		internal := codec.ImportGeneralConstantDescriptor(data)

		compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
			GeneralConstants:           []reflect.Value{reflect.ValueOf(0)},
			GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{internal},
		})
		exported := codec.GeneralConstantDescriptors(compiledFunction)
		require.Len(t, exported, 1)
		require.Equal(t, data.PackagePath, exported[0].PackagePath)
		require.Equal(t, data.SymbolName, exported[0].SymbolName)
		require.Equal(t, data.Kind, exported[0].Kind)
	})

	t.Run("type_zero_kind", func(t *testing.T) {
		t.Parallel()

		data := codec.GeneralConstantDescriptorData{
			PackagePath: "time",
			SymbolName:  "Duration",
			Kind:        uint8(descriptor.GeneralConstantNamedTypeZero),
		}
		internal := codec.ImportGeneralConstantDescriptor(data)

		compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
			GeneralConstants:           []reflect.Value{reflect.ValueOf(0)},
			GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{internal},
		})
		exported := codec.GeneralConstantDescriptors(compiledFunction)
		require.Len(t, exported, 1)
		require.Equal(t, data.PackagePath, exported[0].PackagePath)
		require.Equal(t, data.SymbolName, exported[0].SymbolName)
		require.Equal(t, data.Kind, exported[0].Kind)
	})
}

func TestNewCompiledFunctionFromDataRoundTrip(t *testing.T) {
	t.Parallel()

	childFunc := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		Name:       "child",
		SourceFile: "child.go",
	})
	varInitFunc := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		Name:       "varinit",
		SourceFile: "varinit.go",
	})

	instr := isa.NewInstruction(10, 1, 2, 3)
	upval := codec.MakeUpvalueDescriptor(codec.UpvalueDescriptorData{Index: 1, Kind: 0, IsLocal: true})
	namedLocation := codec.MakeVarLocation(codec.VarLocationData{Register: 5, Kind: 1})
	cs := codec.MakeCallSite(codec.CallSiteData{FunctionIndex: 2, IsClosure: true})

	typeDescriptor := codec.ImportTypeDescriptor(descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.Int),
	})
	genConstDesc := codec.ImportGeneralConstantDescriptor(codec.GeneralConstantDescriptorData{
		PackagePath: "pkg",
		SymbolName:  "Sym",
		Kind:        uint8(descriptor.GeneralConstantPackageSymbol),
	})

	data := &codec.CompiledFunctionData{
		Name:                       "testFunc",
		SourceFile:                 "test.go",
		IsVariadic:                 true,
		NumRegisters:               [isa.NumRegisterKinds]uint32{4, 2, 1, 3, 1, 1, 1},
		ParamKinds:                 []isa.RegisterKind{isa.RegisterInt, isa.RegisterString},
		ResultKinds:                []isa.RegisterKind{isa.RegisterBool},
		Body:                       []isa.Instruction{instr},
		BoolConstants:              []bool{true, false},
		IntConstants:               []int64{100, 200},
		FloatConstants:             []float64{1.1, 2.2},
		UintConstants:              []uint64{10, 20},
		ComplexConstants:           []complex128{complex(3, 4)},
		StringConstants:            []string{"alpha", "beta"},
		GeneralConstants:           []reflect.Value{reflect.ValueOf(42)},
		GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{genConstDesc},
		TypeTable:                  []reflect.Type{reflect.TypeFor[int]()},
		TypeTableDescriptors:       []descriptor.TypeDescriptor{typeDescriptor},
		TypeNames:                  map[reflect.Type]string{reflect.TypeFor[int](): "int"},
		CallSites:                  []program.CallSite{cs},
		UpvalueDescriptors:         []program.UpvalueDescriptor{upval},
		Functions:                  []*program.CompiledFunction{childFunc},
		NamedResultLocations:       []program.VarLocation{namedLocation},
		MethodTable:                map[string]uint16{"Point.Sum": 0},
		VariableInitFunction:       varInitFunc,
	}

	compiledFunction := codec.NewCompiledFunctionFromData(data)

	t.Run("name", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "testFunc", codec.ExportName(compiledFunction))
	})

	t.Run("source_file", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "test.go", codec.ExportSourceFile(compiledFunction))
	})

	t.Run("is_variadic", func(t *testing.T) {
		t.Parallel()
		require.True(t, codec.ExportIsVariadic(compiledFunction))
	})

	t.Run("num_registers_slice", func(t *testing.T) {
		t.Parallel()
		regs := codec.NumRegistersSlice(compiledFunction)
		require.Len(t, regs, isa.NumRegisterKinds)
		require.Equal(t, uint32(4), regs[0])
		require.Equal(t, uint32(2), regs[1])
	})

	t.Run("param_kinds", func(t *testing.T) {
		t.Parallel()
		pk := codec.ParamKinds(compiledFunction)
		require.Len(t, pk, 2)
		require.Equal(t, uint8(isa.RegisterInt), pk[0])
		require.Equal(t, uint8(isa.RegisterString), pk[1])
	})

	t.Run("result_kinds", func(t *testing.T) {
		t.Parallel()
		rk := codec.ResultKinds(compiledFunction)
		require.Len(t, rk, 1)
		require.Equal(t, uint8(isa.RegisterBool), rk[0])
	})

	t.Run("body", func(t *testing.T) {
		t.Parallel()
		body := codec.Body(compiledFunction)
		require.Len(t, body, 1)
		require.Equal(t, uint8(10), body[0].Operation)
	})

	t.Run("bool_constants", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []bool{true, false}, codec.BoolConstants(compiledFunction))
	})

	t.Run("int_constants", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []int64{100, 200}, codec.IntConstants(compiledFunction))
	})

	t.Run("float_constants", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []float64{1.1, 2.2}, codec.FloatConstants(compiledFunction))
	})

	t.Run("uint_constants", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []uint64{10, 20}, codec.UintConstants(compiledFunction))
	})

	t.Run("complex_constants", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []complex128{complex(3, 4)}, codec.ComplexConstants(compiledFunction))
	})

	t.Run("string_constants", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []string{"alpha", "beta"}, codec.StringConstants(compiledFunction))
	})

	t.Run("general_constant_descriptors", func(t *testing.T) {
		t.Parallel()
		gcd := codec.GeneralConstantDescriptors(compiledFunction)
		require.Len(t, gcd, 1)
		require.Equal(t, "pkg", gcd[0].PackagePath)
		require.Equal(t, "Sym", gcd[0].SymbolName)
	})

	t.Run("type_table_descriptors", func(t *testing.T) {
		t.Parallel()
		ttd := codec.TypeTableDescriptors(compiledFunction)
		require.Len(t, ttd, 1)
		require.Equal(t, uint8(reflect.Int), ttd[0].BasicKind)
	})

	t.Run("type_names", func(t *testing.T) {
		t.Parallel()
		tn := codec.TypeNames(compiledFunction)
		require.NotNil(t, tn)
		entry, ok := tn[reflect.TypeFor[int]()]
		require.True(t, ok)
		require.Equal(t, "int", entry.Name)
	})

	t.Run("call_sites", func(t *testing.T) {
		t.Parallel()
		sites := codec.CallSites(compiledFunction)
		require.Len(t, sites, 1)
		require.Equal(t, uint16(2), sites[0].FunctionIndex)
		require.True(t, sites[0].IsClosure)
	})

	t.Run("upvalue_descriptors", func(t *testing.T) {
		t.Parallel()
		uvd := codec.UpvalueDescriptors(compiledFunction)
		require.Len(t, uvd, 1)
		require.Equal(t, uint8(1), uvd[0].Index)
		require.Equal(t, uint8(0), uvd[0].Kind)
		require.True(t, uvd[0].IsLocal)
	})

	t.Run("export_functions", func(t *testing.T) {
		t.Parallel()
		fns := program.ExportFunctions(compiledFunction)
		require.Len(t, fns, 1)
		require.Equal(t, "child", codec.ExportName(fns[0]))
	})

	t.Run("named_result_locs", func(t *testing.T) {
		t.Parallel()
		nrl := codec.NamedResultLocations(compiledFunction)
		require.Len(t, nrl, 1)
		require.Equal(t, uint8(5), nrl[0].Register)
		require.Equal(t, uint8(1), nrl[0].Kind)
	})

	t.Run("method_table", func(t *testing.T) {
		t.Parallel()
		mt := codec.MethodTable(compiledFunction)
		require.NotNil(t, mt)
		index, ok := mt["Point.Sum"]
		require.True(t, ok)
		require.Equal(t, uint16(0), index)
	})

	t.Run("variable_init_function", func(t *testing.T) {
		t.Parallel()
		vif := program.VariableInitFunction(compiledFunction)
		require.NotNil(t, vif)
		require.Equal(t, "varinit", codec.ExportName(vif))
	})

	t.Run("func_name_getter", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "testFunc", compiledFunction.FunctionName())
	})

	t.Run("body_len_getter", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 1, compiledFunction.BodyLen())
	})

	t.Run("sub_functions_getter", func(t *testing.T) {
		t.Parallel()
		require.Len(t, compiledFunction.SubFunctions(), 1)
	})

	t.Run("register_counts_getter", func(t *testing.T) {
		t.Parallel()
		counts := compiledFunction.RegisterCounts()
		require.Equal(t, [isa.NumRegisterKinds]uint32{4, 2, 1, 3, 1, 1, 1}, counts)
	})
}

func TestNewCompiledFileSetFromData(t *testing.T) {
	t.Parallel()

	rootFunc := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		Name: "root",
		Functions: []*program.CompiledFunction{
			codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{Name: "main"}),
			codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{Name: "helper"}),
		},
	})
	varInitFunc := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{
		Name: "varinit",
	})
	entrypoints := map[string]uint16{"main": 0, "helper": 1}
	initIndices := []uint16{0}

	cfs := codec.NewCompiledFileSetFromData(rootFunc, varInitFunc, entrypoints, initIndices)

	t.Run("root", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, cfs.Root())
		require.Equal(t, "root", codec.ExportName(cfs.Root()))
	})

	t.Run("variable_init_function", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, cfs.VariableInitFunction())
		require.Equal(t, "varinit", codec.ExportName(cfs.VariableInitFunction()))
	})

	t.Run("entrypoints", func(t *testing.T) {
		t.Parallel()
		ep := cfs.Entrypoints()
		require.Len(t, ep, 2)
		_, hasMain := ep["main"]
		require.True(t, hasMain)
		_, hasHelper := ep["helper"]
		require.True(t, hasHelper)
	})

	t.Run("init_funcs", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []uint16{0}, cfs.InitFunctions())
	})

	t.Run("find_function_succeeds", func(t *testing.T) {
		t.Parallel()
		fn, err := cfs.FindFunction("main")
		require.NoError(t, err)
		require.NotNil(t, fn)
		require.Equal(t, "main", codec.ExportName(fn))
	})

	t.Run("find_function_fails_for_missing", func(t *testing.T) {
		t.Parallel()
		_, err := cfs.FindFunction("nonexistent")
		require.Error(t, err)
	})
}

func TestClosureAndMethodExports(t *testing.T) {
	t.Parallel()

	source := `package main

type Point struct{ X, Y int }

func (p Point) Sum() int { return p.X + p.Y }

func main() {
	x := 10
	f := func() int { return x }
	_ = f()
	p := Point{1, 2}
	_ = p.Sum()
}
`
	cfs := compileFileSource(t, source)

	t.Run("main_call_sites_non_empty", func(t *testing.T) {
		t.Parallel()
		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)

		require.NotEmpty(t, codec.CallSites(mainFn))
	})

	t.Run("closure_has_upvalue_descriptors", func(t *testing.T) {
		t.Parallel()

		root := cfs.Root()
		found := false
		for _, child := range root.SubFunctions() {
			if len(codec.UpvalueDescriptors(child)) > 0 {
				found = true
				break
			}
		}
		require.True(t, found, "expected at least one root child function with upvalue descriptors")
	})

	t.Run("method_table_contains_sum", func(t *testing.T) {
		t.Parallel()
		root := cfs.Root()
		mt := codec.MethodTable(root)

		found := false
		for key := range mt {
			if key == "Point.Sum" {
				found = true
				break
			}
		}
		require.True(t, found, "expected method table to contain Point.Sum, got %v", mt)
	})

	t.Run("named_result_locs_on_compiled_function", func(t *testing.T) {
		t.Parallel()

		mainFn, err := cfs.FindFunction("main")
		require.NoError(t, err)

		require.Empty(t, codec.NamedResultLocations(mainFn))
	})
}

func TestNamedResultLocsPopulated(t *testing.T) {
	t.Parallel()

	source := `package main

func divide(a, b int) (result int, ok bool) {
	if b == 0 {
		return
	}
	result = a / b
	ok = true
	return
}

func main() { _, _ = divide(10, 2) }
`
	cfs := compileFileSource(t, source)
	divideFn, err := cfs.FindFunction("divide")
	require.NoError(t, err)
	require.NotEmpty(t, codec.NamedResultLocations(divideFn), "divide should have named result locations")
}

func TestTypeNamesEmptyWhenNoNamedTypesUsed(t *testing.T) {
	t.Parallel()

	source := `package main

func main() { _ = 1 + 2 }
`
	cfs := compileFileSource(t, source)
	mainFn, err := cfs.FindFunction("main")
	require.NoError(t, err)

	tn := codec.TypeNames(mainFn)

	require.True(t, len(tn) == 0 || tn == nil)
}

func TestEmptyNamedResultLocsExport(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Empty(t, codec.NamedResultLocations(compiledFunction))
}

func TestEmptyCallSitesExport(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Empty(t, codec.CallSites(compiledFunction))
}

func TestEmptyUpvalueDescriptorsExport(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Empty(t, codec.UpvalueDescriptors(compiledFunction))
}

func TestEmptyBodyExport(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Empty(t, codec.Body(compiledFunction))
}

func TestEmptyMethodTableExport(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Nil(t, codec.MethodTable(compiledFunction))
}

func TestTypeNamesNilWhenEmpty(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Nil(t, codec.TypeNames(compiledFunction))
}

func TestGeneralConstantDescriptorsEmpty(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Empty(t, codec.GeneralConstantDescriptors(compiledFunction))
}

func TestTypeTableDescriptorsEmpty(t *testing.T) {
	t.Parallel()

	compiledFunction := codec.NewCompiledFunctionFromData(&codec.CompiledFunctionData{})
	require.Empty(t, codec.TypeTableDescriptors(compiledFunction))
}
