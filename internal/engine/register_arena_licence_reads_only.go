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
	"fmt"
	"hash/fnv"
	"reflect"
	"runtime"
	"slices"
)

const (
	// globalWatchRangeLimit bounds the storage ranges one watch records; a larger image
	// makes the watch incomplete, which keeps it dirty.
	globalWatchRangeLimit = 4096

	// globalWatchDepth bounds how deep the watch follows references from a global, and how
	// deep the native-argument check follows an argument.
	globalWatchDepth = 6

	// globalWatchElementLimit bounds the elements one container contributes to a walk.
	globalWatchElementLimit = 512
)

// readsOnlyStdlibFunctions is the hand-reviewed reads-only class.
//
// Each reads its []byte arguments only, keeps no reference to them, returns no alias of
// them, and has no parameter through which it could call guest code
// (TestReadsOnlyStdlibFunctionsAreChecked).
var readsOnlyStdlibFunctions = map[string]bool{
	"bytes.Equal":                 true,
	"bytes.Compare":               true,
	"bytes.Contains":              true,
	"bytes.ContainsAny":           true,
	"bytes.ContainsRune":          true,
	"bytes.Count":                 true,
	"bytes.EqualFold":             true,
	"bytes.HasPrefix":             true,
	"bytes.HasSuffix":             true,
	"bytes.Index":                 true,
	"bytes.IndexAny":              true,
	"bytes.IndexByte":             true,
	"bytes.IndexRune":             true,
	"bytes.LastIndex":             true,
	"bytes.LastIndexByte":         true,
	"unicode/utf8.Valid":          true,
	"unicode/utf8.RuneCount":      true,
	"unicode/utf8.FullRune":       true,
	"unicode/utf8.DecodeRune":     true,
	"unicode/utf8.DecodeLastRune": true,
	"crypto/sha256.Sum256":        true,
	"crypto/sha256.Sum224":        true,
	"crypto/sha1.Sum":             true,
	"crypto/sha512.Sum512":        true,
	"crypto/md5.Sum":              true,
	"hash/crc32.ChecksumIEEE":     true,
	"hash/adler32.Checksum":       true,
	"encoding/hex.EncodeToString": true,
	"encoding/json.Valid":         true,
}

// readsOnlyByPointer caches the built-in reads-only classification by code pointer.
var readsOnlyByPointer typedCache[uintptr, bool]

// readsOnlyCheck is one reads-only argument the audit build verifies after the call.
type readsOnlyCheck struct {
	// function is the native callee.
	function reflect.Value

	// original is the string the bytes were converted from.
	original string

	// view is the bytes passed, and copied the copy passed instead on a first call.
	view, copied []byte

	// sum is the checksum of view before the call.
	sum uint64
}

