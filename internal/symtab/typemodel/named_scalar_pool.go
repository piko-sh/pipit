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
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
)

const (
	// namedScalarKindRows is the number of distinct basic kinds the pool covers; one row per
	// entry in namedScalarPoolColumn.
	namedScalarKindRows = 17

	// maxNamedScalarPoolKeys bounds the process-global byKey map. Past this cap a further
	// distinct type collapses to its underlying kind without being stored.
	maxNamedScalarPoolKeys = 1 << 16
)

const (
	// namedScalarRowBool is the pool row ordinal for bool.
	namedScalarRowBool = iota

	// namedScalarRowInt is the pool row ordinal for int.
	namedScalarRowInt

	// namedScalarRowInt8 is the pool row ordinal for int8.
	namedScalarRowInt8

	// namedScalarRowInt16 is the pool row ordinal for int16.
	namedScalarRowInt16

	// namedScalarRowInt32 is the pool row ordinal for int32.
	namedScalarRowInt32

	// namedScalarRowInt64 is the pool row ordinal for int64.
	namedScalarRowInt64

	// namedScalarRowUint is the pool row ordinal for uint.
	namedScalarRowUint

	// namedScalarRowUint8 is the pool row ordinal for uint8.
	namedScalarRowUint8

	// namedScalarRowUint16 is the pool row ordinal for uint16.
	namedScalarRowUint16

	// namedScalarRowUint32 is the pool row ordinal for uint32.
	namedScalarRowUint32

	// namedScalarRowUint64 is the pool row ordinal for uint64.
	namedScalarRowUint64

	// namedScalarRowUintptr is the pool row ordinal for uintptr.
	namedScalarRowUintptr

	// namedScalarRowFloat32 is the pool row ordinal for float32.
	namedScalarRowFloat32

	// namedScalarRowFloat64 is the pool row ordinal for float64.
	namedScalarRowFloat64

	// namedScalarRowComplex64 is the pool row ordinal for complex64.
	namedScalarRowComplex64

	// namedScalarRowComplex128 is the pool row ordinal for complex128.
	namedScalarRowComplex128

	// namedScalarRowString is the pool row ordinal for string.
	namedScalarRowString

	// namedScalarRowCount is the number of populated rows.
	namedScalarRowCount
)

var (
	_ [namedScalarKindRows - namedScalarRowCount]struct{}
)

var (
	// namedScalarPoolMembers is the immutable membership set of every pool type. Built once
	// at init so hot-ish lookups (method-dispatch resolution, box-path predicates) never
	// take the registry lock.
	namedScalarPoolMembers = buildNamedScalarPoolMembers()
)

var (
	// namedScalarPool is the process-global assignment state.
	namedScalarPool = newNamedScalarPoolRegistry()

	// traceNamedScalarPoolEnabled mirrors PIPIT_TRACE_POOL, read once at start-up so a fresh
	// assignment pays one branch.
	traceNamedScalarPoolEnabled = os.Getenv("PIPIT_TRACE_POOL") != ""

	// namedScalarPoolExhaustions counts every named basic type that collapsed to its
	// underlying kind because the kind's pool columns were all assigned.
	namedScalarPoolExhaustions atomic.Int64

	// namedScalarPoolExhaustionWarned gates the one-time exhaustion warning.
	namedScalarPoolExhaustionWarned atomic.Bool
)

// NamedScalarPoolInfo carries the source-level identity recorded for an assigned pool
// type. Used to render user-facing type names (fmt %T, assertion panic messages) and to
// key method-table lookups.
type NamedScalarPoolInfo struct {
	// QualifiedName is the Go-facing rendering, e.g. "main.Word", matching what `%T` prints
	// for a program-defined type.
	QualifiedName string

	// BareName is the undecorated type identifier, e.g. "Word", matching the methodTable's
	// "<Type>.<Method>" key prefix.
	BareName string

	// PkgName is the defining package's short name, e.g. "main"; empty when the type has no
	// package (which should not happen for user declarations).
	PkgName string

	// PkgPath is the declaring package's import path, so a bytecode type descriptor can name
	// the type and a load can find the same pool column again.
	PkgPath string
}

