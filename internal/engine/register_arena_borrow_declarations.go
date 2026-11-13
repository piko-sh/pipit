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
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// methodNameMinSegments is the fewest dot-separated segments a qualified method name has
// after its package path: the package, the receiver type and the method.
const methodNameMinSegments = 3

var (
	// ErrBorrowDeclarationsFrozen is returned for a declaration made after the first
	// evaluation.
	ErrBorrowDeclarationsFrozen = errors.New("engine: borrow declarations are frozen by the first evaluation")

	// ErrBorrowDeclarationRefused is returned for a declaration the checks refuse; the
	// function keeps the registry's class.
	ErrBorrowDeclarationRefused = errors.New("engine: borrow declaration refused")

	// hostDeclarationCounters counts what host borrow declarations, global reuse and
	// reads-only natives did, for tests and diagnostics.
	hostDeclarationCounters struct {
		declared, refused, dirtyByNative, views, viewFallbacks, attestations, violations,
		checksums atomic.Int64
	}

	// readsOnlyAttested holds the code pointers of reads-only natives whose first call
	// passed the audit build's copy check.
	readsOnlyAttested typedCache[uintptr, bool]

	// readsOnlyDishonest holds the code pointers of reads-only natives seen writing an
	// argument; they take the copying path from then on.
	readsOnlyDishonest typedCache[uintptr, bool]

	// readsOnlyViolationsMu guards readsOnlyViolationList.
	readsOnlyViolationsMu sync.Mutex

	// readsOnlyViolationList records each native seen writing a reads-only argument.
	readsOnlyViolationList []string

	// readsOnlyPending holds, per VM, the reads-only checks the audit build verifies once
	// the call returns.
	readsOnlyPending typedCache[*VM, []readsOnlyCheck]

	// globalReuseWatches maps a GlobalStore to its active watch.
	globalReuseWatches typedCache[*GlobalStore, *GlobalReuseWatch]

	// globalReuseWatchCount is the number of active watches, so a native call pays one
	// atomic load when there are none.
	globalReuseWatchCount atomic.Int32
)

// BorrowDeclarations holds the borrow classes a host declared for its native functions.
type BorrowDeclarations struct {
	// classes maps a function's code pointer to its declared class.
	classes atomic.Pointer[map[uintptr]BorrowClass]

	// refusals records each refused declaration, as "name: class: reason".
	refusals []string

	// mu serialises declarations and the freeze.
	mu sync.Mutex

	// frozen is set by the first evaluation.
	frozen atomic.Bool

	// fast caches the reads-only decision under these declarations once they are frozen, in
	// a lock-free direct-mapped cache.
	fast pointerClassCache
}

// NewBorrowDeclarations returns an empty, unfrozen declaration set.
//
// Returns *BorrowDeclarations which is ready to take declarations.
func NewBorrowDeclarations() *BorrowDeclarations {
	return &BorrowDeclarations{}
}

// Declare records a native function's borrow class after validating its escape tags and
// signature.
//
// Takes function (any) which is the native function the host registers.
// Takes class (BorrowClass) which is the declared class.
//
// Returns error which wraps ErrBorrowDeclarationsFrozen or ErrBorrowDeclarationRefused
// when the declaration is not recorded; the function then keeps the registry's class.
//
// Concurrency: Serialises declarations and freezing with the declaration set's mutex and
// publishes replacement maps atomically.
func (d *BorrowDeclarations) Declare(function any, class BorrowClass) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	value := reflect.ValueOf(function)
	name := borrowDeclarationName(value)
	if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
		return d.refuse(name, class, "not a function", ErrBorrowDeclarationRefused)
	}
	reason, sentinel := "", ErrBorrowDeclarationRefused
	if d.frozen.Load() {
		reason, sentinel = "frozen by the first evaluation", ErrBorrowDeclarationsFrozen
	} else {
		reason = borrowDeclarationRefusal(value, name, class)
	}
	if reason != "" {
		return d.refuse(name, class, reason, sentinel)
	}
	next := map[uintptr]BorrowClass{}
	if current := d.classes.Load(); current != nil {
		maps.Copy(next, *current)
	}
	next[value.Pointer()] = class
	d.classes.Store(&next)
	if licenceCounting {
		hostDeclarationCounters.declared.Add(1)
	}
	return nil
}

// Freeze stops further declarations; the first evaluation calls it.
//
// Concurrency: Serialises with declarations using the declaration set's mutex and
// publishes the frozen state atomically.
func (d *BorrowDeclarations) Freeze() {
	if d == nil || d.frozen.Load() {
		return
	}
	d.mu.Lock()
	d.frozen.Store(true)
	d.mu.Unlock()
}

// Refusals lists the refused declarations, as "name: class: reason".
//
// Returns []string which is a copy.
//
// Concurrency: Copies the refusal list while holding the declaration set's mutex.
func (d *BorrowDeclarations) Refusals() []string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.refusals)
}

