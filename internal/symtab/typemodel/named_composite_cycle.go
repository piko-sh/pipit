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

package typemodel

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const (
	// cycleShapeSeparator separates the members of a group in its registry key.
	cycleShapeSeparator = "\x00"

	// maxCycleFuncArity is the most parameters and results a function placeholder may have,
	// reflect.FuncOf's own limit.
	maxCycleFuncArity = 128
)

// CycleMember describes one named array, slice, map, channel or function type of a group
// whose underlying types reach one another, such as `type L []L` or the pair `type X []Y;
// type Y map[string]X`.
//
// Such a type's underlying type contains the type itself, so its runtime type cannot be
// copied from a finished underlying type (see NamedCompositeCycleFor).
type CycleMember struct {
	// Placeholder is an unnamed type of the member's kind and layout that holds no member,
	// such as []*byte for a slice. A function placeholder has the member's parameter and
	// result counts and variadic flag, and an array placeholder its length and element
	// layout.
	Placeholder reflect.Type

	// Identity names the member.
	Identity NamedCompositeIdentity

	// Shape is the member's underlying type written in the cycle shape notation
	// (CycleShapeMember, CycleShapeLeaf and the composite forms), which with the identities
	// identifies the group across compiles and loaded bytecode.
	Shape string
}

// cycleFor is NamedCompositeCycleFor against this registry, charged to quota.
//
// Everything that can be checked before forging is checked first, so a malformed group
// forges nothing. The members are then charged to the quota and the process-wide cap, and
// stay charged whether or not the group is kept, since a forged descriptor and its
// runtime names are never freed.
//
// Takes quota (*NamedCompositeQuota) which is charged for the members. May be nil.
// Takes members ([]CycleMember) which describe the group.
// Takes build (func(map[string]reflect.Type) ([]reflect.Type, error)) which converts the
// underlying types.
//
// Returns map[string]reflect.Type which maps each qualified name to its type.
// Returns error when the group cannot be built.
//
// Concurrent access acquires r.mu in read mode to find a built group and in write mode to
// charge and to record a new one. build runs without the lock.
func (r *namedCompositeRegistry) cycleFor(quota *NamedCompositeQuota, members []CycleMember, build func(forged map[string]reflect.Type) ([]reflect.Type, error)) (map[string]reflect.Type, error) {
	if err := checkCycleMembers(members); err != nil {
		return nil, err
	}
	sortCycleMembers(members)
	key := cycleKey(members)
	r.mu.RLock()
	existing, found := r.cycles[key]
	r.mu.RUnlock()
	if found {
		return cycleTypesByName(members, existing), nil
	}
	if !namedCompositeForging {
		return nil, fmt.Errorf("%w: %s is self-referential, which this build cannot represent", ErrNamedCompositeUnsupported, qualifiedNameOf(members[0].Identity))
	}
	if err := forgeLayoutError(); err != nil {
		return nil, err
	}
	if recorded, err := r.chargeCycle(quota, key, members); recorded != nil || err != nil {
		return recorded, err
	}
	forged := make([]reflect.Type, len(members))
	byName := make(map[string]reflect.Type, len(members))
	for index, member := range members {
		named, err := forgeNamedType(member.Placeholder, qualifiedNameOf(member.Identity), member.Identity.PkgPath)
		if err != nil {
			return nil, err
		}
		forged[index] = named
		byName[qualifiedNameOf(member.Identity)] = named
	}
	underlying, err := build(byName)
	if err != nil {
		return nil, err
	}
	if len(underlying) != len(members) {
		return nil, fmt.Errorf("%w: %d underlying types for %d members", ErrNamedCompositeUnsupported, len(underlying), len(members))
	}
	for index := range members {
		if err := patchCycleMember(forged[index], underlying[index]); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrNamedCompositeUnsupported, qualifiedNameOf(members[index].Identity), err)
		}
	}
	return r.recordCycle(key, members, forged, underlying), nil
}

// chargeCycle charges a group's members before they are forged, unless another build has
// recorded the group meanwhile.
//
// Takes quota (*NamedCompositeQuota) which is charged. May be nil.
// Takes key (string) which identifies the group.
// Takes members ([]CycleMember) which describe it, sorted.
//
// Returns map[string]reflect.Type which is the recorded group's types, or nil when the
// caller should build it.
// Returns error which wraps ErrNamedCompositeLimit or ErrNamedCompositeQuota.
//
// Concurrent access acquires r.mu in write mode.
func (r *namedCompositeRegistry) chargeCycle(quota *NamedCompositeQuota, key string, members []CycleMember) (map[string]reflect.Type, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, found := r.cycles[key]; found {
		return cycleTypesByName(members, existing), nil
	}
	return nil, r.charge(quota, len(members), qualifiedNameOf(members[0].Identity))
}