type (
	// namedScalarBool is the pool's generic defined type for the bool kind.
	namedScalarBool[T any] bool

	// namedScalarInt is the pool's generic defined type for the int kind.
	namedScalarInt[T any] int

	// namedScalarInt8 is the pool's generic defined type for the int8 kind.
	namedScalarInt8[T any] int8

	// namedScalarInt16 is the pool's generic defined type for the int16 kind.
	namedScalarInt16[T any] int16

	// namedScalarInt32 is the pool's generic defined type for the int32 kind.
	namedScalarInt32[T any] int32

	// namedScalarInt64 is the pool's generic defined type for the int64 kind.
	namedScalarInt64[T any] int64

	// namedScalarUint is the pool's generic defined type for the uint kind.
	namedScalarUint[T any] uint

	// namedScalarUint8 is the pool's generic defined type for the uint8 kind.
	namedScalarUint8[T any] uint8

	// namedScalarUint16 is the pool's generic defined type for the uint16 kind.
	namedScalarUint16[T any] uint16

	// namedScalarUint32 is the pool's generic defined type for the uint32 kind.
	namedScalarUint32[T any] uint32

	// namedScalarUint64 is the pool's generic defined type for the uint64 kind.
	namedScalarUint64[T any] uint64

	// namedScalarUintptr is the pool's generic defined type for the uintptr kind.
	namedScalarUintptr[T any] uintptr

	// namedScalarFloat32 is the pool's generic defined type for the float32 kind.
	namedScalarFloat32[T any] float32

	// namedScalarFloat64 is the pool's generic defined type for the float64 kind.
	namedScalarFloat64[T any] float64

	// namedScalarComplex64 is the pool's generic defined type for the complex64 kind.
	namedScalarComplex64[T any] complex64

	// namedScalarComplex128 is the pool's generic defined type for the complex128 kind.
	namedScalarComplex128[T any] complex128

	// namedScalarString is the pool's generic defined type for the string kind.
	namedScalarString[T any] string
)

// namedScalarPoolRegistry is the process-global assignment state: which named basic type
// owns which pool entry.
type namedScalarPoolRegistry struct {
	// byKey maps the stable assignment key (pkgpath-qualified type string plus kind row) to
	// the assigned pool type; a nil value records an exhausted-pool decision so repeat
	// lookups stay cheap.
	byKey map[string]reflect.Type

	// info is the reverse map from an assigned pool type to its source-level identity.
	info map[reflect.Type]NamedScalarPoolInfo

	// authoritative records pool types whose info came from a real go/types package rather
	// than one synthesised from a path, so a later compile-side call can replace the guess.
	authoritative map[reflect.Type]struct{}

	// mu guards byKey, info, authoritative and nextColumn. Placed after the maps so the
	// pointerful prefix the GC scans stays small.
	mu sync.RWMutex

	// nextColumn holds the per-kind-row allocation cursor into namedScalarPoolColumns.
	nextColumn [namedScalarKindRows]int
}

// newNamedScalarPoolRegistry builds an empty registry with its maps ready for use.
//
// Returns *namedScalarPoolRegistry which owns no assignments yet.
func newNamedScalarPoolRegistry() *namedScalarPoolRegistry {
	return &namedScalarPoolRegistry{
		byKey:         make(map[string]reflect.Type),
		info:          make(map[reflect.Type]NamedScalarPoolInfo),
		authoritative: make(map[reflect.Type]struct{}),
		mu:            sync.RWMutex{},
		nextColumn:    [namedScalarKindRows]int{},
	}
}

// typeForResult is typeFor with exhaustion reported separately from "not poolable".
//
// Takes named (*types.Named) which is the named basic type.
// Takes keyCap (int) which bounds the registry's key count.
//
// Returns poolType (reflect.Type) which is nil when unavailable.
// Returns exhausted (bool) which is true when named is poolable but got no column.
func (r *namedScalarPoolRegistry) typeForResult(named *types.Named, keyCap int) (poolType reflect.Type, exhausted bool) {
	if existing := r.typeFor(named, keyCap, false); existing != nil {
		return existing, false
	}
	basic, ok := named.Underlying().(*types.Basic)
	if !ok {
		return nil, false
	}
	_, poolable := namedScalarRowForBasicKind(basic.Kind())
	return nil, poolable
}

