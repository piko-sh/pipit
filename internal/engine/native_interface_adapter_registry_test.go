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
	"io/fs"
	"reflect"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type registryProbe interface {
	Ping(n int) int
}

type registryProbeAdapter struct{ *AdapterCore }

func (a *registryProbeAdapter) Ping(n int) int {
	results, err := a.Invoke("Ping", reflect.ValueOf(n))
	if err != nil || len(results) == 0 {
		return -1
	}
	return int(results[0].Int())
}

type bareProbeAdapter struct{}

func (bareProbeAdapter) Ping(n int) int { return n }

func registryProbeRoot(parameterCount int) *program.CompiledFunction {
	root := program.NewNamedFunction("root")
	method := program.NewNamedFunction("Probe.Ping")
	method.HasReceiver = true
	method.ParameterKinds = append([]isa.RegisterKind{isa.RegisterGeneral}, make([]isa.RegisterKind, parameterCount)...)
	for i := 1; i < len(method.ParameterKinds); i++ {
		method.ParameterKinds[i] = isa.RegisterInt64
	}
	method.ResultKinds = []isa.RegisterKind{isa.RegisterInt64}
	root.Functions = append(root.Functions, method)
	if err := program.RegisterMethod(root, "Probe.Ping", 0); err != nil {
		panic(err)
	}
	return root
}

func TestRegisteredAdapterBuildsOnlyForMatchingMethods(t *testing.T) {
	probeType := reflect.TypeFor[registryProbe]()
	RegisterInterfaceAdapter(probeType, func(core *AdapterCore) reflect.Value {
		return reflect.ValueOf(&registryProbeAdapter{AdapterCore: core})
	})
	require.True(t, IsSupportedAdapterInterface(probeType))
	require.False(t, IsSupportedAdapterInterface(reflect.TypeFor[interface{ Pong() }]()))

	tests := []struct {
		name     string
		root     *program.CompiledFunction
		typeName string
		want     bool
	}{
		{name: "method with the interface's shape", root: registryProbeRoot(1), typeName: "Probe", want: true},
		{name: "method with a different arity", root: registryProbeRoot(2), typeName: "Probe", want: false},
		{name: "type without the method", root: registryProbeRoot(1), typeName: "Other", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := NewVM(t.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
			vm.rootFunction = tt.root
			adapter := buildRegisteredAdapter(vm, reflect.ValueOf(struct{ N int }{N: 1}), probeType, tt.typeName)
			require.Equal(t, tt.want, adapter.IsValid())
			if tt.want {
				require.True(t, adapter.Type().Implements(probeType))
			}
		})
	}
}