// recordCycle records a built group, or returns the group another compile recorded first.
//
// Takes key (string) which identifies the group.
// Takes members ([]CycleMember) which describe it, sorted.
// Takes forged ([]reflect.Type) which are the patched types in member order.
// Takes underlying ([]reflect.Type) which are their underlying types.
//
// Returns map[string]reflect.Type which maps each qualified name to its type.
//
// Concurrent access acquires r.mu in write mode.
func (r *namedCompositeRegistry) recordCycle(key string, members []CycleMember, forged, underlying []reflect.Type) map[string]reflect.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, found := r.cycles[key]; found {
		return cycleTypesByName(members, existing)
	}
	for index, member := range members {
		named := forged[index]
		r.byKey[namedCompositeKey{underlying: underlying[index], pkgPath: member.Identity.PkgPath, name: member.Identity.Name}] = named
		r.storeInfo(named, underlying[index], member.Identity)
	}
	r.cycles[key] = forged
	return cycleTypesByName(members, forged)
}

// NamedCompositeCycleFor returns the runtime types of a group of named composite types
// whose underlying types reach one another, building them on first use. New types are
// charged to no quota.
//
// Takes members ([]CycleMember) which describe the group. The slice is reordered by
// package path and name.
// Takes build (func(map[string]reflect.Type) ([]reflect.Type, error)) which converts the
// members' underlying types given the forged types keyed by qualified name, returning
// them in member order.
//
// Returns map[string]reflect.Type which maps each member's qualified name to its type.
// Returns error which wraps ErrNamedCompositeLimit or ErrNamedCompositeUnsupported, or is
// build's error.
func NamedCompositeCycleFor(members []CycleMember, build func(forged map[string]reflect.Type) ([]reflect.Type, error)) (map[string]reflect.Type, error) {
	return namedComposites.cycleFor(nil, members, build)
}

// CycleShapeMember writes a reference to a member of the group in the cycle shape
// notation.
//
// Takes pkgPath (string) which is the member's package path.
// Takes name (string) which is its name with any type arguments.
//
// Returns string which is the reference.
func CycleShapeMember(pkgPath, name string) string {
	return "@" + pkgPath + "." + name
}

// CycleShapeLeaf writes a type that reaches no member of the group in the cycle shape
// notation, by its runtime identity.
//
// Takes t (reflect.Type) which is the type's runtime type.
//
// Returns string which is the reference.
func CycleShapeLeaf(t reflect.Type) string {
	if t == nil {
		return "#nil"
	}
	return "#" + strconv.FormatUint(uint64(reflect.ValueOf(t).Pointer()), 16)
}

// CycleShapeComposite writes an unnamed composite type that reaches a member in the cycle
// shape notation.
//
// Takes kind (reflect.Kind) which is the composite's kind.
// Takes detail (int) which is an array's length or a channel's direction, else zero.
// Takes variadic (bool) which is true for a variadic function.
// Takes parts ([]string) which are the element, or the key then the value of a map, or
// the parameters then the results of a function, with inCount saying where they split.
// Takes inCount (int) which is a function's parameter count.
//
// Returns string which is the composite's shape.
func CycleShapeComposite(kind reflect.Kind, detail int, variadic bool, parts []string, inCount int) string {
	if kind == reflect.Func {
		inCount = min(max(inCount, 0), len(parts))
		suffix := ""
		if variadic {
			suffix = "..."
		}
		return "func(" + strings.Join(parts[:inCount], ",") + suffix + ")(" + strings.Join(parts[inCount:], ",") + ")"
	}
	first, second := "", ""
	if len(parts) > 0 {
		first = parts[0]
	}
	if len(parts) > 1 {
		second = parts[1]
	}
	switch kind {
	case reflect.Map:
		return "map[" + first + "]" + second
	case reflect.Array:
		return "[" + strconv.Itoa(detail) + "]" + first
	case reflect.Chan:
		return "chan" + strconv.Itoa(detail) + " " + first
	case reflect.Pointer:
		return "*" + first
	default:
		return "[]" + first
	}
}

// CyclePlaceholder returns a placeholder (see CycleMember) for a member of kind with no
// member in it. A function placeholder takes inCount parameters and outCount results.
//
// Takes kind (reflect.Kind) which is the member's kind.
// Takes inCount (int) which is a function's parameter count.
// Takes outCount (int) which is a function's result count.
// Takes variadic (bool) which is true for a variadic function.
// Takes length (int) which is an array's length.
// Takes element (reflect.Type) which is an array element with the element's layout.
//
// Returns reflect.Type which is the placeholder.
// Returns error which wraps ErrNamedCompositeUnsupported for any other kind, a negative
// count or length, or an array too large for reflect to build.
func CyclePlaceholder(kind reflect.Kind, inCount, outCount int, variadic bool, length int, element reflect.Type) (reflect.Type, error) {
	word := reflect.TypeFor[*byte]()
	switch kind {
	case reflect.Slice:
		return reflect.SliceOf(word), nil
	case reflect.Map:
		return reflect.MapOf(word, word), nil
	case reflect.Chan:
		return reflect.ChanOf(reflect.BothDir, word), nil
	case reflect.Func:
		return funcPlaceholder(inCount, outCount, variadic)
	case reflect.Array:
		return ArrayOfChecked(length, element)
	default:
		return nil, fmt.Errorf("%w: kind %s cannot be a member", ErrNamedCompositeUnsupported, kind)
	}
}

