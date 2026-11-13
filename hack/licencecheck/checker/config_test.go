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

package checker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedConfigParses(t *testing.T) {
	_, err := loadConfig()
	require.NoError(t, err)
}

func TestActiveConfigIsTheEmbeddedOneOutsideFixtures(t *testing.T) {
	require.Nil(t, testConfig)
	active, err := activeConfig()
	require.NoError(t, err)
	embedded, err := loadConfig()
	require.NoError(t, err)
	require.Same(t, embedded, active)
	require.NotEmpty(t, active.UnsafeAllowed, "the embedded configuration lists the reviewed unsafe functions")
}

func TestEmbeddedConfigLoadsOnce(t *testing.T) {
	first, err := loadConfig()
	require.NoError(t, err)
	second, err := loadConfig()
	require.NoError(t, err)
	require.Same(t, first, second)
}

func TestMalformedEmbeddedConfigIsAnError(t *testing.T) {
	embedded := configJSON
	t.Cleanup(func() { configJSON = embedded })
	configJSON = []byte("{")

	parsed, err := parseEmbeddedConfig()
	require.ErrorContains(t, err, "licencecheck.json")
	require.Nil(t, parsed)
}