// typeFor assigns or returns the pool type for a named basic type on this registry.
//
// Refuses new distinct keys once byKey holds keyCap of them so retention stays bounded.
//
// Takes named (*types.Named) whose underlying type must be a *types.Basic with a pool
// row.
// Takes keyCap (int) which is the maximum number of distinct keys byKey may retain.
// Takes synthetic (bool) which is true when named was rebuilt from an import path rather
// than type-checked, so its package name is a guess.
//
// Returns the assigned pool reflect.Type, or nil when the kind has no row, its columns
// are exhausted, or the key cap has been reached.
//
// Concurrency: acquires r.mu in read mode for the fast path, upgrading to write mode for
// new assignments.
func (r *namedScalarPoolRegistry) typeFor(named *types.Named, keyCap int, synthetic bool) reflect.Type {
	basic, ok := named.Underlying().(*types.Basic)
	if !ok {
		return nil
	}
	row, ok := namedScalarRowForBasicKind(basic.Kind())
	if !ok {
		return nil
	}

	key := types.TypeString(named, nil) + "\x00" + strconv.Itoa(row)

	r.mu.RLock()
	poolType, seen := r.byKey[key]
	_, settled := r.authoritative[poolType]
	r.mu.RUnlock()
	if seen && (synthetic || settled || poolType == nil) {
		return poolType
	}

	info := namedScalarPoolInfoFor(named)
	QualifiedName := info.QualifiedName

	r.mu.Lock()
	defer r.mu.Unlock()
	if poolType, seen = r.byKey[key]; seen {
		if poolType != nil && !synthetic {
			r.recordInfo(poolType, info, true)
		}
		return poolType
	}
	if len(r.byKey) >= keyCap {
		return nil
	}
	column := r.nextColumn[row]
	if column >= len(namedScalarPoolColumns) {
		r.byKey[key] = nil
		recordNamedScalarPoolExhaustion(QualifiedName, basic.Name(), len(namedScalarPoolColumns))
		return nil
	}
	r.nextColumn[row] = column + 1
	poolType = namedScalarPoolColumns[column][row]
	r.byKey[key] = poolType
	r.recordInfo(poolType, info, !synthetic)
	traceNamedScalarPoolAssignment(key, column, row, synthetic, QualifiedName)
	return poolType
}

// recordInfo stores a pool type's source-level identity under the registry write lock.
//
// Takes poolType (reflect.Type) which is the assigned pool type.
// Takes info (NamedScalarPoolInfo) which is the identity to store.
// Takes settled (bool) which is true when info came from a real go/types package and so
// must not be overwritten by a later synthesised one.
func (r *namedScalarPoolRegistry) recordInfo(poolType reflect.Type, info NamedScalarPoolInfo, settled bool) {
	r.info[poolType] = info
	if settled {
		r.authoritative[poolType] = struct{}{}
	}
}

// IsNamedScalarPoolType reports whether t is one of the pool's pre-declared named scalar
// types. Lock-free; safe from any goroutine.
//
// Takes t (reflect.Type) which is the candidate type; may be nil.
//
// Returns bool which is true when t is a pool type.
func IsNamedScalarPoolType(t reflect.Type) bool {
	if t == nil {
		return false
	}
	_, ok := namedScalarPoolMembers[t]
	return ok
}

// NamedScalarPoolExhaustions reports how many named basic types collapsed to their
// underlying kind. A non-zero count means type assertions on those types are no longer
// exact.
//
// Returns the process-wide exhaustion count.
func NamedScalarPoolExhaustions() int64 {
	return namedScalarPoolExhaustions.Load()
}

// NamedScalarPoolTypeFor assigns or returns the existing pool type for the named basic
// type.
//
// Takes named (*types.Named) whose underlying type must be a *types.Basic with a pool
// row. Callers must have excluded symbol-registered native types already.
//
// Returns the assigned pool reflect.Type, or nil when the kind has no row or the kind's
// columns are exhausted. Callers then keep the general-purpose collapsing behaviour.
func NamedScalarPoolTypeFor(named *types.Named) reflect.Type {
	return namedScalarPool.typeFor(named, maxNamedScalarPoolKeys, false)
}

