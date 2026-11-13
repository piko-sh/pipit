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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReflectValueLayoutProbesHoldInEveryBuild(t *testing.T) {
	t.Parallel()

	probes := map[string]func() error{
		"the whole layout":             verifyReflectValueLayout,
		"the start-up check":           verifyUnsafeRuntimeLayout,
		"the size of a Value":          verifyReflectValueSize,
		"an addressable Value":         verifyAddressableValueLayout,
		"a Value that is not":          verifyNonAddressableValueLayout,
		"the read-only flags":          verifyReadOnlyFlagLayout,
		"a slice Value and its header": verifySliceValueLayout,
	}
	for name, probe := range probes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, probe())
		})
	}
}
