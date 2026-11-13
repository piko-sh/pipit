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
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
)

const zeroCountBankSource = `import "probe"

func leaf() {
	probe.Check()
}

func work(n int, f float64) int {
	a, b, c := n, n*2, n*3
	leaf()
	return a + b + c + int(f)
}

func nest(n int) int {
	if n == 0 {
		return work(n, 1.5)
	}
	return nest(n-1) + work(n, float64(n))
}

total := 0
for i := 0; i < 64; i++ {
	total += nest(i % 8)
}
total
`

func TestZeroCountRegisterBanksCarryNoPointer(t *testing.T) {
	for _, forceGoDispatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("forceGoDispatch=%t", forceGoDispatch), func(t *testing.T) {
			var vm *engine.VM
			var violations []string
			checks := 0
			check := func() {
				checks++
				for fp := 0; fp <= vm.FramePointer && fp < len(vm.CallStack); fp++ {
					registers := vm.CallStack[fp].Registers
					if cap(registers.Ints) == 0 && unsafe.SliceData(registers.Ints) != nil {
						violations = append(violations, fmt.Sprintf("frame %d: empty int bank at %p", fp, unsafe.SliceData(registers.Ints)))
					}
					if cap(registers.Floats) == 0 && unsafe.SliceData(registers.Floats) != nil {
						violations = append(violations, fmt.Sprintf("frame %d: empty float bank at %p", fp, unsafe.SliceData(registers.Floats)))
					}
				}
			}
			registry := symtab.NewSymbolRegistry(symtab.SymbolExports{"probe": {"Check": reflect.ValueOf(check)}})
			service := app.NewService()
			service.UseSymbols(registry)
			compiled, err := service.Compile(t.Context(), zeroCountBankSource)
			require.NoError(t, err)

			vm = engine.NewVM(t.Context(), engine.NewGlobalStore(), registry)
			vm.Limits.ForceGoDispatch = forceGoDispatch
			result, err := vm.Execute(compiled)
			require.NoError(t, err)
			require.EqualValues(t, zeroCountBankWant(), result)
			require.Positive(t, checks, "leaf must run")
			require.Empty(t, violations, "an empty register bank must carry a nil pointer")
		})
	}
}

func zeroCountBankWant() int {
	work := func(n int, f float64) int { return 6*n + int(f) }
	var nest func(n int) int
	nest = func(n int) int {
		if n == 0 {
			return work(n, 1.5)
		}
		return nest(n-1) + work(n, float64(n))
	}
	total := 0
	for i := range 64 {
		total += nest(i % 8)
	}
	return total
}
