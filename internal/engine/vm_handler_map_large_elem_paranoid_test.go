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

//go:build !safe && pipit_arena_paranoid

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapFastPathsRefuseIndirectElementsUnderParanoid(t *testing.T) {
	t.Parallel()

	stringKeyed := reflect.ValueOf(map[string]largeElem136{"k": {}})
	intKeyed := reflect.ValueOf(map[int64]largeElem136{1: {}})

	for name, call := range map[string]func(){
		"the faststr read":   func() { mapAccessFastStrToGeneral(stringKeyed, "k") },
		"the faststr assign": func() { mapAssignFastStr(stringKeyed, "k") },
		"the fast64 read":    func() { mapAccessFast64ToGeneral(intKeyed, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				require.NotNil(t, recovered, "an indirect-element map must be refused")
				err, ok := recovered.(error)
				require.True(t, ok)
				require.Contains(t, err.Error(), "stored indirectly")
			}()
			call()
		})
	}
}
