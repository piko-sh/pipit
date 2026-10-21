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
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"piko.sh/safeconv"

	"pipit.sh/pipit/internal/fault"
)

const (
	// DefaultNamedCompositeQuota is how many new named composite types one interpreter may
	// add to the process registry unless its embedder chooses otherwise.
	DefaultNamedCompositeQuota = 4096

	// DefaultNamedCompositeProcessLimit is the process-wide backstop on the registry's size,
	// counting every type forged, including those a failed or lost build leaves behind.
	DefaultNamedCompositeProcessLimit = 1 << 16
)

var (
	// ErrNamedCompositeLimit is fault.ErrNamedCompositeLimit, kept here so callers of this
	// package can match it without importing fault.
	ErrNamedCompositeLimit = fault.ErrNamedCompositeLimit

	// ErrNamedCompositeQuota is fault.ErrNamedCompositeQuota.
	ErrNamedCompositeQuota = fault.ErrNamedCompositeQuota

	// ErrNamedCompositeUnsupported is fault.ErrNamedCompositeUnsupported.
	ErrNamedCompositeUnsupported = fault.ErrNamedCompositeUnsupported

	// namedComposites is the process-global registry of named composite types.
	namedComposites = newNamedCompositeRegistry(DefaultNamedCompositeProcessLimit)
)

// NamedCompositeIdentity names a named composite type for NamedCompositeTypeFor.
type NamedCompositeIdentity struct {
	// PkgPath is the declaring package's import path, e.g. "example.com/shapes".
	PkgPath string

	// PkgName is the declaring package's short name, e.g. "shapes".
	PkgName string

	// Name is the type's name with any type arguments and without its package, e.g. "Points"
	// or "Stack[int]".
	Name string

	// BareName is the declared identifier, e.g. "Stack", which keys the method table.
	BareName string
}

// NamedCompositeInfo is the registry's record of one named composite type.
type NamedCompositeInfo struct {
	// Underlying is the runtime type of the declared underlying type, which the named type
	// converts to and from without copying.
	Underlying reflect.Type

	// NamedTypeInfo is the type's source-level identity.
	NamedTypeInfo

	// ID is the type's position in the registry, counted from one, so a value that cannot
	// hold a reflect.Type (a closure's record of its named func type) can name it.
	ID uint32
}

// NamedCompositeQuota bounds how many new named composite types one interpreter may add
// to the process registry.
//
// A type already in the registry, built by this interpreter or another, costs nothing.
// Every type this interpreter forges is charged, including the members of a group whose
// build failed or lost a race, since their descriptors stay in the process. A quota is
// safe for concurrent use. A nil quota charges nothing, leaving only the process-wide
// cap.
type NamedCompositeQuota struct {
	// limit is the most types the quota allows.
	limit int64

	// used is how many types the quota has been charged.
	used atomic.Int64
}

// namedCompositeKey identifies one named composite type. Two declarations with the same
// package path and name but different underlying types are different types.
type namedCompositeKey struct {
	// underlying is the declared underlying type's runtime type.
	underlying reflect.Type

	// pkgPath is the declaring package's import path.
	pkgPath string

	// name is the type's name with any type arguments.
	name string
}

// namedCompositeRegistry gives every named array, slice, map, channel and function type a
// runtime type of its own.
//
// Each distinct type is built once per process and kept for its lifetime. Identical
// declarations in different programs therefore share one entry. Every forge is charged to
// the caller's quota (NamedCompositeQuota) and counted against the process-wide cap.
// Builds without the forging code keep no registry entries.
type namedCompositeRegistry struct {
	// byKey maps each identity to its type.
	byKey map[namedCompositeKey]reflect.Type

	// info maps each built type to its record. Read without the lock on dispatch and
	// formatting paths.
	info sync.Map

	// cycles maps the key of each built group of self-referential types
	// (NamedCompositeCycleFor) to its types in member order.
	cycles map[string][]reflect.Type

	// byID lists the built types in ID order. byID[id-1] is the type with that ID.
	byID []reflect.Type

	// forged counts every type the registry has forged, kept or not, which the process-wide
	// cap bounds.
	forged int

	// limit is the process-wide cap on forged.
	limit int

	// mu guards byKey, cycles, byID, forged and limit.
	mu sync.RWMutex
}

// Qualified renders the identity as %T prints it, e.g. "main.Points".
//
// Returns string which is the package-qualified name.
func (identity NamedCompositeIdentity) Qualified() string {
	return qualifiedNameOf(identity)
}

