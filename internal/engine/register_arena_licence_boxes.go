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
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// boxShareCall holds the decision about sharing arena boxes with a reflected callee.
//
// The decision is made once per call, at its first candidate argument.
type boxShareCall struct {
	// arena is the calling VM's arena.
	arena *RegisterArena

	// function is the native callee.
	function reflect.Value

	// decided is true once shares holds the decision.
	decided bool

	// shares is true when the callee borrows and the arena and build allow sharing.
	shares bool
}

// newBoxShareCall returns the box-sharing state for a reflect call to function; it
// decides nothing yet.
//
// Takes arena (*RegisterArena) which is the caller's arena; may be nil.
// Takes function (reflect.Value) which is the native callee.
//
// Returns boxShareCall which is the state.
func newBoxShareCall(arena *RegisterArena, function reflect.Value) boxShareCall {
	return boxShareCall{arena: arena, function: function, decided: false, shares: false}
}

// share returns raw as a read-only arena box when box sharing applies to argument index
// of the call, and raw unchanged otherwise.
//
// Takes site (*program.CallSite) which describes the call.
// Takes index (int) which is the argument's position.
// Takes kind (isa.RegisterKind) which is the argument's register kind.
// Takes raw (reflect.Value) which is the argument as read from its register.
//
// Returns reflect.Value which is the argument to shape.
// Returns bool which is true when the box is shared.
func (c *boxShareCall) share(site *program.CallSite, index int, kind isa.RegisterKind, raw reflect.Value) (reflect.Value, bool) {
	if c.arena == nil || site.IsEllipsisSpread || !raw.IsValid() || raw.Kind() == reflect.Interface ||
		!raw.CanAddr() || !boxIndirect(raw) {
		return raw, false
	}
	if !c.sharesWith() {
		return raw, false
	}
	if !nativeParameterIsInterface(c.function.Type(), index+len(site.LinkedTypeArgs), false) {
		return raw, false
	}
	if !c.arena.trueBox(raw, kind) {
		return raw, false
	}
	return readOnlyBox(raw), true
}

// sharesWith decides whether a callee may share arena boxes.
//
// Sharing requires a licensed arena in an exact-probe build and a callee classified as
// borrows by the registry. The decision is made once per call.
//
// Returns bool which is the decision.
func (c *boxShareCall) sharesWith() bool {
	if !c.decided {
		c.decided = true
		a := c.arena
		c.shares = arenaUsesUnsafeSlabs && licenceExactProbes && a.licensed && !a.heapData &&
			c.function.IsValid() && c.function.Kind() == reflect.Func && a.nativeBorrowClass(c.function) == BorrowBorrows
	}
	return c.shares
}

// note records the hand-off of a shared box with boxed=true.
//
// The value passed is the box.
//
// Takes value (reflect.Value) which is the shared box.
func (c *boxShareCall) note(value reflect.Value) {
	if !c.arena.countsHandOffs() {
		return
	}
	c.arena.recordHandOff(c.arena.nativeBorrowClass(c.function).handOffCause(), value, true)
}

// trueBox reports whether value's storage is a slot of one of the arena's box slabs,
// which nothing writes once the box is made, and whether value is a box
// registerToReflectValue made for this call from a string or scalar register.
//
// A General register's value is never shared, even when it points at a box.
//
// Takes value (reflect.Value) which is an addressable, indirect value.
// Takes kind (isa.RegisterKind) which is the register kind it was read from.
//
// Returns bool which is true for a true box.
func (a *RegisterArena) trueBox(value reflect.Value, kind isa.RegisterKind) bool {
	switch kind {
	case isa.RegisterString, isa.RegisterInt64, isa.RegisterFloat64, isa.RegisterUint64, isa.RegisterComplex128:
	default:
		return false
	}
	pointer := ReflectValuePtr(value)
	return a.OwnsStringBox(pointer) || a.ownsScalarBox(pointer)
}
