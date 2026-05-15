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
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type registryProbe interface {
	Ping(n int) int
}

type registryProbeAdapter struct{ core *AdapterCore }

func (a *registryProbeAdapter) Ping(n int) int {
	results, err := a.core.Invoke("Ping", reflect.ValueOf(n))
	if err != nil || len(results) == 0 {
		return -1
	}
	return int(results[0].Int())
}

func registryProbeRoot(parameterCount int) *program.CompiledFunction {
	root := program.NewNamedFunction("root")
	method := program.NewNamedFunction("Probe.Ping")
	method.HasReceiver = true
	method.ParameterKinds = append([]isa.RegisterKind{isa.RegisterGeneral}, make([]isa.RegisterKind, parameterCount)...)
	for i := 1; i < len(method.ParameterKinds); i++ {
		method.ParameterKinds[i] = isa.RegisterInt
	}
	method.ResultKinds = []isa.RegisterKind{isa.RegisterInt}
	root.Functions = append(root.Functions, method)
	if err := program.RegisterMethod(root, "Probe.Ping", 0); err != nil {
		panic(err)
	}
	return root
}

func TestRegisteredAdapterBuildsOnlyForMatchingMethods(t *testing.T) {
	probeType := reflect.TypeFor[registryProbe]()
	RegisterInterfaceAdapter(probeType, func(core *AdapterCore) reflect.Value {
		return reflect.ValueOf(&registryProbeAdapter{core: core})
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
			vm := NewVM(context.Background(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
			vm.rootFunction = tt.root
			adapter := buildRegisteredAdapter(vm, reflect.ValueOf(struct{ N int }{1}), probeType, tt.typeName)
			require.Equal(t, tt.want, adapter.IsValid())
			if tt.want {
				require.True(t, adapter.Type().Implements(probeType))
			}
		})
	}
}

func TestAdapterCoreInvokeRejectsUnknownMethods(t *testing.T) {
	vm := NewVM(context.Background(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Panics(t, func() { RegisterInterfaceAdapter(tt.iface, tt.build) })
		})
	}
}

func TestRegisterInterfaceAdapterIsSafeUnderConcurrentLookups(t *testing.T) {
	type concurrentProbe interface{ Tick() }
	var group sync.WaitGroup
	for range 8 {
		group.Add(2)
		go func() {
			defer group.Done()
			RegisterInterfaceAdapter(reflect.TypeFor[concurrentProbe](), func(*AdapterCore) reflect.Value { return reflect.Value{} })
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
