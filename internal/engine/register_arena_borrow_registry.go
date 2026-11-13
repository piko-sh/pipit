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
	"maps"
	"reflect"
	"runtime"
	"slices"
	"sync/atomic"
)

const (
	// pointerAlignShift drops the low bits of a code or type pointer, which alignment leaves
	// zero, before the pointer indexes a cache or picks a shard.
	pointerAlignShift = 4

	// pointerClassPointerBits is the widest code pointer the class cache holds: a slot packs
	// the pointer above a three-bit class tag.
	pointerClassPointerBits = 61

	// pointerClassMaxClass is the largest class the cache's tag can hold; tag zero marks an
	// empty slot.
	pointerClassMaxClass = 6
)

const (
	// borrowRankBorrows is borrowStrictness's least conservative rank, for BorrowBorrows and
	// BorrowReadsOnly.
	borrowRankBorrows = iota

	// borrowRankAliases is borrowStrictness's rank for BorrowResultAliases.
	borrowRankAliases

	// borrowRankRetains is borrowStrictness's rank for BorrowRetains.
	borrowRankRetains

	// borrowRankUnknown is borrowStrictness's most conservative rank, for BorrowUnknown and
	// unrecognised classes.
	borrowRankUnknown
)

// pointerClassSlots is the cache's size; a power of two.
const pointerClassSlots = 512

// pointerClassCache maps a code pointer to a class in [0, 6]. A slot holds pointer<<3 |
// class+1, or zero when empty; code pointers lie below 2^61.
type pointerClassCache struct {
	// slots caches packed native function addresses and their borrow classes.
	slots [pointerClassSlots]atomic.Uint64
}

// BorrowClass is what a native function does with the arguments it is handed.
type BorrowClass uint8

const (
	// BorrowUnknown is the default.
	//
	// Nothing is known, so an arena argument revokes.
	BorrowUnknown BorrowClass = iota

	// BorrowBorrows keeps no reference to an argument once the call returns.
	BorrowBorrows

	// BorrowResultAliases may return a result that aliases an argument, but keeps no other
	// reference.
	BorrowResultAliases

	// BorrowRetains may keep a reference to an argument beyond the call.
	BorrowRetains

	// BorrowReadsOnly borrows (keeps no reference once the call returns, and returns no
	// result that aliases an argument) and never writes through an argument. A []byte(s)
	// argument of such a native may be the string's own bytes.
	BorrowReadsOnly
)

// borrowRegistryAliasAware names the hand-reviewed registry that separates borrowing,
// result-aliasing and retaining functions.
const borrowRegistryAliasAware = "alias-aware"

var (
	// readsOnlyFast caches the built-in reads-only classification (1 reads-only, 0 not) of
	// natives no declaration applies to. It is consulted only while no native has ever been
	// demoted (readsOnlyDemotions), so a demotion never races a stale entry.
	readsOnlyFast pointerClassCache

	// readsOnlyDemotions counts demotions to readsOnlyDishonest since the last reset.
	readsOnlyDemotions atomic.Int64
)

// nativeBorrowRegistry classes native functions by their fully qualified name (as
// runtime.FuncForPC reports it.
type nativeBorrowRegistry struct {
	// classes maps a qualified function name to its class.
	classes map[string]BorrowClass

	// gcTags maps a qualified function name to the class implied by gc's escape tags, for
	// the functions whose tags were checked.
	gcTags map[string]BorrowClass

	// byPointer caches classifications by function code pointer.
	byPointer typedCache[uintptr, BorrowClass]

	// name identifies the registry in reports.
	name string

	// fast is a lock-free direct-mapped cache in front of byPointer.
	fast pointerClassCache
}

// gcEscapeTagClasses records the classes gc's escape tags imply for the functions checked
// with `go build -a -gcflags=strings=-m -gcflags=errors=-m` on go1.27.1.
var gcEscapeTagClasses = map[string]BorrowClass{
	"strings.TrimSpace": BorrowResultAliases,
	"strings.ToUpper":   BorrowResultAliases,
	"strings.Contains":  BorrowBorrows,
	"strings.Split":     BorrowRetains,
	"strings.Fields":    BorrowRetains,
	"errors.New":        BorrowRetains,
}

