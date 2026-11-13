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
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func declaredBorrower(s string, xs []string) int { return len(s) + len(xs) }

func declaredCallbackTaker(xs []string, f func(string)) {}

func declaredAliaser(b []byte) []byte { return b }

func declaredReader(b []byte) int { return len(b) }

func TestBorrowDeclarationChecks(t *testing.T) {
	declarations := NewBorrowDeclarations()
	require.NoError(t, declarations.Declare(declaredBorrower, BorrowBorrows))
	require.NoError(t, declarations.Declare(declaredReader, BorrowReadsOnly))
	require.NoError(t, declarations.Declare(declaredCallbackTaker, BorrowRetains), "a revoking class needs no check")
	for _, refused := range []struct {
		function any
		class    BorrowClass
		reason   string
	}{
		{errors.New, BorrowBorrows, "gc escape tags"},
		{declaredCallbackTaker, BorrowBorrows, "can carry guest code"},
		{declaredAliaser, BorrowReadsOnly, "may alias"},
		{(&strings.Builder{}).Len, BorrowBorrows, "method value"},
		{42, BorrowBorrows, "not a function"},
	} {
		err := declarations.Declare(refused.function, refused.class)
		require.ErrorIs(t, err, ErrBorrowDeclarationRefused)
		require.Contains(t, err.Error(), refused.reason)
	}
	_, found := declarations.lookup(reflect.ValueOf(declaredBorrower))
	require.False(t, found, "no declaration is visible before the freeze")
	declarations.Freeze()
	class, found := declarations.lookup(reflect.ValueOf(declaredBorrower))
	require.True(t, found)
	require.Equal(t, BorrowBorrows, class)
	_, found = declarations.lookup(reflect.ValueOf(errors.New))
	require.False(t, found, "a refused declaration leaves the registry's class")
	require.ErrorIs(t, declarations.Declare(declaredReader, BorrowBorrows), ErrBorrowDeclarationsFrozen)
	require.Len(t, declarations.Refusals(), 6)
}

func TestNamesAMethodReadsTheQualifiedName(t *testing.T) {
	cases := map[string]bool{
		"bytes.(*Buffer).Write":                true,
		"example.com/pkg.T.Method":             true,
		"example.com/pkg.Function":             false,
		"example.com/pkg.Function.func1":       false,
		"example.com/pkg.Function.deferwrap1":  false,
		"example.com/pkg.Function.gowrap":      false,
		"example.com/pkg.Function.gowrap1":     false,
		"example.com/pkg.Function.deferwrap":   false,
		"example.com/nested/pkg.(*T).M":        true,
		"example.com/nested/pkg.Value.Release": true,
	}
	for name, want := range cases {
		require.Equal(t, want, namesAMethod(name), name)
	}
}

func TestTypeMayCarryGuestCodeFollowsContainers(t *testing.T) {
	type cyclic struct{ next *cyclic }
	type holder struct {
		n        int
		callback func()
	}
	cases := []struct {
		valueType reflect.Type
		name      string
		want      bool
	}{
		{name: "a function", valueType: reflect.TypeFor[func()](), want: true},
		{name: "an interface", valueType: reflect.TypeFor[any](), want: true},
		{name: "an unsafe pointer", valueType: reflect.TypeFor[unsafe.Pointer](), want: true},
		{name: "a slice of functions", valueType: reflect.TypeFor[[]func()](), want: true},
		{name: "a map with interface values", valueType: reflect.TypeFor[map[string]any](), want: true},
		{name: "a map of scalars", valueType: reflect.TypeFor[map[string]int](), want: false},
		{name: "a struct holding a function", valueType: reflect.TypeFor[holder](), want: true},
		{name: "a cyclic struct of scalars", valueType: reflect.TypeFor[cyclic](), want: false},
		{name: "a channel of bytes", valueType: reflect.TypeFor[chan []byte](), want: false},
		{name: "a string", valueType: reflect.TypeFor[string](), want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, typeMayCarryGuestCode(testCase.valueType, map[reflect.Type]bool{}))
		})
	}
}

func TestBorrowDeclarationsOfANilSetAreEmpty(t *testing.T) {
	var declarations *BorrowDeclarations
	declarations.Freeze()
	require.Nil(t, declarations.Refusals())
	_, ok := declarations.lookup(reflect.ValueOf(strings.Contains))
	require.False(t, ok)

	set := NewBorrowDeclarations()
	set.Freeze()
	set.Freeze()
	_, ok = set.lookup(reflect.ValueOf(strings.Contains))
	require.False(t, ok, "a frozen set with no declarations")
	require.Equal(t, "<invalid reflect.Value>", borrowDeclarationName(reflect.Value{}))
}