// ArrayOfChecked is reflect.ArrayOf for a length and element that may come from a
// bytecode file.
//
// Takes length (int) which is the array's length.
// Takes element (reflect.Type) which is its element type.
//
// Returns reflect.Type which is the array type.
// Returns error which wraps ErrNamedCompositeUnsupported.
func ArrayOfChecked(length int, element reflect.Type) (result reflect.Type, err error) {
	switch {
	case element == nil:
		return nil, fmt.Errorf("%w: array without an element type", ErrNamedCompositeUnsupported)
	case length < 0:
		return nil, fmt.Errorf("%w: negative array length %d", ErrNamedCompositeUnsupported, length)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("%w: %v", ErrNamedCompositeUnsupported, recovered)
		}
	}()
	return reflect.ArrayOf(length, element), nil
}

// funcPlaceholder builds a function placeholder with the given counts.
//
// Takes inCount (int) which is the parameter count.
// Takes outCount (int) which is the result count.
// Takes variadic (bool) which is true for a variadic function.
//
// Returns reflect.Type which is the placeholder.
// Returns error which wraps ErrNamedCompositeUnsupported for a negative count or a
// signature reflect cannot build.
func funcPlaceholder(inCount, outCount int, variadic bool) (result reflect.Type, err error) {
	if inCount < 0 || outCount < 0 || inCount+outCount > maxCycleFuncArity || (variadic && inCount == 0) {
		return nil, fmt.Errorf("%w: signature with %d parameters and %d results", ErrNamedCompositeUnsupported, inCount, outCount)
	}
	word := reflect.TypeFor[*byte]()
	in := slices.Repeat([]reflect.Type{word}, inCount)
	if variadic {
		in[inCount-1] = reflect.SliceOf(word)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("%w: %v", ErrNamedCompositeUnsupported, recovered)
		}
	}()
	return reflect.FuncOf(in, slices.Repeat([]reflect.Type{word}, outCount), variadic), nil
}

// checkCycleMembers checks everything about a group that can be checked before forging.
// The group must have members, each with a nameable placeholder of a member kind, a name,
// a shape and a qualified name of its own.
//
// Takes members ([]CycleMember) which describe the group.
//
// Returns error which wraps ErrNamedCompositeUnsupported naming the first problem.
func checkCycleMembers(members []CycleMember) error {
	if len(members) == 0 {
		return fmt.Errorf("%w: empty group", ErrNamedCompositeUnsupported)
	}
	seen := make(map[string]bool, len(members))
	for _, member := range members {
		name := qualifiedNameOf(member.Identity)
		switch {
		case member.Identity.Name == "" || member.Shape == "":
			return fmt.Errorf("%w: group member %q has no name or shape", ErrNamedCompositeUnsupported, name)
		case seen[name]:
			return fmt.Errorf("%w: group names %s twice", ErrNamedCompositeUnsupported, name)
		}
		seen[name] = true
		if err := checkNameable(member.Placeholder, name); err != nil {
			return err
		}
	}
	return nil
}

// cycleKey returns the registry key of a group of members sorted by sortCycleMembers.
//
// Takes members ([]CycleMember) which are the sorted members.
//
// Returns string which identifies the group.
func cycleKey(members []CycleMember) string {
	var key strings.Builder
	for _, member := range members {
		key.WriteString(member.Identity.PkgPath)
		key.WriteString(cycleShapeSeparator)
		key.WriteString(member.Identity.Name)
		key.WriteString(cycleShapeSeparator)
		key.WriteString(member.Shape)
		key.WriteString(cycleShapeSeparator)
	}
	return key.String()
}

// sortCycleMembers orders members by package path and name, so the same group gives the
// same key and member order whichever member a compile reaches first.
//
// Takes members ([]CycleMember) which are sorted in place.
func sortCycleMembers(members []CycleMember) {
	slices.SortFunc(members, func(a, b CycleMember) int {
		return cmp.Or(strings.Compare(a.Identity.PkgPath, b.Identity.PkgPath), strings.Compare(a.Identity.Name, b.Identity.Name))
	})
}

// cycleTypesByName pairs a group's types with its members' qualified names.
//
// Takes members ([]CycleMember) which are the sorted members.
// Takes types ([]reflect.Type) which are their types in the same order.
//
// Returns map[string]reflect.Type which maps each qualified name to its type.
func cycleTypesByName(members []CycleMember, types []reflect.Type) map[string]reflect.Type {
	result := make(map[string]reflect.Type, len(members))
	for index, member := range members {
		result[qualifiedNameOf(member.Identity)] = types[index]
	}
	return result
}
