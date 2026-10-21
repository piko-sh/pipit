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

package compile

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

func TestAPackageVariablePastTheAddressableSlotsFailsTheCompile(t *testing.T) {
	t.Parallel()

	globals := engine.NewGlobalStore()
	for range math.MaxUint16 + 1 {
		globals.AllocInt(0)
	}
	source := "package main\n\nvar counter int\nvar label string\n\nfunc main() {\n\tcounter++\n\tlabel = \"x\"\n\t_ = counter\n}\n"

	var err error
	require.NotPanics(t, func() {
		_, err = tryCompileSnippetInto(t, source, symtab.NewSymbolRegistry(nil), globals, policy.InterpFeaturesAll, 0, 0)
	})
	require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted)
	require.ErrorContains(t, err, "counter")
}

func TestAFullGlobalBankIsNotGrownByAFailedCompile(t *testing.T) {
	t.Parallel()

	globals := engine.NewGlobalStore()
	for range math.MaxUint16 {
		globals.AllocInt(0)
	}
	source := "package main\n\nvar counter int\nvar label string\n\nfunc main() {\n\tcounter++\n\tlabel = \"x\"\n}\n"

	_, err := tryCompileSnippetInto(t, source, symtab.NewSymbolRegistry(nil), globals, policy.InterpFeaturesAll, 0, 0)
	require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted)
	require.ErrorContains(t, err, "package variable counter")
	lengths := globals.Lengths()
	require.EqualValues(t, math.MaxUint16, lengths[isa.RegisterInt64], "the full bank stays at the limit")
	require.EqualValues(t, 1, lengths[isa.RegisterString], "a bank with room still serves its variable")
}
