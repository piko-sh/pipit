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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
)

type shadowProbe struct {
	Count int
	Name  string
}

func TestMarshalShadowMemoLivesOnTheProgramRoot(t *testing.T) {
	vm := NewVM(context.Background(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	root := program.NewNamedFunction("root")
	vm.rootFunction = root
	value := reflect.ValueOf(shadowProbe{Count: 1, Name: "probe"})

	require.Equal(t, value.Interface(), shadowForMarshal(vm, value).Interface())

	cached, ok := root.MarshalShadowMemo.Load(value.Type())
	require.True(t, ok, "the answer must be memoised on the root that asked")
	require.Equal(t, false, cached)

	other := program.NewNamedFunction("other")
	_, leaked := other.MarshalShadowMemo.Load(value.Type())
	require.False(t, leaked, "a different program must not see the memo")
}

func TestMarshalShadowWithoutRootIsUncached(t *testing.T) {
	vm := NewVM(context.Background(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	walk := marshalShadowWalk{vm: vm, budget: marshalShadowLeafCap}
	require.False(t, walk.typeNeedsShadow(reflect.TypeFor[shadowProbe]()))
}