// ReadsOnlyStdlibFunctions lists the built-in reads-only natives.
//
// Returns []string which is sorted.
func ReadsOnlyStdlibFunctions() []string {
	names := make([]string, 0, len(readsOnlyStdlibFunctions))
	for name := range readsOnlyStdlibFunctions {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ReadsOnlyDisagreements lists the built-in reads-only natives that gc's escape tags or
// the alias-aware registry class less conservatively than borrows, as GcTagDisagreements
// does for borrow classes.
//
// Returns []string which is sorted; empty when the table agrees.
func ReadsOnlyDisagreements() []string {
	var disagreements []string
	for name := range readsOnlyStdlibFunctions {
		if gcClass, tagged := gcEscapeTagClasses[name]; tagged && gcClass != BorrowBorrows {
			disagreements = append(disagreements, fmt.Sprintf("%s: readsOnly, gc %s", name, gcClass))
		}
		if class, listed := aliasAwareStdlibClasses[name]; listed && class != BorrowBorrows {
			disagreements = append(disagreements, fmt.Sprintf("%s: readsOnly, alias-aware %s", name, class))
		}
	}
	slices.Sort(disagreements)
	return disagreements
}

// ReadsOnlyRefusal applies the host declaration checks for the reads-only class to a
// built-in or candidate function, as a declaration would.
//
// Takes function (any) which is the native function.
//
// Returns string which is the reason it could not be reads-only, or "".
func ReadsOnlyRefusal(function any) string {
	value := reflect.ValueOf(function)
	if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
		return "not a function"
	}
	return borrowDeclarationRefusal(value, borrowDeclarationName(value), BorrowReadsOnly)
}

// ReadsOnlyViolations lists the reads-only natives the audit build saw writing an
// argument.
//
// Returns []string which is a copy.
//
// Concurrency: Copies the shared violation list while holding readsOnlyViolationsMu.
func ReadsOnlyViolations() []string {
	readsOnlyViolationsMu.Lock()
	defer readsOnlyViolationsMu.Unlock()
	return slices.Clone(readsOnlyViolationList)
}

// nativeReadsOnly reports whether the licence treats function as reads-only for this
// arena.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns bool which is true when its []byte(s) arguments may be the string's bytes.
func (a *RegisterArena) nativeReadsOnly(function reflect.Value) bool {
	if a == nil || !a.licensed || !function.IsValid() || function.Kind() != reflect.Func || function.IsNil() {
		return false
	}
	readsOnly := a.nativeReadsOnlyByClass(function)
	return readsOnly
}

// nativeReadsOnlyByClass is nativeReadsOnly's classification of a valid function value.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns bool which is true for a reads-only native never seen writing.
func (a *RegisterArena) nativeReadsOnlyByClass(function reflect.Value) bool {
	pointer := function.Pointer()
	declarations := a.borrowDeclarations != nil

	cache := &readsOnlyFast
	if declarations {
		cache = nil
		if a.borrowDeclarations.frozen.Load() {
			cache = &a.borrowDeclarations.fast
		}
	}
	if cache != nil && readsOnlyDemotions.Load() == 0 {
		if class, ok := cache.load(pointer); ok {
			return class == 1
		}
	}
	readsOnly := a.nativeReadsOnlySlow(function, pointer, declarations)
	if cache != nil && readsOnlyDemotions.Load() == 0 {
		class := uint8(0)
		if readsOnly {
			class = 1
		}
		cache.store(pointer, class)
	}
	return readsOnly
}

// nativeReadsOnlySlow is nativeReadsOnlyByClass without its cache.
//
// Takes function (reflect.Value) which is the native callee.
// Takes pointer (uintptr) which is its code pointer.
// Takes declarations (bool) which is true when the host's borrow declarations apply.
//
// Returns bool which is true for a reads-only native never seen writing.
func (a *RegisterArena) nativeReadsOnlySlow(function reflect.Value, pointer uintptr, declarations bool) bool {
	if _, dishonest := readsOnlyDishonest.load(pointer); dishonest {
		return false
	}
	if declarations {
		if declared, ok := a.borrowDeclarations.lookup(function); ok {
			return declared == BorrowReadsOnly
		}
	}
	if readsOnly, ok := readsOnlyByPointer.load(pointer); ok {
		return readsOnly
	}
	readsOnly := false
	if info := runtime.FuncForPC(pointer); info != nil {
		readsOnly = readsOnlyStdlibFunctions[info.Name()]
	}
	readsOnlyByPointer.store(pointer, readsOnly)
	return readsOnly
}

// takeReadsOnlyChecks removes and returns vm's queued checks.
//
// Takes vm (*VM) which is about to make a native call.
//
// Returns []readsOnlyCheck which is nil when none is queued.
func takeReadsOnlyChecks(vm *VM) []readsOnlyCheck {
	checks, _ := readsOnlyPending.loadAndDelete(vm)
	return checks
}

// verifyReadsOnlyChecks runs after the call.
//
// A copy that changed, or bytes whose checksum changed, shows the native writing an
// argument; the native is reported and demoted.
//
// Takes checks ([]readsOnlyCheck) which the conversions queued.
//
// Concurrency: Protects the shared violation list with readsOnlyViolationsMu; the caller
// must own the VM's pending checks.
func verifyReadsOnlyChecks(checks []readsOnlyCheck) {
	for _, check := range checks {
		if !check.wrote() {
			continue
		}
		pointer := check.function.Pointer()
		readsOnlyDemotions.Add(1)
		readsOnlyDishonest.store(pointer, true)
		readsOnlyAttested.delete(pointer)
		if licenceCounting {
			hostDeclarationCounters.violations.Add(1)
		}
		readsOnlyViolationsMu.Lock()
		readsOnlyViolationList = append(readsOnlyViolationList, borrowDeclarationName(check.function)+": wrote a reads-only []byte argument")
		readsOnlyViolationsMu.Unlock()
	}
}

// wrote reports whether the native wrote the checked argument: its copy no longer matches
// the original, or its bytes' checksum changed. A copy that still matches attests the
// native, once.
//
// Returns bool which is true when the native wrote the argument.
func (check *readsOnlyCheck) wrote() bool {
	if check.copied == nil {
		return checksumBytes(check.view) != check.sum
	}
	if string(check.copied) != check.original {
		return true
	}
	if _, loaded := readsOnlyAttested.loadOrStore(check.function.Pointer(), true); !loaded && licenceCounting {
		hostDeclarationCounters.attestations.Add(1)
	}
	return false
}

// ResetReadsOnlyAuditForTest forgets the audit build's attestations and demotions.
//
// Concurrency: Protects the violation list with readsOnlyViolationsMu; tests must stop
// audited calls before resetting the other shared audit tables.
func ResetReadsOnlyAuditForTest() {
	readsOnlyAttested.clear()
	readsOnlyDishonest.clear()
	readsOnlyFast.clear()
	readsOnlyDemotions.Store(0)
	readsOnlyPending.clear()
	readsOnlyViolationsMu.Lock()
	readsOnlyViolationList = nil
	readsOnlyViolationsMu.Unlock()
}

// checksumBytes is FNV-1a over b.
//
// Takes b ([]byte) which is read.
//
// Returns uint64 which is the checksum.
func checksumBytes(b []byte) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write(b)
	return hash.Sum64()
}

// takeAuditedReadsOnlyChecks is takeReadsOnlyChecks in the reuse audit build, which
// queues them, and nil otherwise.
//
// Takes vm (*VM) which makes the next native call.
//
// Returns []readsOnlyCheck which holds the checks to verify after the call, or nil.
func takeAuditedReadsOnlyChecks(vm *VM) []readsOnlyCheck {
	if !arenaAuditEnabled {
		return nil
	}
	return takeReadsOnlyChecks(vm)
}