func TestAdapterCoreInvokeRejectsUnknownMethods(t *testing.T) {
	vm := NewVM(t.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	vm.rootFunction = registryProbeRoot(1)
	core := &AdapterCore{vm: vm, underlying: reflect.ValueOf(1), typeName: "Probe", methods: map[string]adapterMethodRef{}}
	_, err := core.Invoke("Ping")
	require.ErrorContains(t, err, "no method Ping")
	require.Equal(t, reflect.ValueOf(1).Interface(), core.Underlying().Interface())
}

func TestRegisterInterfaceAdapterPanicsOnMisuse(t *testing.T) {
	tests := []struct {
		name  string
		iface reflect.Type
		build adapterBuilder
	}{
		{name: "non-interface type", iface: reflect.TypeFor[int](), build: func(*AdapterCore) reflect.Value { return reflect.Value{} }},
		{name: "empty interface", iface: reflect.TypeFor[any](), build: func(*AdapterCore) reflect.Value { return reflect.Value{} }},
		{name: "nil builder", iface: reflect.TypeFor[registryProbe](), build: nil},
		{name: "an adapter without the core", iface: reflect.TypeFor[registryProbe](), build: func(*AdapterCore) reflect.Value {
			return reflect.ValueOf(bareProbeAdapter{})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Panics(t, func() { RegisterInterfaceAdapter(tt.iface, tt.build) })
		})
	}
}

func TestRegisterAdapterFamilyRejectsAnOptionalThatIsNotAnInterface(t *testing.T) {
	require.PanicsWithValue(t, "engine.RegisterInterfaceAdapter: int is not an interface with methods", func() {
		registerAdapterFamily([]reflect.Type{reflect.TypeFor[registryProbe]()}, []reflect.Type{reflect.TypeFor[int]()},
			func(core *AdapterCore) reflect.Value {
				return reflect.ValueOf(&registryProbeAdapter{AdapterCore: core})
			})
	})
}

func TestAdapterCoreSatisfiesChecksTheMethodShape(t *testing.T) {
	t.Parallel()

	vm := probeVM(t, "File", probeMethod{name: "Stat", results: valueAndError})
	core := &AdapterCore{
		vm:         vm,
		methods:    map[string]adapterMethodRef{"Stat": {root: nil, index: 0}},
		typeName:   "File",
		underlying: reflect.ValueOf(struct{ N int }{N: 1}),
	}
	require.True(t, core.satisfies(program.DecodeInterfaceMethodRequirement("Stat/0/2")), "fs.File's Stat()")
	require.False(t, core.satisfies(program.DecodeInterfaceMethodRequirement("Stat/1/2")), "fs.StatFS's Stat(name)")
	require.True(t, core.satisfies(program.DecodeInterfaceMethodRequirement("Stat")), "an unrecorded shape is not checked")
	require.False(t, core.satisfies(program.DecodeInterfaceMethodRequirement("Open/1/2")))
	require.True(t, adapterResolvedAll(core, []string{"Stat/0/2"}))
	require.False(t, adapterResolvedAll(core, []string{"Stat/0/2", "Stat/1/2"}))
}

func TestTypedResult(t *testing.T) {
	t.Parallel()

	core := &AdapterCore{vm: nil, methods: nil, typeName: "Site", underlying: reflect.Value{}}
	matches, err := core.typedResult[[]string]("Glob", []reflect.Value{reflect.ValueOf([]string{"a"})}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, matches)

	matches, err = core.typedResult[[]string]("Glob", []reflect.Value{reflect.Zero(reflect.TypeFor[any]())}, 0)
	require.NoError(t, err)
	require.Nil(t, matches)

	_, err = core.typedResult[[]string]("Glob", []reflect.Value{reflect.ValueOf([]int{1})}, 0)
	require.EqualError(t, err, "pipit adapter for Site: Glob returned []int, not []string")
}

func TestRegisterInterfaceAdapterIsSafeUnderConcurrentLookups(t *testing.T) {
	type concurrentProbe interface{ Tick() }
	var group sync.WaitGroup
	for range 8 {
		group.Add(2)
		go func() {
			defer group.Done()
			RegisterInterfaceAdapter(reflect.TypeFor[concurrentProbe](), func(core *AdapterCore) reflect.Value {
				return reflect.ValueOf(&registryProbeAdapter{AdapterCore: core})
			})
		}()
		go func() {
			defer group.Done()
			for range 64 {
				_ = IsSupportedAdapterInterface(reflect.TypeFor[concurrentProbe]())
			}
		}()
	}
	group.Wait()
	require.True(t, IsSupportedAdapterInterface(reflect.TypeFor[concurrentProbe]()))
}

func TestOptionalMethodGroupsCountOnlyWhenComplete(t *testing.T) {
	t.Parallel()

	value := reflect.ValueOf(struct{ N int }{N: 1})
	pointer := reflect.ValueOf(&struct{ N int }{N: 1})

	tests := []struct {
		name    string
		methods []probeMethod
		value   reflect.Value
		want    map[string]bool
	}{
		{
			name:    "Open alone",
			methods: []probeMethod{probeOpen},
			value:   value,
			want:    map[string]bool{"Open": true, "Stat": false, "ReadLink": false, "Lstat": false},
		},
		{
			name:    "ReadLink without Lstat is not fs.ReadLinkFS",
			methods: []probeMethod{probeOpen, probeReadLink},
			value:   value,
			want:    map[string]bool{"ReadLink": false, "Lstat": false},
		},
		{
			name:    "ReadLink and Lstat together",
			methods: []probeMethod{probeOpen, probeReadLink, probeLstat},
			value:   value,
			want:    map[string]bool{"ReadLink": true, "Lstat": true},
		},
		{
			name:    "a parameter in the wrong bank",
			methods: []probeMethod{probeOpen, {name: "Stat", parameters: []isa.RegisterKind{isa.RegisterInt64}, results: probeStat.results}},
			value:   value,
			want:    map[string]bool{"Stat": false},
		},
		{
			name:    "a result in the wrong bank",
			methods: []probeMethod{probeOpen, probeLstat, {name: "ReadLink", parameters: nameParameter, results: valueAndError}},
			value:   value,
			want:    map[string]bool{"ReadLink": false, "Lstat": false},
		},
		{
			name:    "the wrong arity",
			methods: []probeMethod{probeOpen, {name: "Stat", parameters: nil, results: probeStat.results}},
			value:   value,
			want:    map[string]bool{"Stat": false},
		},
		{
			name:    "a pointer method on a value",
			methods: []probeMethod{probeOpen, {name: "Stat", parameters: probeStat.parameters, results: probeStat.results, pointer: true}},
			value:   value,
			want:    map[string]bool{"Stat": false},
		},
		{
			name: "a result of the wrong type",
			methods: []probeMethod{probeOpen, {
				name:       "Glob",
				parameters: nameParameter,
				results:    valueAndError,
				signature:  reflect.TypeFor[func(string) ([]int, error)](),
			}},
			value: value,
			want:  map[string]bool{"Glob": false},
		},
		{
			name: "a result of the right type",
			methods: []probeMethod{probeOpen, {
				name:       "Glob",
				parameters: nameParameter,
				results:    valueAndError,
				signature:  reflect.TypeFor[func(string) ([]string, error)](),
			}},
			value: value,
			want:  map[string]bool{"Glob": true},
		},
		{
			name:    "a pointer method on a pointer",
			methods: []probeMethod{probeOpen, {name: "Stat", parameters: probeStat.parameters, results: probeStat.results, pointer: true}},
			value:   pointer,
			want:    map[string]bool{"Stat": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			core := builtFSCore(t, probeVM(t, "Site", tt.methods...), tt.value)
			for method, want := range tt.want {
				require.Equal(t, want, core.has(method), method)
			}
		})
	}
}

func TestAdaptResult(t *testing.T) {
	t.Parallel()

	vm := probeVM(t, "Site", probeOpen)
	core := &AdapterCore{vm: vm, methods: nil, typeName: "Site", underlying: reflect.Value{}}
	native, err := fstest.MapFS{"a": {Data: []byte("x")}}.Open("a")
	require.NoError(t, err)
	var nilFile fs.File

	tests := []struct {
		name    string
		results []reflect.Value
		want    fs.File
		wantErr string
	}{
		{name: "a missing result", results: nil},
		{name: "an invalid result", results: []reflect.Value{{}}},
		{name: "a nil interface", results: []reflect.Value{reflect.ValueOf(&nilFile).Elem()}},
		{name: "a native file passes through", results: []reflect.Value{reflect.ValueOf(native)}, want: native},
		{name: "a value that is not a file", results: []reflect.Value{reflect.ValueOf(42)}, wantErr: "pipit adapter for Site: Open returned int, which does not implement fs.File"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := core.adaptResult[fs.File]("Open", tt.results, 0)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, file)
		})
	}
}

