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

package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/rootfs"
)

const recoverRoundTripSource = `package main

func guarded(divisor int) (result int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = -1
		}
	}()
	return 100 / divisor
}

func EntrypointRun() int { return guarded(0) }
`

func TestRecoverSurvivesBytecodeRoundTrip(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := rootfs.Open(directory)
	require.NoError(t, err)
	bytecodeStore := adapters.NewBytecodeStore(store)

	service := newTestService(t, WithBytecodeStore(bytecodeStore))
	compiled, err := service.CompileFileSet(context.Background(),
		map[string]string{"main.go": recoverRoundTripSource})
	require.NoError(t, err)

	direct, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, -1, direct, "the compiled program recovers from the division panic")

	require.NoError(t, service.SaveCompiled(context.Background(), "recover-round-trip", compiled))
	loaded, err := service.LoadCompiled(context.Background(), "recover-round-trip")
	require.NoError(t, err)

	reloaded, err := service.ExecuteEntrypoint(context.Background(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, -1, reloaded, "recover must work identically in a loaded bundle")

	recoveringFunctions := 0
	for _, function := range program.ExportFunctions(loaded.Root()) {
		if !function.HasRecover {
			continue
		}
		recoveringFunctions++
	}
	require.Equalf(t, 2, recoveringFunctions,
		"guarded and its deferred closure both call recover, so both must carry HasRecover\n"+
			"after unpacking. A lower count means the codec dropped the flag.")
}

const receiverRoundTripSource = `package main

type counter struct{ n int }

func (c *counter) Add(delta int) int {
	c.n += delta
	return c.n
}

func EntrypointRun() int {
	c := &counter{}
	c.Add(2)
	double := func(v int) int { return v * 2 }
	var boxed any = double
	if _, isFunc := boxed.(func(int) int); !isFunc {
		return -1
	}
	if _, wrongShape := boxed.(func(string) int); wrongShape {
		return -2
	}
	return c.Add(3)
}
`

func TestReceiverAndSignatureSurviveBytecodeRoundTrip(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := rootfs.Open(directory)
	require.NoError(t, err)
	bytecodeStore := adapters.NewBytecodeStore(store)

	service := newTestService(t, WithBytecodeStore(bytecodeStore))
	compiled, err := service.CompileFileSet(context.Background(),
		map[string]string{"main.go": receiverRoundTripSource})
	require.NoError(t, err)

	direct, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 5, direct,
		"the closure must assert to its own func type and not to a differently shaped one")

	require.NoError(t, service.SaveCompiled(context.Background(), "receiver-round-trip", compiled))
	loaded, err := service.LoadCompiled(context.Background(), "receiver-round-trip")
	require.NoError(t, err)

	reloaded, err := service.ExecuteEntrypoint(context.Background(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, direct, reloaded,
		"a loaded bundle must keep both the receiver marking and closure signature identity")

	methods := 0
	signatures := 0
	for _, function := range program.ExportFunctions(loaded.Root()) {
		if function.HasReceiver {
			methods++
		}
		if function.SignatureReflectType != nil {
			signatures++
		}
	}
	require.Positivef(t, signatures,
		"a loaded function must carry its recorded signature type; without it a closure has no\n"+
			"func identity, so a type assertion, a type switch, reflect.TypeOf and %%T all miss it.")
	require.Equalf(t, 1, methods,
		"Add is the only method, and its receiver must still be marked one after unpacking.\n"+
			"A zero count means the codec dropped HasReceiver, which makes every method of a\n"+
			"loaded module look like it takes one argument too many and lose the Format, Error\n"+
			"and String adapters fmt dispatches through.")
}