// NamedScalarPoolTypeForPath returns the pool type for the named scalar declared as name
// in the package at pkgPath over the basic kind. Creates its column on first access so a
// bytecode load can recover the identity a compile recorded.
//
// Takes pkgPath (string) which is the declaring package's import path.
// Takes name (string) which is the type's declared name.
// Takes kind (types.BasicKind) which is the underlying basic kind.
//
// Returns reflect.Type which is nil when the kind has no pool row or the pool is full.
func NamedScalarPoolTypeForPath(pkgPath, name string, kind types.BasicKind) reflect.Type {
	pkg := types.NewPackage(pkgPath, path.Base(pkgPath))
	object := types.NewTypeName(token.NoPos, pkg, name, nil)
	named := types.NewNamed(object, types.Typ[kind], nil)
	return namedScalarPool.typeFor(named, maxNamedScalarPoolKeys, true)
}

// NamedScalarPoolTypeForResult is NamedScalarPoolTypeFor that also reports exhaustion. A
// nil type with exhausted true means the pool refused a poolable type, and callers must
// turn that into a compile error.
//
// Takes named (*types.Named) which is the named basic type.
//
// Returns poolType (reflect.Type) which is nil when unavailable.
// Returns exhausted (bool) which is true when the pool refused a poolable type.
func NamedScalarPoolTypeForResult(named *types.Named) (poolType reflect.Type, exhausted bool) {
	return namedScalarPool.typeForResult(named, maxNamedScalarPoolKeys)
}

// LookupNamedScalarPoolInfo returns the recorded source-level identity for a pool type.
//
// Takes t (reflect.Type) which is the candidate type. May be nil.
//
// Returns the identity record and true when t is an assigned pool type.
//
// Concurrency: acquires namedScalarPool.mu in read mode.
func LookupNamedScalarPoolInfo(t reflect.Type) (NamedScalarPoolInfo, bool) {
	if t == nil {
		return NamedScalarPoolInfo{}, false
	}
	if _, member := namedScalarPoolMembers[t]; !member {
		return NamedScalarPoolInfo{}, false
	}
	namedScalarPool.mu.RLock()
	defer namedScalarPool.mu.RUnlock()
	info, ok := namedScalarPool.info[t]
	return info, ok
}

// NamedScalarDisplayType renders t for user-facing messages.
//
// Substitutes the source-level qualified name for pool types so panics and %T read like
// Go's output (e.g. "main.Word" instead of the pool instantiation's own name).
//
// Takes t (reflect.Type) which may be nil.
//
// Returns the rendered type string; "<nil>" for nil.
func NamedScalarDisplayType(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}
	if info, ok := LookupNamedScalarPoolInfo(t); ok {
		return info.QualifiedName
	}
	return t.String()
}

// namedScalarPoolColumn builds the 17-kind column of pool types for one tag.
//
// Row order must match namedScalarRowForBasicKind.
//
// Returns the [namedScalarKindRows]reflect.Type column for Tag.
func namedScalarPoolColumn[Tag any]() [namedScalarKindRows]reflect.Type {
	return [namedScalarKindRows]reflect.Type{
		reflect.TypeFor[namedScalarBool[Tag]](),
		reflect.TypeFor[namedScalarInt[Tag]](),
		reflect.TypeFor[namedScalarInt8[Tag]](),
		reflect.TypeFor[namedScalarInt16[Tag]](),
		reflect.TypeFor[namedScalarInt32[Tag]](),
		reflect.TypeFor[namedScalarInt64[Tag]](),
		reflect.TypeFor[namedScalarUint[Tag]](),
		reflect.TypeFor[namedScalarUint8[Tag]](),
		reflect.TypeFor[namedScalarUint16[Tag]](),
		reflect.TypeFor[namedScalarUint32[Tag]](),
		reflect.TypeFor[namedScalarUint64[Tag]](),
		reflect.TypeFor[namedScalarUintptr[Tag]](),
		reflect.TypeFor[namedScalarFloat32[Tag]](),
		reflect.TypeFor[namedScalarFloat64[Tag]](),
		reflect.TypeFor[namedScalarComplex64[Tag]](),
		reflect.TypeFor[namedScalarComplex128[Tag]](),
		reflect.TypeFor[namedScalarString[Tag]](),
	}
}