type promotedProbe interface {
	Name() string
}

type promotedProbeAdapter struct{ *AdapterCore }

func (a *promotedProbeAdapter) Name() string {
	results, err := a.Invoke("Name")
	if err != nil || len(results) == 0 {
		return err.Error()
	}
	return results[0].String()
}

func TestARegisteredAdapterServesAMethodPromotedFromAnEmbeddedField(t *testing.T) {
	probeType := reflect.TypeFor[promotedProbe]()
	RegisterInterfaceAdapter(probeType, func(core *AdapterCore) reflect.Value {
		return reflect.ValueOf(&promotedProbeAdapter{AdapterCore: core})
	})
	innerType := reflect.StructOf([]reflect.StructField{
		{Name: "N", Type: reflect.TypeFor[int]()},
		{Name: pipitIDFieldPrefix + "Inner", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
	outerType := reflect.StructOf([]reflect.StructField{
		{Name: isa.EmbeddedUnexportedPrefix + "Inner", Type: innerType},
		{Name: pipitIDFieldPrefix + "Outer", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
	vm := adapterHostVM(t, "Inner.Name", stringReturningMethod(t, "Name", "inner"))

	adapter := buildRegisteredAdapter(vm, reflect.New(outerType).Elem(), probeType, "Outer")
	require.True(t, adapter.IsValid(), "the method promoted from the embedded field satisfies the interface, as in Go")
	probe, ok := reflect.TypeAssert[promotedProbe](adapter)
	require.True(t, ok)
	require.Equal(t, "inner", probe.Name())
	core, ok := registeredAdapterCore(adapter)
	require.True(t, ok)
	require.Equal(t, []int{0}, core.methods["Name"].path, "the call goes through the embedded field")

	require.False(t, buildRegisteredAdapter(vm, reflect.New(innerType).Elem(), reflect.TypeFor[registryProbe](), "Inner").IsValid())
}
