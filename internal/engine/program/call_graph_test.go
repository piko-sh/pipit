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

package program

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestCalleeUsesScalarBanksOnlyRefusesGeneralAndTypedSliceBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parameters []isa.RegisterKind
		results    []isa.RegisterKind
		want       bool
	}{
		{name: "no parameters and no results", want: true},
		{name: "scalar parameters and results", parameters: []isa.RegisterKind{isa.RegisterInt64, isa.RegisterFloat64}, results: []isa.RegisterKind{isa.RegisterBool, isa.RegisterString}, want: true},
		{name: "a general parameter", parameters: []isa.RegisterKind{isa.RegisterInt64, isa.RegisterGeneral}},
		{name: "a typed slice parameter", parameters: []isa.RegisterKind{isa.RegisterSliceByte}},
		{name: "a general result", parameters: []isa.RegisterKind{isa.RegisterInt64}, results: []isa.RegisterKind{isa.RegisterGeneral}},
		{name: "a typed slice result", results: []isa.RegisterKind{isa.RegisterUint64, isa.RegisterSliceInt64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := &CompiledFunction{ParameterKinds: tt.parameters, ResultKinds: tt.results}

			require.Equal(t, tt.want, CalleeUsesScalarBanksOnly(callee))
		})
	}
}

func TestDirectCallSubOpcodeTakesTheScalarCallOnlyWhereItFits(t *testing.T) {
	t.Parallel()

	scalar := func() *CompiledFunction {
		return &CompiledFunction{ParameterKinds: []isa.RegisterKind{isa.RegisterInt64}, ResultKinds: []isa.RegisterKind{isa.RegisterInt64}}
	}
	variadic := scalar()
	variadic.IsVariadic = true
	general := scalar()
	general.ResultKinds = []isa.RegisterKind{isa.RegisterGeneral}
	tests := []struct {
		name   string
		callee *CompiledFunction
		site   CallSite
		want   isa.SubOpcode
	}{
		{name: "a callee that deals only in scalars", callee: scalar(), want: isa.SubOpCallScalar},
		{name: "a callee that is not known", callee: nil, want: isa.SubOpCall},
		{name: "a variadic callee", callee: variadic, want: isa.SubOpCall},
		{name: "a callee with a general result", callee: general, want: isa.SubOpCall},
		{name: "a site that instantiates a generic function", callee: scalar(), site: CallSite{LinkedTypeArgs: []reflect.Type{reflect.TypeFor[int]()}}, want: isa.SubOpCall},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, DirectCallSubOpcode(&tt.site, tt.callee))
		})
	}
}