// buildNamedScalarPoolMembers collects every pool type into a membership set.
//
// Returns the map keyed by pool reflect.Type.
func buildNamedScalarPoolMembers() map[reflect.Type]struct{} {
	members := make(map[reflect.Type]struct{}, len(namedScalarPoolColumns)*namedScalarKindRows)
	for columnIndex := range namedScalarPoolColumns {
		for _, poolType := range &namedScalarPoolColumns[columnIndex] {
			members[poolType] = struct{}{}
		}
	}
	return members
}

// namedScalarRowForBasicKind maps a go/types basic kind to its pool column row.
//
// Takes kind (types.BasicKind) which is the underlying basic kind.
//
// Returns the row index and true, or 0 and false when the kind has no pool row (untyped
// kinds, unsafe.Pointer).
func namedScalarRowForBasicKind(kind types.BasicKind) (int, bool) {
	switch kind {
	case types.Bool:
		return namedScalarRowBool, true
	case types.Int:
		return namedScalarRowInt, true
	case types.Int8:
		return namedScalarRowInt8, true
	case types.Int16:
		return namedScalarRowInt16, true
	case types.Int32:
		return namedScalarRowInt32, true
	case types.Int64:
		return namedScalarRowInt64, true
	case types.Uint:
		return namedScalarRowUint, true
	case types.Uint8:
		return namedScalarRowUint8, true
	case types.Uint16:
		return namedScalarRowUint16, true
	case types.Uint32:
		return namedScalarRowUint32, true
	case types.Uint64:
		return namedScalarRowUint64, true
	case types.Uintptr:
		return namedScalarRowUintptr, true
	case types.Float32:
		return namedScalarRowFloat32, true
	case types.Float64:
		return namedScalarRowFloat64, true
	case types.Complex64:
		return namedScalarRowComplex64, true
	case types.Complex128:
		return namedScalarRowComplex128, true
	case types.String:
		return namedScalarRowString, true
	default:
		return 0, false
	}
}

// recordNamedScalarPoolExhaustion counts a collapse and logs the first one.
//
// Takes QualifiedName (string) which is the type that collapsed.
// Takes kindName (string) which is the exhausted basic kind.
// Takes columns (int) which is the pool's column count for that kind.
func recordNamedScalarPoolExhaustion(QualifiedName, kindName string, columns int) {
	namedScalarPoolExhaustions.Add(1)
	if namedScalarPoolExhaustionWarned.Swap(true) {
		return
	}
	slog.Default().Warn("named-scalar pool exhausted; further named basic types of this kind collapse to their underlying type",
		slog.String("type", QualifiedName),
		slog.String("kind", kindName),
		slog.Int("columns", columns),
	)
}

// traceNamedScalarPoolAssignment logs a fresh column assignment when PIPIT_TRACE_POOL is
// set. A type reaching the pool under two keys silently creates two runtime types,
// surfacing as a reflect conversion failure.
//
// Takes key (string) which is the assignment key.
// Takes column (int) which is the column the key was given.
// Takes row (int) which is the kind row.
// Takes synthetic (bool) which records whether the caller rebuilt the type from a path.
// Takes qualifiedName (string) which is the type's display name.
func traceNamedScalarPoolAssignment(key string, column, row int, synthetic bool, qualifiedName string) {
	if !traceNamedScalarPoolEnabled {
		return
	}
	slog.Default().Debug("named-scalar pool column assigned",
		slog.String("key", key),
		slog.Int("column", column),
		slog.Int("row", row),
		slog.Bool("synthetic", synthetic),
		slog.String("type", qualifiedName))
}

// namedScalarPoolInfoFor reads a named basic type's source-level identity.
//
// Takes named (*types.Named) which is the type being assigned a column.
//
// Returns NamedScalarPoolInfo with empty package fields when the type has no package (a
// type built for a probe, or one of the predeclared basics).
func namedScalarPoolInfoFor(named *types.Named) NamedScalarPoolInfo {
	info := NamedScalarPoolInfo{
		QualifiedName: types.TypeString(named, func(p *types.Package) string { return p.Name() }),
		BareName:      named.Obj().Name(),
		PkgName:       "",
		PkgPath:       "",
	}
	if pkg := named.Obj().Pkg(); pkg != nil {
		info.PkgName = pkg.Name()
		info.PkgPath = pkg.Path()
	}
	return info
}