// aliasAwareStdlibClasses is the hand-reviewed registry, by Go semantics rather than gc's
// tags.
var aliasAwareStdlibClasses = map[string]BorrowClass{
	"strings.Contains":               BorrowBorrows,
	"strings.HasPrefix":              BorrowBorrows,
	"strings.HasSuffix":              BorrowBorrows,
	"strings.Index":                  BorrowBorrows,
	"strings.EqualFold":              BorrowBorrows,
	"strings.Count":                  BorrowBorrows,
	"strings.Compare":                BorrowBorrows,
	"strings.Clone":                  BorrowBorrows,
	"strings.ToUpper":                BorrowResultAliases,
	"strings.ToLower":                BorrowResultAliases,
	"strings.TrimSpace":              BorrowResultAliases,
	"strings.Trim":                   BorrowResultAliases,
	"strings.TrimPrefix":             BorrowResultAliases,
	"strings.TrimSuffix":             BorrowResultAliases,
	"strings.Split":                  BorrowResultAliases,
	"strings.Fields":                 BorrowResultAliases,
	"strings.Join":                   BorrowResultAliases,
	"strings.Replace":                BorrowResultAliases,
	"strings.ReplaceAll":             BorrowResultAliases,
	"strings.Repeat":                 BorrowResultAliases,
	"strconv.Atoi":                   BorrowBorrows,
	"strconv.Itoa":                   BorrowBorrows,
	"strconv.ParseInt":               BorrowBorrows,
	"strconv.ParseFloat":             BorrowBorrows,
	"strconv.ParseBool":              BorrowBorrows,
	"strconv.FormatInt":              BorrowBorrows,
	"strconv.Quote":                  BorrowBorrows,
	"errors.New":                     BorrowRetains,
	"errors.Is":                      BorrowBorrows,
	"errors.Unwrap":                  BorrowResultAliases,
	"fmt.Sprintf":                    BorrowBorrows,
	"fmt.Sprint":                     BorrowBorrows,
	"fmt.Sprintln":                   BorrowBorrows,
	"fmt.Println":                    BorrowBorrows,
	"fmt.Printf":                     BorrowBorrows,
	"fmt.Print":                      BorrowBorrows,
	"fmt.Errorf":                     BorrowRetains,
	"bytes.Equal":                    BorrowBorrows,
	"bytes.Contains":                 BorrowBorrows,
	"bytes.HasPrefix":                BorrowBorrows,
	"bytes.Index":                    BorrowBorrows,
	"bytes.Clone":                    BorrowBorrows,
	"bytes.TrimSpace":                BorrowResultAliases,
	"bytes.Split":                    BorrowResultAliases,
	"bytes.Fields":                   BorrowResultAliases,
	"bytes.ToUpper":                  BorrowResultAliases,
	"bytes.NewBuffer":                BorrowRetains,
	"bytes.NewBufferString":          BorrowBorrows,
	"sort.Strings":                   BorrowBorrows,
	"sort.Ints":                      BorrowBorrows,
	"sort.Float64s":                  BorrowBorrows,
	"sort.Slice":                     BorrowBorrows,
	"sort.SliceStable":               BorrowBorrows,
	"sort.SearchInts":                BorrowBorrows,
	"sort.SearchStrings":             BorrowBorrows,
	"strings.(*Builder).Write":       BorrowBorrows,
	"strings.(*Builder).WriteString": BorrowBorrows,
	"bytes.(*Buffer).Write":          BorrowBorrows,
	"bytes.(*Buffer).WriteString":    BorrowBorrows,
}

// methodValueProbe gives methodValueCallPC a method value to read the pointer of.
type methodValueProbe struct{}

// methodValueCallPC is the code pointer of every method value reflect makes.
var methodValueCallPC = reflect.ValueOf(methodValueProbe{}).Method(0).Pointer()

// slot returns the slot a pointer maps to.
//
// Functions are at least 16-byte aligned, so the low four bits carry nothing.
//
// Takes pointer (uintptr) which is the code pointer.
//
// Returns *atomic.Uint64 which is the slot.
func (c *pointerClassCache) slot(pointer uintptr) *atomic.Uint64 {
	return &c.slots[(pointer>>pointerAlignShift)&(pointerClassSlots-1)]
}

// load returns the cached class of pointer.
//
// Takes pointer (uintptr) which is the code pointer.
//
// Returns uint8 which is the class.
// Returns bool which is false on a miss.
func (c *pointerClassCache) load(pointer uintptr) (uint8, bool) {
	word := c.slot(pointer).Load()
	if word == 0 || uintptr(word>>3) != pointer {
		return 0, false
	}
	return uint8(word&7) - 1, true
}

// store caches class for pointer, unless the pointer does not fit.
//
// Takes pointer (uintptr) which is the code pointer.
// Takes class (uint8) which is the class, at most 6.
func (c *pointerClassCache) store(pointer uintptr, class uint8) {
	if pointer>>pointerClassPointerBits != 0 || class > pointerClassMaxClass {
		return
	}
	c.slot(pointer).Store(uint64(pointer)<<3 | uint64(class+1))
}