// NewNamedCompositeQuota returns a quota for one interpreter.
//
// Takes limit (int) which is the most new types the interpreter may add. Zero or less
// gives DefaultNamedCompositeQuota.
//
// Returns *NamedCompositeQuota which has been charged nothing.
func NewNamedCompositeQuota(limit int) *NamedCompositeQuota {
	if limit <= 0 {
		limit = DefaultNamedCompositeQuota
	}
	return &NamedCompositeQuota{limit: int64(limit), used: atomic.Int64{}}
}

// Limit returns the most types the quota allows.
//
// Returns int which is the limit, zero for a nil quota.
func (quota *NamedCompositeQuota) Limit() int {
	if quota == nil {
		return 0
	}
	return int(quota.limit)
}

// Used returns how many types the quota has been charged.
//
// Returns int which is the count, zero for a nil quota.
func (quota *NamedCompositeQuota) Used() int {
	if quota == nil {
		return 0
	}
	return int(quota.used.Load())
}

// TypeFor is NamedCompositeTypeFor charged to this quota.
//
// Takes identity (NamedCompositeIdentity) which names the type.
// Takes underlying (reflect.Type) which is the declared underlying type's runtime type.
//
// Returns reflect.Type which is the named type.
// Returns error which wraps ErrNamedCompositeQuota, ErrNamedCompositeLimit or
// ErrNamedCompositeUnsupported.
func (quota *NamedCompositeQuota) TypeFor(identity NamedCompositeIdentity, underlying reflect.Type) (reflect.Type, error) {
	return namedComposites.typeFor(quota, identity, underlying)
}

// CycleFor is NamedCompositeCycleFor charged to this quota.
//
// Takes members ([]CycleMember) which describe the group.
// Takes build (func(map[string]reflect.Type) ([]reflect.Type, error)) which converts the
// members' underlying types.
//
// Returns map[string]reflect.Type which maps each member's qualified name to its type.
// Returns error which wraps ErrNamedCompositeQuota, ErrNamedCompositeLimit or
// ErrNamedCompositeUnsupported, or is build's error.
func (quota *NamedCompositeQuota) CycleFor(members []CycleMember, build func(forged map[string]reflect.Type) ([]reflect.Type, error)) (map[string]reflect.Type, error) {
	return namedComposites.cycleFor(quota, members, build)
}

// reserve charges count types to the quota.
//
// Takes count (int) which is how many types are about to be forged.
// Takes name (string) which names the first of them for the error.
//
// Returns error which wraps ErrNamedCompositeQuota when the charge would pass the limit.
// Nothing is charged then.
func (quota *NamedCompositeQuota) reserve(count int, name string) error {
	if quota == nil {
		return nil
	}
	for {
		used := quota.used.Load()
		if used+int64(count) > quota.limit {
			return fmt.Errorf("%w: %s would pass the limit of %d", ErrNamedCompositeQuota, name, quota.limit)
		}
		if quota.used.CompareAndSwap(used, used+int64(count)) {
			return nil
		}
	}
}

