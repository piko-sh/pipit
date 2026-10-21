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
	"bytes"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadsOnlyTableAgreesWithTheRegistries(t *testing.T) {
	require.Empty(t, ReadsOnlyDisagreements())
	require.NotEmpty(t, ReadsOnlyStdlibFunctions())
	require.Equal(t, "readsOnly", BorrowReadsOnly.String())
	require.Equal(t, handOffNativeArgBorrows, BorrowReadsOnly.handOffCause())
}

func TestReadsOnlyNeedsALicence(t *testing.T) {
	bytesEqual := reflect.ValueOf(func(a, b []byte) bool { return string(a) == string(b) })
	arena := &RegisterArena{licensed: true}
	declarations := NewBorrowDeclarations()
	require.NoError(t, declarations.Declare(bytesEqual.Interface(), BorrowReadsOnly))
	declarations.Freeze()
	arena.borrowDeclarations = declarations
	require.True(t, arena.nativeReadsOnly(bytesEqual))
	arena.licensed = false
	require.False(t, arena.nativeReadsOnly(bytesEqual), "not licensed")
}

func TestReadsOnlyRefusalChecksTheCandidate(t *testing.T) {
	require.Equal(t, "not a function", ReadsOnlyRefusal(3))
	require.Equal(t, "not a function", ReadsOnlyRefusal((func([]byte))(nil)))
	require.Empty(t, ReadsOnlyRefusal(func(data []byte) int { return len(data) }), "a function that only reads its bytes is accepted")
}

func TestReadsOnlyStdlibFunctionsAreFoundByName(t *testing.T) {
	arena := &RegisterArena{licensed: true}
	require.True(t, arena.nativeReadsOnly(reflect.ValueOf(bytes.Equal)), "bytes.Equal is in the reads-only table")
	require.True(t, arena.nativeReadsOnly(reflect.ValueOf(bytes.Equal)), "the cached answer agrees")
	require.False(t, arena.nativeReadsOnly(reflect.ValueOf(bytes.Clone)), "bytes.Clone is not")
	require.False(t, arena.nativeReadsOnly(reflect.ValueOf(3)), "not a function")

	arena.borrowDeclarations = NewBorrowDeclarations()
	require.True(t, arena.nativeReadsOnly(reflect.ValueOf(bytes.Equal)), "an undeclared function keeps the table's class")
}

func TestVerifyReadsOnlyChecksDemotesANativeThatWrote(t *testing.T) {
	t.Cleanup(ResetReadsOnlyAuditForTest)
	ResetReadsOnlyAuditForTest()
	honest := reflect.ValueOf(func(data []byte) int { return len(data) })
	copier := reflect.ValueOf(func(data []byte) int { data[0] = 'x'; return len(data) })
	viewer := reflect.ValueOf(func(data []byte) int { data[1] = 'z'; return len(data) })

	view := []byte("bytes")
	sum := checksumBytes(view)
	view[1] = 'z'
	verifyReadsOnlyChecks([]readsOnlyCheck{
		{function: honest, original: "bytes", copied: []byte("bytes")},
		{function: copier, original: "bytes", copied: []byte("xytes")},
		{function: viewer, view: view, sum: sum},
	})

	violations := ReadsOnlyViolations()
	require.Len(t, violations, 2, "the copy that changed and the bytes whose checksum changed")
	for _, violation := range violations {
		require.Contains(t, violation, "wrote a reads-only []byte argument")
	}
	_, attested := readsOnlyAttested.load(honest.Pointer())
	require.True(t, attested, "a copy that still matches attests its native")
	arena := &RegisterArena{licensed: true, borrowDeclarations: NewBorrowDeclarations()}
	require.NoError(t, arena.borrowDeclarations.Declare(copier.Interface(), BorrowReadsOnly))
	arena.borrowDeclarations.Freeze()
	require.False(t, arena.nativeReadsOnly(copier), "a native seen writing loses its reads-only class")

	ResetReadsOnlyAuditForTest()
	require.Empty(t, ReadsOnlyViolations())
	require.True(t, arena.nativeReadsOnly(copier), "the reset forgets the demotion")
}

func TestReadsOnlyChecksAreTakenOnce(t *testing.T) {
	t.Cleanup(ResetReadsOnlyAuditForTest)
	vm := newTestVM(t)
	checks := []readsOnlyCheck{{original: "kept"}}
	readsOnlyPending.store(vm, checks)
	if arenaAuditEnabled {
		require.Equal(t, checks, takeAuditedReadsOnlyChecks(vm), "the audit build verifies the queued checks")
	} else {
		require.Nil(t, takeAuditedReadsOnlyChecks(vm), "only the audit build queues checks")
		require.Equal(t, checks, takeReadsOnlyChecks(vm))
	}
	require.Nil(t, takeReadsOnlyChecks(vm), "taken checks are gone")
}

func TestChecksumBytesIsFNV1a(t *testing.T) {
	require.Equal(t, uint64(0xcbf29ce484222325), checksumBytes(nil), "the offset basis for no bytes")
	require.NotEqual(t, checksumBytes([]byte("a")), checksumBytes([]byte("b")))
}
