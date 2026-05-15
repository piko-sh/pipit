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

package passes

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestCompositeZeroReuseRefusesWithoutEscapeVerdict(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{mk(isa.OpLoadGeneralConst, 0, 0, 0)}
	compiledFunction := &program.CompiledFunction{
		Body:             body,
		GeneralConstants: []reflect.Value{reflect.New(reflect.TypeFor[[4]int]()).Elem()},
		AliasInfo:        &program.PointerAliasInfo{},
	}
	PromoteCompositeZeroReuse(compiledFunction)
	require.Equal(t, isa.OpLoadGeneralConst, compiledFunction.Body[0].Op,
		"no ArenaSafeAllocPCs entry means the escape pass did not prove the storage frame-local, "+
			"so the load must keep its fresh-storage form")
}

func TestCompositeZeroReuseRefusesWithoutAliasInfo(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{mk(isa.OpLoadGeneralConst, 0, 0, 0)}
	compiledFunction := &program.CompiledFunction{
		Body:              body,
		GeneralConstants:  []reflect.Value{reflect.New(reflect.TypeFor[[4]int]()).Elem()},
		ArenaSafeAllocPCs: map[int]bool{0: true},
	}
	PromoteCompositeZeroReuse(compiledFunction)
	require.Equal(t, isa.OpLoadGeneralConst, compiledFunction.Body[0].Op,
		"a nil AliasInfo proves nothing, so no site may be promoted")
}

func TestCompositeZeroReuseRefusesNonCompositeConstants(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{mk(isa.OpLoadGeneralConst, 0, 0, 0)}
	compiledFunction := &program.CompiledFunction{
		Body:              body,
		GeneralConstants:  []reflect.Value{reflect.ValueOf(42)},
		ArenaSafeAllocPCs: map[int]bool{0: true},
		AliasInfo:         &program.PointerAliasInfo{},
	}
	PromoteCompositeZeroReuse(compiledFunction)
	require.Equal(t, isa.OpLoadGeneralConst, compiledFunction.Body[0].Op,
		"only struct and array zeroes pay the per-execution copy the reuse form avoids")
}

func TestCompositeZeroReuseRefusesNonZeroComposite(t *testing.T) {
	t.Parallel()
	populated := reflect.New(reflect.TypeFor[[4]int]()).Elem()
	populated.Index(0).SetInt(7)
	body := []isa.Instruction{mk(isa.OpLoadGeneralConst, 0, 0, 0)}
	compiledFunction := &program.CompiledFunction{
		Body:              body,
		GeneralConstants:  []reflect.Value{populated},
		ArenaSafeAllocPCs: map[int]bool{0: true},
		AliasInfo:         &program.PointerAliasInfo{},
	}
	PromoteCompositeZeroReuse(compiledFunction)
	require.Equal(t, isa.OpLoadGeneralConst, compiledFunction.Body[0].Op,
		"reuse zeroes the storage, so it must never stand in for a populated constant")
}
