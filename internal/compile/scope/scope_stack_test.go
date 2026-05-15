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

package scope

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestDeclareAndLookupFindVariablesInTheCurrentScope(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()

	declared := stack.DeclareVar("counter", isa.RegisterInt)

	found, ok := stack.LookupVar("counter")
	require.True(t, ok)
	require.Equal(t, declared.Register, found.Register)
	require.Equal(t, isa.RegisterInt, found.Kind)
}

func TestLookupFallsThroughToAnEnclosingScope(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()
	outer := stack.DeclareVar("outer", isa.RegisterInt)
	stack.PushScope()

	found, ok := stack.LookupVar("outer")

	require.True(t, ok, "an inner scope can see the names an outer scope declared")
	require.Equal(t, outer.Register, found.Register)
}

func TestPoppingAScopeHidesItsDeclarations(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()
	stack.PushScope()
	stack.DeclareVar("inner", isa.RegisterInt)
	stack.PopScope()

	_, ok := stack.LookupVar("inner")

	require.False(t, ok, "a name declared in a popped scope is gone")
}

func TestAnInnerDeclarationShadowsTheOuterOne(t *testing.T) {
	t.Parallel()

	t.Run("a shadow in the same bank takes a fresh register", func(t *testing.T) {
		t.Parallel()
		stack := NewScopeStack("f")
		stack.PushScope()
		outer := stack.DeclareVar("name", isa.RegisterInt)
		stack.PushScope()
		inner := stack.DeclareVar("name", isa.RegisterInt)

		require.NotEqual(t, outer.Register, inner.Register,
			"the shadowing declaration must not alias the register it hides")

		found, ok := stack.LookupVar("name")
		require.True(t, ok)
		require.Equal(t, inner.Register, found.Register, "the innermost declaration wins")

		stack.PopScope()
		restored, ok := stack.LookupVar("name")
		require.True(t, ok)
		require.Equal(t, outer.Register, restored.Register,
			"popping the inner scope restores the shadowed name")
	})

	t.Run("a shadow in another bank changes the kind", func(t *testing.T) {
		t.Parallel()
		stack := NewScopeStack("f")
		stack.PushScope()
		stack.DeclareVar("name", isa.RegisterInt)
		stack.PushScope()
		stack.DeclareVar("name", isa.RegisterString)

		found, ok := stack.LookupVar("name")
		require.True(t, ok)
		require.Equal(t, isa.RegisterString, found.Kind, "the innermost declaration wins")

		stack.PopScope()
		restored, ok := stack.LookupVar("name")
		require.True(t, ok)
		require.Equal(t, isa.RegisterInt, restored.Kind,
			"popping the inner scope restores the shadowed name")
	})
}

func TestDeclarationsAdvanceTheRegisterWatermarkPerBank(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()

	stack.DeclareVar("a", isa.RegisterInt)
	stack.DeclareVar("b", isa.RegisterInt)
	stack.DeclareVar("s", isa.RegisterString)

	peak := stack.PeakRegisters()

	require.Equal(t, uint32(2), peak[isa.RegisterInt])
	require.Equal(t, uint32(1), peak[isa.RegisterString])
	require.Equal(t, uint32(0), peak[isa.RegisterFloat], "an untouched bank needs no registers")
}

func TestLookupOfAnUndeclaredNameFails(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()

	_, ok := stack.LookupVar("absent")

	require.False(t, ok)
}

func TestUpdateVarRewritesADeclaredLocation(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()
	declared := stack.DeclareVar("v", isa.RegisterInt)
	declared.IsCaptured = true

	require.True(t, stack.UpdateVar("v", declared))

	found, ok := stack.LookupVar("v")
	require.True(t, ok)
	require.True(t, found.IsCaptured)
}

func TestUpdateVarRefusesAnUndeclaredName(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()

	require.False(t, stack.UpdateVar("absent", program.VarLocation{}),
		"updating a name that was never declared must be refused")
}

func TestMarkCapturedFlagsTheDeclaration(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()
	stack.DeclareVar("v", isa.RegisterInt)

	stack.MarkCaptured("v")

	found, ok := stack.LookupVar("v")
	require.True(t, ok)
	require.True(t, found.IsCaptured, "capture marks the variable for closure promotion")
}

func TestRestoreWatermarkRewindsTheAllocator(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("f")
	stack.PushScope()
	stack.DeclareVar("a", isa.RegisterInt)
	saved := stack.PeakRegisters()

	stack.PushScope()
	stack.DeclareVar("b", isa.RegisterInt)
	stack.DeclareVar("c", isa.RegisterInt)
	stack.PopScope()
	stack.RestoreWatermark(saved)

	next := stack.DeclareVar("d", isa.RegisterInt)

	require.Equal(t, uint8(1), next.Register,
		"restoring the watermark lets a later declaration reuse the registers the inner scope freed")
}
