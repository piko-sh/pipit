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
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestConfigureASMReturnAdmitsSingleGeneralResult(t *testing.T) {
	t.Parallel()
	general := program.VarLocation{Kind: isa.RegisterGeneral, Register: 4}
	integer := program.VarLocation{Kind: isa.RegisterInt, Register: 2}
	tests := []struct {
		name        string
		returns     []program.VarLocation
		resultKinds []isa.RegisterKind
		wantOK      bool
		wantKind    int64
		wantReg     int64
	}{
		{
			name:        "single general result into a general destination is admitted",
			returns:     []program.VarLocation{general},
			resultKinds: []isa.RegisterKind{isa.RegisterGeneral},
			wantOK:      true,
			wantKind:    int64(isa.RegisterGeneral),
			wantReg:     4,
		},
		{
			name:        "general result into a typed destination is refused",
			returns:     []program.VarLocation{integer},
			resultKinds: []isa.RegisterKind{isa.RegisterGeneral},
			wantOK:      false,
		},
		{
			name:        "two results with a general one are refused",
			returns:     []program.VarLocation{integer, general},
			resultKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral},
			wantOK:      false,
		},
		{
			name:        "two scalar results stay admitted",
			returns:     []program.VarLocation{integer, {Kind: isa.RegisterInt, Register: 3}},
			resultKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
			wantOK:      true,
			wantKind:    int64(isa.RegisterInt),
			wantReg:     2,
		},
		{
			name:        "general result into an upvalue destination is refused",
			returns:     []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 1, IsUpvalue: true}},
			resultKinds: []isa.RegisterKind{isa.RegisterGeneral},
			wantOK:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var info AsmCallInfo
			site := &program.CallSite{Returns: tt.returns}
			callee := &program.CompiledFunction{ResultKinds: tt.resultKinds}
			require.Equal(t, tt.wantOK, ConfigureASMReturn(&info, site, callee))
			if !tt.wantOK {
				return
			}
			require.Equal(t, int64(len(tt.returns)), info.ReturnCount)
			require.Equal(t, tt.wantKind, info.returnDestinationKind)
			require.Equal(t, tt.wantReg, info.returnDestinationRegister)
		})
	}
}

func TestAsmInlineReturnKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   isa.RegisterKind
		single bool
		want   bool
	}{
		{name: "int single", kind: isa.RegisterInt, single: true, want: true},
		{name: "string among several", kind: isa.RegisterString, single: false, want: true},
		{name: "general single", kind: isa.RegisterGeneral, single: true, want: true},
		{name: "general among several", kind: isa.RegisterGeneral, single: false, want: false},
		{name: "complex", kind: isa.RegisterComplex, single: true, want: false},
		{name: "byte slice", kind: isa.RegisterSliceByte, single: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, asmInlineReturnKind(tt.kind, tt.single))
		})
	}
}
