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

package sandboxbroker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagingIdentity(t *testing.T) {
	t.Parallel()
	namespace, err := newStagingNamespace()
	require.NoError(t, err)
	other, err := newStagingNamespace()
	require.NoError(t, err, "namespace generation failed")
	require.NotEqual(t, other, namespace)
	first, err := StagingName(namespace, 1)
	require.NoError(t, err)
	second, err := StagingName(namespace, 2)
	require.NoError(t, err, "operation identities collided")
	require.NotEqual(t, second, first)
	again, err := StagingName(namespace, 1)
	require.NoError(t, err, "staging identity not reproducible")
	require.Equal(t, again, first)
	require.LessOrEqual(t, len(first), maximumNameBytes, "unbounded staging basename")
	require.NotContains(t, first, "/")
	for _, name := range []string{first, strings.ToUpper(first), "directory/" + first, first + "/file"} {
		require.False(t, validRelativePath(name, false), "script can address private staging name: %v", name)
	}
	for _, invalid := range []string{"", namespace[:len(namespace)-1], strings.Repeat("A", 32), strings.Repeat("z", 32), "../" + namespace} {
		_, err := StagingName(invalid, 1)
		require.ErrorIs(t, err, ErrInvalidPolicy, "invalid namespace accepted: %v", invalid)
	}
	_, err = StagingName(namespace, 0)
	require.ErrorIs(t, err, ErrInvalidPolicy, "zero identity accepted")
}