// clear empties the cache.
func (c *pointerClassCache) clear() {
	for index := range c.slots {
		c.slots[index].Store(0)
	}
}

// String returns the class's name.
//
// Returns string which is the name.
func (c BorrowClass) String() string {
	switch c {
	case BorrowBorrows:
		return "borrows"
	case BorrowResultAliases:
		return "resultAliases"
	case BorrowRetains:
		return "retains"
	case BorrowReadsOnly:
		return "readsOnly"
	default:
		return "unknown"
	}
}

// handOffCause maps the class to its native-argument hand-off cause.
//
// Returns HandOffCause which is the cause an arena argument is counted under.
func (c BorrowClass) handOffCause() HandOffCause {
	switch c {
	case BorrowBorrows, BorrowReadsOnly:
		return handOffNativeArgBorrows
	case BorrowResultAliases:
		return handOffNativeArgResultAliases
	case BorrowRetains:
		return handOffNativeArgRetains
	default:
		return handOffNativeArgUnknown
	}
}

// Name returns the registry's name.
//
// Returns string which is the name.
func (r *nativeBorrowRegistry) Name() string {
	return r.name
}

// Class returns the class registered for a qualified function name.
//
// Takes name (string) which is the qualified name.
//
// Returns BorrowClass which is BorrowUnknown when the name is not listed.
func (r *nativeBorrowRegistry) Class(name string) BorrowClass {
	return r.classes[name]
}

// classify returns the class of a native function value.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns BorrowClass which is BorrowUnknown for anything not a listed function.
func (r *nativeBorrowRegistry) classify(function reflect.Value) BorrowClass {
	if len(r.classes) == 0 || !function.IsValid() || function.Kind() != reflect.Func || function.IsNil() {
		return BorrowUnknown
	}
	pointer := function.Pointer()
	if pointer == methodValueCallPC {
		return r.classifyMethodValue(function)
	}
	if class, ok := r.fast.load(pointer); ok {
		return BorrowClass(class)
	}
	if class, ok := r.byPointer.load(pointer); ok {
		r.fast.store(pointer, uint8(class))
		return class
	}
	class := BorrowUnknown
	if info := runtime.FuncForPC(pointer); info != nil {
		class = r.classes[info.Name()]
	}
	r.byPointer.store(pointer, class)
	r.fast.store(pointer, uint8(class))
	return class
}

// gcTagDisagreements lists the registered functions whose class is less conservative than
// the class gc's escape tags imply, as "name: registry class, gc class".
//
// Returns []string which is sorted.
func (r *nativeBorrowRegistry) gcTagDisagreements() []string {
	var disagreements []string
	for name, gcClass := range r.gcTags {
		class, listed := r.classes[name]
		if !listed || borrowStrictness(class) >= borrowStrictness(gcClass) {
			continue
		}
		disagreements = append(disagreements, fmt.Sprintf("%s: %s, gc %s", name, class, gcClass))
	}
	slices.Sort(disagreements)
	return disagreements
}

// borrowStrictness orders classes from least to most conservative.
//
// Takes class (BorrowClass) which is the class to rank.
//
// Returns int which is the rank.
func borrowStrictness(class BorrowClass) int {
	switch class {
	case BorrowBorrows, BorrowReadsOnly:
		return borrowRankBorrows
	case BorrowResultAliases:
		return borrowRankAliases
	case BorrowRetains:
		return borrowRankRetains
	default:
		return borrowRankUnknown
	}
}

// aliasAwareBorrowRegistry returns the hand-reviewed stdlib registry.
//
// Returns *nativeBorrowRegistry which is the alias-aware registry.
func aliasAwareBorrowRegistry() *nativeBorrowRegistry {
	return newBorrowRegistry(borrowRegistryAliasAware, maps.Clone(aliasAwareStdlibClasses))
}

// newBorrowRegistry builds a registry with the gc escape-tag table attached.
//
// Takes name (string) which identifies the registry.
// Takes classes (map[string]BorrowClass) which may be nil.
//
// Returns *nativeBorrowRegistry which is ready for use.
func newBorrowRegistry(name string, classes map[string]BorrowClass) *nativeBorrowRegistry {
	return &nativeBorrowRegistry{name: name, classes: classes, gcTags: gcEscapeTagClasses, byPointer: typedCache[uintptr, BorrowClass]{}, fast: pointerClassCache{}}
}

// Probe is methodValueProbe's method.
func (methodValueProbe) Probe() {}