// refuse records a refused declaration.
//
// Takes name (string) which names the function.
// Takes class (BorrowClass) which was declared.
// Takes reason (string) which says why it was refused.
// Takes sentinel (error) which is the error to wrap.
//
// Returns error which wraps sentinel.
func (d *BorrowDeclarations) refuse(name string, class BorrowClass, reason string, sentinel error) error {
	d.refusals = append(d.refusals, fmt.Sprintf("%s: %s: %s", name, class, reason))
	if licenceCounting {
		hostDeclarationCounters.refused.Add(1)
	}
	return fmt.Errorf("%w: %s declared %s: %s", sentinel, name, class, reason)
}

// lookup returns function's declared class once the declarations are frozen.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns BorrowClass which is the declared class.
// Returns bool which is false when none.
func (d *BorrowDeclarations) lookup(function reflect.Value) (BorrowClass, bool) {
	if d == nil || !d.frozen.Load() || !function.IsValid() || function.Kind() != reflect.Func || function.IsNil() {
		return BorrowUnknown, false
	}
	classes := d.classes.Load()
	if classes == nil {
		return BorrowUnknown, false
	}
	class, ok := (*classes)[function.Pointer()]
	return class, ok
}

// borrowDeclarationName names a declared function as runtime.FuncForPC reports it.
//
// Takes value (reflect.Value) which is the declared value.
//
// Returns string which is the qualified name, or a description.
func borrowDeclarationName(value reflect.Value) string {
	if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
		return fmt.Sprintf("%v", value)
	}
	if info := runtime.FuncForPC(value.Pointer()); info != nil {
		return info.Name()
	}
	return "unnamed"
}

// borrowDeclarationRefusal applies the checks a host borrow declaration must pass.
//
// Takes value (reflect.Value) which is the declared function.
// Takes name (string) which is its qualified name.
// Takes class (BorrowClass) which is the declared class.
//
// Returns string which is the reason for refusal, or "" when accepted.
func borrowDeclarationRefusal(value reflect.Value, name string, class BorrowClass) string {
	if strings.HasSuffix(name, "-fm") || name == "reflect.methodValueCall" || (runtime.GOARCH == "wasm" && namesAMethod(name)) {
		return "a method value's code pointer does not name the method"
	}
	if gcClass, tagged := gcEscapeTagClasses[name]; tagged && borrowStrictness(class) < borrowStrictness(gcClass) {
		return "gc escape tags say " + gcClass.String()
	}
	if class.handOffCause().Revokes() {
		return ""
	}
	functionType := value.Type()
	for index := range functionType.NumIn() {
		if typeMayCarryGuestCode(functionType.In(index), map[reflect.Type]bool{}) {
			return fmt.Sprintf("parameter %d (%s) can carry guest code, so the native may call back", index, functionType.In(index))
		}
	}
	if class == BorrowReadsOnly {
		return readsOnlyResultRefusal(functionType)
	}
	return ""
}

// readsOnlyResultRefusal refuses a reads-only declaration of a function whose result may
// alias an argument: one that is neither a string nor free of pointers.
//
// Takes functionType (reflect.Type) which is the declared function's type.
//
// Returns string which is the reason for refusal, or "" when every result is safe.
func readsOnlyResultRefusal(functionType reflect.Type) string {
	for index := range functionType.NumOut() {
		if result := functionType.Out(index); result.Kind() != reflect.String && !typeIsPointerFree(result) {
			return fmt.Sprintf("result %d (%s) may alias an argument", index, result)
		}
	}
	return ""
}

// namesAMethod reports whether a qualified function name, as runtime.FuncForPC reports
// it, names a method ("pkg.(*T).M" or "pkg.T.M"). The compiler's closures and go and
// defer wrappers ("pkg.F.func1", "pkg.F.gowrap1", "pkg.F.deferwrap1") are not methods.
//
// Takes name (string) which is the qualified name.
//
// Returns bool which is true when the name has a receiver type.
func namesAMethod(name string) bool {
	segment := name[strings.LastIndexByte(name, '/')+1:]
	if strings.Contains(segment, ".(") {
		return true
	}
	parts := strings.Split(segment, ".")
	if len(parts) < methodNameMinSegments {
		return false
	}
	last := parts[len(parts)-1]
	return !strings.HasPrefix(last, "func") && !strings.HasPrefix(last, "gowrap") && !strings.HasPrefix(last, "deferwrap")
}

// typeMayCarryGuestCode reports whether a value of valueType can hold a function or an
// interface, through which a native could run guest code.
//
// Takes valueType (reflect.Type) which is the parameter type.
// Takes seen (map[reflect.Type]bool) which breaks cycles.
//
// Returns bool which is true when it can.
func typeMayCarryGuestCode(valueType reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[valueType] {
		return false
	}
	seen[valueType] = true
	switch valueType.Kind() {
	case reflect.Func, reflect.Interface, reflect.UnsafePointer:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return typeMayCarryGuestCode(valueType.Elem(), seen)
	case reflect.Map:
		return typeMayCarryGuestCode(valueType.Key(), seen) || typeMayCarryGuestCode(valueType.Elem(), seen)
	case reflect.Struct:
		for field := range valueType.Fields() {
			if typeMayCarryGuestCode(field.Type, seen) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