// typeFor returns the type for identity over underlying, building it on first use.
//
// Takes quota (*NamedCompositeQuota) which is charged for a new type. May be nil.
// Takes identity (NamedCompositeIdentity) which names the type.
// Takes underlying (reflect.Type) which is the declared underlying type's runtime type.
//
// Returns reflect.Type which is the named type, or underlying itself in a build without
// the forging code.
// Returns error which wraps ErrNamedCompositeQuota, ErrNamedCompositeLimit or
// ErrNamedCompositeUnsupported.
//
// Concurrent access acquires r.mu in read mode for a known identity and in write mode to
// build a new one.
func (r *namedCompositeRegistry) typeFor(quota *NamedCompositeQuota, identity NamedCompositeIdentity, underlying reflect.Type) (reflect.Type, error) {
	if err := checkNameable(underlying, identity.Name); err != nil {
		return nil, err
	}
	if !namedCompositeForging {
		return underlying, nil
	}
	key := namedCompositeKey{underlying: underlying, pkgPath: identity.PkgPath, name: identity.Name}
	r.mu.RLock()
	existing, ok := r.byKey[key]
	r.mu.RUnlock()
	if ok {
		return existing, nil
	}
	if err := forgeLayoutError(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok = r.byKey[key]; ok {
		return existing, nil
	}
	if err := r.charge(quota, 1, qualifiedNameOf(identity)); err != nil {
		return nil, err
	}
	named, err := forgeNamedType(underlying, qualifiedNameOf(identity), identity.PkgPath)
	if err != nil {
		return nil, err
	}
	r.byKey[key] = named
	r.storeInfo(named, underlying, identity)
	return named, nil
}

// charge counts count new forges against the process-wide cap and the quota.
//
// Takes quota (*NamedCompositeQuota) which is charged. May be nil.
// Takes count (int) which is how many types are about to be forged.
// Takes name (string) which names the first of them for the error.
//
// Returns error which wraps ErrNamedCompositeLimit or ErrNamedCompositeQuota. Nothing is
// charged then.
//
// The caller must hold r.mu in write mode.
func (r *namedCompositeRegistry) charge(quota *NamedCompositeQuota, count int, name string) error {
	if r.forged+count > r.limit {
		return fmt.Errorf("%w: %s would pass the process limit of %d", ErrNamedCompositeLimit, name, r.limit)
	}
	if err := quota.reserve(count, name); err != nil {
		return err
	}
	r.forged += count
	return nil
}

// storeInfo records a kept type and gives it the next ID.
//
// Takes named (reflect.Type) which is the forged type.
// Takes underlying (reflect.Type) which is its underlying type.
// Takes identity (NamedCompositeIdentity) which names it.
//
// The caller must hold r.mu in write mode.
func (r *namedCompositeRegistry) storeInfo(named, underlying reflect.Type, identity NamedCompositeIdentity) {
	r.byID = append(r.byID, named)
	r.info.Store(named, NamedCompositeInfo{
		Underlying: underlying,
		NamedTypeInfo: NamedTypeInfo{
			QualifiedName: qualifiedNameOf(identity),
			Name:          identity.Name,
			BareName:      identity.BareName,
			PkgName:       identity.PkgName,
			PkgPath:       identity.PkgPath,
		},
		ID: safeconv.IntToUint32(len(r.byID)),
	})
}

// setLimit replaces the process-wide cap.
//
// Takes limit (int) which is the new cap.
//
// Returns int which is the previous cap.
//
// Concurrent access acquires r.mu in write mode.
func (r *namedCompositeRegistry) setLimit(limit int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous := r.limit
	r.limit = limit
	return previous
}

// lookup returns the record of t.
//
// Takes t (reflect.Type) which may be nil.
//
// Returns NamedCompositeInfo which is the record.
// Returns bool which is true when t is a named composite type.
func (r *namedCompositeRegistry) lookup(t reflect.Type) (NamedCompositeInfo, bool) {
	if t == nil || !isCompositeKind(t.Kind()) {
		return NamedCompositeInfo{}, false
	}
	stored, ok := r.info.Load(t)
	if !ok {
		return NamedCompositeInfo{}, false
	}
	info, ok := stored.(NamedCompositeInfo)
	return info, ok
}

// typeByID returns the type with the given ID.
//
// Takes id (uint32) which is an ID from a NamedCompositeInfo, or zero.
//
// Returns reflect.Type which is nil for zero or an ID the registry never issued.
//
// Concurrent access acquires r.mu in read mode.
func (r *namedCompositeRegistry) typeByID(id uint32) reflect.Type {
	if id == 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if int(id) > len(r.byID) {
		return nil
	}
	return r.byID[id-1]
}

// newNamedCompositeRegistry builds an empty registry.
//
// Takes limit (int) which is the process-wide cap on forged types.
//
// Returns *namedCompositeRegistry which holds no types yet.
func newNamedCompositeRegistry(limit int) *namedCompositeRegistry {
	return &namedCompositeRegistry{
		byKey:  make(map[namedCompositeKey]reflect.Type),
		info:   sync.Map{},
		cycles: make(map[string][]reflect.Type),
		byID:   nil,
		forged: 0,
		limit:  limit,
		mu:     sync.RWMutex{},
	}
}

// SetNamedCompositeProcessLimit replaces the process-wide cap on forged named composite
// types, the backstop behind every interpreter's quota. Types already forged stay.
//
// Takes limit (int) which is the new cap. Zero or less restores
// DefaultNamedCompositeProcessLimit.
//
// Returns int which is the previous cap.
func SetNamedCompositeProcessLimit(limit int) int {
	if limit <= 0 {
		limit = DefaultNamedCompositeProcessLimit
	}
	return namedComposites.setLimit(limit)
}

// NamedCompositeTypesSupported reports whether this build gives named composite types
// runtime types of their own. Builds without the forging code (the safe and js/wasm
// builds) do not.
//
// Returns bool which is true when named composite types get their own runtime types.
func NamedCompositeTypesSupported() bool {
	return namedCompositeForging
}

// NamedCompositeTypeFor returns the runtime type of the named composite type identity
// whose underlying type is underlying, building it on first use.
//
// Takes identity (NamedCompositeIdentity) which names the type.
// Takes underlying (reflect.Type) which is the declared underlying type's runtime type,
// an unnamed array, slice, map, channel or function type.
//
// Returns reflect.Type which is the named type, or underlying itself in a build without
// the forging code.
// Returns error which wraps ErrNamedCompositeLimit or ErrNamedCompositeUnsupported.
func NamedCompositeTypeFor(identity NamedCompositeIdentity, underlying reflect.Type) (reflect.Type, error) {
	return namedComposites.typeFor(nil, identity, underlying)
}

// LookupNamedCompositeInfo returns the record of a named composite type.
//
// Takes t (reflect.Type) which may be nil.
//
// Returns NamedCompositeInfo which is the record.
// Returns bool which is true when t is a named composite type. Lock-free.
func LookupNamedCompositeInfo(t reflect.Type) (NamedCompositeInfo, bool) {
	return namedComposites.lookup(t)
}

// IsNamedCompositeType reports whether t is a named composite type. Lock-free.
//
// Takes t (reflect.Type) which may be nil.
//
// Returns bool which is true for a type NamedCompositeTypeFor built.
func IsNamedCompositeType(t reflect.Type) bool {
	_, ok := namedComposites.lookup(t)
	return ok
}

// NamedCompositeID returns the registry ID of a named composite type.
//
// Takes t (reflect.Type) which may be nil.
//
// Returns uint32 which is zero when t is not a named composite type.
func NamedCompositeID(t reflect.Type) uint32 {
	info, _ := namedComposites.lookup(t)
	return info.ID
}

// NamedCompositeTypeByID returns the named composite type with the given ID.
//
// Takes id (uint32) which is an ID NamedCompositeID returned, or zero.
//
// Returns reflect.Type which is nil for zero or an unknown ID.
func NamedCompositeTypeByID(id uint32) reflect.Type {
	return namedComposites.typeByID(id)
}

// LookupNamedTypeInfo returns the source-level identity of a type whose runtime type
// pipit supplies for a named type, which is a named-scalar pool type or a named composite
// type.
//
// Takes t (reflect.Type) which may be nil.
//
// Returns NamedTypeInfo which is the identity.
// Returns bool which is true when t is either kind of type.
func LookupNamedTypeInfo(t reflect.Type) (NamedTypeInfo, bool) {
	if info, ok := LookupNamedScalarPoolInfo(t); ok {
		return info, true
	}
	if info, ok := namedComposites.lookup(t); ok {
		return info.NamedTypeInfo, true
	}
	return NamedTypeInfo{}, false
}

// checkNameable reports whether underlying can be the underlying type of a named
// composite type, before anything is forged or charged.
//
// Takes underlying (reflect.Type) which is the candidate.
// Takes name (string) which names the type for the error.
//
// Returns error which wraps ErrNamedCompositeUnsupported for a missing or named type, or
// one of another kind.
func checkNameable(underlying reflect.Type, name string) error {
	switch {
	case underlying == nil:
		return fmt.Errorf("%w: %s has no underlying type", ErrNamedCompositeUnsupported, name)
	case !isCompositeKind(underlying.Kind()):
		return fmt.Errorf("%w: %s has kind %s", ErrNamedCompositeUnsupported, name, underlying.Kind())
	case underlying.Name() != "":
		return fmt.Errorf("%w: underlying type %s of %s is already named", ErrNamedCompositeUnsupported, underlying, name)
	}
	return nil
}

// isCompositeKind reports whether a type of kind can be a named composite type, so the
// lookups decline every other type without touching the registry.
//
// Takes kind (reflect.Kind) which is the candidate's kind.
//
// Returns bool which is true for array, slice, map, channel and function kinds.
func isCompositeKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return true
	default:
		return false
	}
}

// qualifiedNameOf renders identity as %T prints it, e.g. "main.Points".
//
// Takes identity (NamedCompositeIdentity) which names the type.
//
// Returns string which is the package-qualified name.
func qualifiedNameOf(identity NamedCompositeIdentity) string {
	if identity.PkgName == "" {
		return identity.Name
	}
	return identity.PkgName + "." + identity.Name
}
