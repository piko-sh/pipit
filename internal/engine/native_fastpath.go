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
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// fastPathDispatcherTag labels RecordFastPathTypeMismatch records so callers can
	// attribute mismatches to the fastpath dispatch layer.
	fastPathDispatcherTag = "fastpath dispatcher"

	// digestBytesMD5 is the byte width of an MD5 digest (also the UUID length). Used by
	// fpDispatchBytesArr16 to size the arena allocation and as the [16]byte type parameter.
	digestBytesMD5 = 16

	// digestBytesSHA1 is the byte width of a SHA-1 digest. Used by fpDispatchBytesArr20.
	digestBytesSHA1 = 20

	// digestBytesSHA256 is the byte width of a SHA-256 digest. Used by fpDispatchBytesArr32.
	digestBytesSHA256 = 32

	// digestBytesSHA512 is the byte width of a SHA-512 digest. Used by fpDispatchBytesArr64.
	digestBytesSHA512 = 64
)

const (
	// fastPathTagNone indicates that no fast-path dispatch is available for this call site
	// after classification has been attempted. The zero value (unprobed) is implicit - any
	// call site starts at 0 before classification.
	fastPathTagNone nativeFastPathTag = iota + 1

	// fastPathTagStringString tags func(string) string.
	fastPathTagStringString

	// fastPathTagStringInt tags func(string) int.
	fastPathTagStringInt

	// fastPathTagStringBool tags func(string) bool.
	fastPathTagStringBool

	// fastPathTagStringRuneBool tags func(string, int32) bool.
	fastPathTagStringRuneBool

	// fastPathTagStringRuneInt tags func(string, int32) int.
	fastPathTagStringRuneInt

	// fastPathTagString2Bool tags func(string, string) bool.
	fastPathTagString2Bool

	// fastPathTagString2String tags func(string, string) string.
	fastPathTagString2String

	// fastPathTagString2Int tags func(string, string) int.
	fastPathTagString2Int

	// fastPathTagString3String tags func(string, string, string) string.
	fastPathTagString3String

	// fastPathTagIntString tags func(int) string.
	fastPathTagIntString

	// fastPathTagIntInt tags func(int) int.
	fastPathTagIntInt

	// fastPathTagIntBool tags func(int) bool.
	fastPathTagIntBool

	// fastPathTagInt2Int tags func(int, int) int.
	fastPathTagInt2Int

	// fastPathTagInt2Bool tags func(int, int) bool.
	fastPathTagInt2Bool

	// fastPathTagInt2String tags func(int, int) string.
	fastPathTagInt2String

	// fastPathTagInt64IntString tags func(int64, int) string.
	fastPathTagInt64IntString

	// fastPathTagStringIntError tags func(string) (int, error).
	fastPathTagStringIntError

	// fastPathTagFloat64Float64 tags func(float64) float64.
	fastPathTagFloat64Float64

	// fastPathTagFloat642Float64 tags func(float64, float64) float64.
	fastPathTagFloat642Float64

	// fastPathTagAnyBool tags func(any) bool.
	fastPathTagAnyBool

	// fastPathTagAnyString tags func(any) string.
	fastPathTagAnyString

	// fastPathTagAnyInt tags func(any) int.
	fastPathTagAnyInt

	// fastPathTagAnyInt64 tags func(any) int64.
	fastPathTagAnyInt64

	// fastPathTagAnyFloat64 tags func(any) float64.
	fastPathTagAnyFloat64

	// fastPathTagAny2Any tags func(any, any) any.
	fastPathTagAny2Any

	// fastPathTagRetString tags func() string.
	fastPathTagRetString

	// fastPathTagRetBool tags func() bool.
	fastPathTagRetBool

	// fastPathTagRetInt tags func() int.
	fastPathTagRetInt

	// fastPathTagRetInt64 tags func() int64.
	fastPathTagRetInt64

	// fastPathTagRetFloat64 tags func() float64.
	fastPathTagRetFloat64

	// fastPathTagRetError tags func() error.
	fastPathTagRetError

	// fastPathTagVoid tags func().
	fastPathTagVoid

	// fastPathTagVoidString tags func(string).
	fastPathTagVoidString

	// fastPathTagVoidInt tags func(int).
	fastPathTagVoidInt

	// fastPathTagVoidInt64 tags func(int64).
	fastPathTagVoidInt64

	// fastPathTagVoidBool tags func(bool).
	fastPathTagVoidBool

	// fastPathTagVoidString2 tags func(string, string).
	fastPathTagVoidString2

	// fastPathTagStringError tags func(string) error.
	fastPathTagStringError

	// fastPathTagSprintfString tags func(string, ...any) string.
	fastPathTagSprintfString

	// fastPathTagSprintfError tags func(string, ...any) error.
	fastPathTagSprintfError

	// fastPathTagSprintVarargs tags func(...any) string.
	fastPathTagSprintVarargs

	// fastPathTagBytesArr32 tags func([]byte) [32]byte (SHA-256 and similar fixed-output
	// hashes).
	fastPathTagBytesArr32

	// fastPathTagBytesArr16 tags func([]byte) [16]byte. Covers MD5 (md5.Sum) and UUID-shape
	// hashes via the same arena-routed mechanism as fastPathTagBytesArr32.
	fastPathTagBytesArr16

	// fastPathTagBytesArr20 tags func([]byte) [20]byte. Covers SHA-1 (sha1.Sum) via the same
	// arena-routed mechanism.
	fastPathTagBytesArr20

	// fastPathTagBytesArr64 tags func([]byte) [64]byte. Covers SHA-512 (sha512.Sum512) via
	// the same arena-routed mechanism.
	fastPathTagBytesArr64

	// pipitIDFieldPrefix is the prefix of the sentinel field the compiler injects on every
	// reflect.StructOf-synthesised named struct. Aliased from isa so the compiler that
	// writes the field and the engine that recognises it cannot disagree.
	pipitIDFieldPrefix = isa.SynthesisedIDFieldPrefix
)

var (
	// nativeFastPathNone is the sentinel value indicating that a call site has no fast-path
	// specialisation available.
	nativeFastPathNone = &struct{}{}

	// nativeFastPathNoneEntry is the shared immutable entry stored at a call site once
	// classification finds no fast path. Sharing one instance avoids an allocation each time
	// a non-specialised site is probed.
	nativeFastPathNoneEntry = &nativeFastPathEntry{fn: nativeFastPathNone, receiverAddr: 0, tag: 0}

	// fastPathTagByType maps concrete function reflect.Types to their corresponding
	// fast-path tag for O(1) classification.
	fastPathTagByType = map[reflect.Type]nativeFastPathTag{
		reflect.TypeFor[func(string) string]():                 fastPathTagStringString,
		reflect.TypeFor[func(string) int]():                    fastPathTagStringInt,
		reflect.TypeFor[func(string) bool]():                   fastPathTagStringBool,
		reflect.TypeFor[func(string, int32) bool]():            fastPathTagStringRuneBool,
		reflect.TypeFor[func(string, int32) int]():             fastPathTagStringRuneInt,
		reflect.TypeFor[func(string, string) bool]():           fastPathTagString2Bool,
		reflect.TypeFor[func(string, string) string]():         fastPathTagString2String,
		reflect.TypeFor[func(string, string) int]():            fastPathTagString2Int,
		reflect.TypeFor[func(string, string, string) string](): fastPathTagString3String,
		reflect.TypeFor[func(int) string]():                    fastPathTagIntString,
		reflect.TypeFor[func(int) int]():                       fastPathTagIntInt,
		reflect.TypeFor[func(int) bool]():                      fastPathTagIntBool,
		reflect.TypeFor[func(int, int) int]():                  fastPathTagInt2Int,
		reflect.TypeFor[func(int, int) bool]():                 fastPathTagInt2Bool,
		reflect.TypeFor[func(int, int) string]():               fastPathTagInt2String,
		reflect.TypeFor[func(int64, int) string]():             fastPathTagInt64IntString,
		reflect.TypeFor[func(string) (int, error)]():           fastPathTagStringIntError,
		reflect.TypeFor[func(float64) float64]():               fastPathTagFloat64Float64,
		reflect.TypeFor[func(float64, float64) float64]():      fastPathTagFloat642Float64,
		reflect.TypeFor[func(any) bool]():                      fastPathTagAnyBool,
		reflect.TypeFor[func(any) string]():                    fastPathTagAnyString,
		reflect.TypeFor[func(any) int]():                       fastPathTagAnyInt,
		reflect.TypeFor[func(any) int64]():                     fastPathTagAnyInt64,
		reflect.TypeFor[func(any) float64]():                   fastPathTagAnyFloat64,
		reflect.TypeFor[func(any, any) any]():                  fastPathTagAny2Any,
		reflect.TypeFor[func() string]():                       fastPathTagRetString,
		reflect.TypeFor[func() bool]():                         fastPathTagRetBool,
		reflect.TypeFor[func() int]():                          fastPathTagRetInt,
		reflect.TypeFor[func() int64]():                        fastPathTagRetInt64,
		reflect.TypeFor[func() float64]():                      fastPathTagRetFloat64,
		reflect.TypeFor[func() error]():                        fastPathTagRetError,
		reflect.TypeFor[func()]():                              fastPathTagVoid,
		reflect.TypeFor[func(string)]():                        fastPathTagVoidString,
		reflect.TypeFor[func(int)]():                           fastPathTagVoidInt,
		reflect.TypeFor[func(int64)]():                         fastPathTagVoidInt64,
		reflect.TypeFor[func(bool)]():                          fastPathTagVoidBool,
		reflect.TypeFor[func(string, string)]():                fastPathTagVoidString2,
		reflect.TypeFor[func(string) error]():                  fastPathTagStringError,
		reflect.TypeFor[func(string, ...any) string]():         fastPathTagSprintfString,
		reflect.TypeFor[func(string, ...any) error]():          fastPathTagSprintfError,
		reflect.TypeFor[func(...any) string]():                 fastPathTagSprintVarargs,
		reflect.TypeFor[func([]byte) [32]byte]():               fastPathTagBytesArr32,
		reflect.TypeFor[func([]byte) [16]byte]():               fastPathTagBytesArr16,
		reflect.TypeFor[func([]byte) [20]byte]():               fastPathTagBytesArr20,
		reflect.TypeFor[func([]byte) [64]byte]():               fastPathTagBytesArr64,
	}

	// fastPathDispatchTable is an array of dispatch functions indexed by nativeFastPathTag.
	// Populated at init time.
	fastPathDispatchTable [fastPathTagBytesArr64 + 1]fastPathDispatcher

	// bytesArr32ABIType caches the *abi.Type pointer for [32]byte so the fast-path
	// dispatcher can construct the result reflect.Value via unsafeNewAt without
	// re-extracting the ABI type on every call.
	bytesArr32ABIType = reflectValueABIType(reflect.TypeFor[[32]byte]())

	// bytesArr16ABIType caches the *abi.Type pointer for [16]byte (MD5 / UUID byte size).
	bytesArr16ABIType = reflectValueABIType(reflect.TypeFor[[16]byte]())

	// bytesArr20ABIType caches the *abi.Type pointer for [20]byte (SHA-1 digest size).
	bytesArr20ABIType = reflectValueABIType(reflect.TypeFor[[20]byte]())

	// bytesArr64ABIType caches the *abi.Type pointer for [64]byte (SHA-512 digest size).
	bytesArr64ABIType = reflectValueABIType(reflect.TypeFor[[64]byte]())
)

var (
	// pipitSynthesisedTypeCache memoises type classification results, keyed by reflect.Type.
	// Collapses a per-call field walk into a single map load.
	pipitSynthesisedTypeCache sync.Map

	// pipitContainsSynthCache memoises typeContainsPipitSynthesised: whether a fmt walk of a
	// type reaches a pipit-synthesised struct through slice/array/map/struct nesting (never
	// through a pointer). Keyed by the same interned reflect.Type values as
	// pipitSynthesisedTypeCache and written once per type.
	pipitContainsSynthCache sync.Map
)

// nativeFastPathEntry is the immutable per-site classification cache. Published through
// an atomic pointer so a sibling goroutine cannot read a torn two-word fn interface.
type nativeFastPathEntry struct {
	// fn is the extracted native function value to dispatch, or nativeFastPathNone when
	// classification found no fast path.
	fn any

	// receiverAddr is the address of the method receiver fn was bound to, used to invalidate
	// the cache when the same method is called on a different receiver. Zero for non-method
	// sites.
	receiverAddr uintptr

	// tag selects which fast-path dispatcher handles fn.
	tag nativeFastPathTag
}

// nativeFastPathTag identifies which fast-path case matched so that subsequent calls can
// dispatch via a uint8 jump table instead of the full interface type switch.
type nativeFastPathTag uint8

// fastPathDispatcher is the signature for individual fast-path dispatch functions, each
// handling exactly one tag case.
type fastPathDispatcher func(vm *VM, cached any, site *program.CallSite, registers *Registers)

// reuseVarArgsBuf returns a []any slice of length n, reusing the pre-allocated buffer on
// the CallSite when possible.
//
// Takes cs (*CallSite) which owns the reusable buffer.
// Takes n (int) which is the required slice length.
//
// Returns a []any slice of the requested length, either resliced from the existing buffer
// or freshly allocated if the buffer capacity is insufficient.
func reuseVarArgsBuf(cs *program.CallSite, n int) []any {
	if cap(cs.VariadicArgumentsBuffer) >= n {
		return cs.VariadicArgumentsBuffer[:n]
	}

	cs.VariadicArgumentsBuffer = make([]any, n)

	return cs.VariadicArgumentsBuffer
}

// classifyNativeFastPath determines the fast-path tag for a given function value by
// looking up its reflect.Type in the dispatch map.
//
// Takes v (any) which is the native function value to classify.
//
// Returns the matching nativeFastPathTag, or fastPathTagNone if no fast-path
// specialisation exists for the function's type signature.
func classifyNativeFastPath(v any) nativeFastPathTag {
	tag, ok := fastPathTagByType[reflect.TypeOf(v)]
	if !ok {
		return fastPathTagNone
	}

	return tag
}

// tryNativeFastPath attempts to call a native function via a direct type assertion on the
// already-extracted function value, bypassing reflect.Value.Call().
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes site (*CallSite) which is the call site metadata including argument and return
// register locations.
// Takes v (any) which is the native function value to dispatch.
// Takes registers (*Registers) which is the VM register file to read arguments from and
// write results to.
//
// Returns true and the matched tag if the fast path was taken, or false and
// fastPathTagNone if no fast-path specialisation is available. The third return is the
// recovered panic from the native call (e.g. sync.Mutex.Unlock on an unlocked mutex), nil
// if none.
func tryNativeFastPath(vm *VM, site *program.CallSite, v any, registers *Registers) (bool, nativeFastPathTag, any) {
	tag := classifyNativeFastPath(v)
	if tag == fastPathTagNone {
		atomic.StorePointer(&site.NativeFastPath, unsafe.Pointer(nativeFastPathNoneEntry))
		return false, fastPathTagNone, nil
	}

	if site.IsEllipsisSpread {
		return false, fastPathTagNone, nil
	}

	if siteArgsRequireInterfaceAdapter(vm, site, registers) {
		return false, fastPathTagNone, nil
	}

	panicValue := dispatchNativeFastPathTagged(vm, tag, v, site, registers)

	return true, tag, panicValue
}

// siteArgsRequireInterfaceAdapter reports whether any of the site's general-bank
// arguments is a pipit-synthesised value that needs adapter wrapping before it can
// satisfy the native function's interface parameter. Such args bypass the fast path so
// the slow path can build the adapter.
//
// Takes vm (*VM) which owns the method table consulted for adapter eligibility.
// Takes site (*CallSite) which holds the per-parameter interface flags and per-argument
// static type names.
// Takes registers (*Registers) which holds the live argument values.
//
// Returns true when at least one argument needs adapter wrapping.
func siteArgsRequireInterfaceAdapter(vm *VM, site *program.CallSite, registers *Registers) bool {
	for i, argument := range site.Arguments {
		if parameterSlotIsInterface(site, i) {
			if argument.Kind == isa.RegisterGeneral {
				value := registers.General[argument.Register]
				if value.IsValid() && argumentIsPipitSynthesised(value) {
					return true
				}
			}
		}
		if argStaticTypeNameRequiresAdapter(vm, site, i) {
			return true
		}
		if argDynamicValueRequiresAdapter(vm, argument, registers) {
			return true
		}
	}
	return false
}

// parameterSlotIsInterface reports whether the i-th fixed parameter of the callee has
// interface kind. Variadic tails are not recorded in parameterInterfaceFlags - they're
// handled separately via the `_pipitID_` sentinel check in the fast-path `any`-tail
// dispatchers.
//
// Takes site (*CallSite) which carries the compile-time flag table.
// Takes argumentIndex (int) which is the position to inspect.
//
// Returns false when no flag table exists, the position is past the table, or the slot is
// concrete.
func parameterSlotIsInterface(site *program.CallSite, argumentIndex int) bool {
	if argumentIndex >= len(site.ParameterInterfaceFlags) {
		return false
	}
	return site.ParameterInterfaceFlags[argumentIndex]
}

// argumentIsPipitSynthesised peels one layer of interface boxing and returns true when
// the underlying reflect.Type carries pipit's `_pipitID_` sentinel field. Used by the
// fast-path interface-slot check to decide whether to fall through to the slow path's
// adapter builder.
//
// Takes value (reflect.Value) which is the live argument value.
//
// Returns true when the value's reflect.Type is pipit-synthesised.
func argumentIsPipitSynthesised(value reflect.Value) bool {
	probe := value
	if probe.Kind() == reflect.Interface && !probe.IsNil() {
		probe = probe.Elem()
	}
	if !probe.IsValid() {
		return false
	}
	return isPipitSynthesisedReflectType(probe.Type())
}

// argStaticTypeNameRequiresAdapter checks the Compiler-recorded static type name for
// argument i against the interface-adapter method table.
//
// Takes vm (*VM) which is the active interpreter instance.
// Takes site (*CallSite) which carries the Compiler-recorded static names.
// Takes argumentIndex (int) which is the zero-based argument position.
//
// Returns true when the static name is registered as needing the adapter; false when no
// static name is recorded at the index.
func argStaticTypeNameRequiresAdapter(vm *VM, site *program.CallSite, argumentIndex int) bool {
	if argumentIndex >= len(site.ArgumentStaticTypeNames) {
		return false
	}
	staticName := site.ArgumentStaticTypeNames[argumentIndex]
	if staticName == "" {
		return false
	}
	return typeNameHasInterfaceAdapter(vm, staticName)
}

// argDynamicValueRequiresAdapter inspects the live general-bank register for a
// pipit-synthesised reflect.Type whose source-level name has a registered adapter method.
//
// Takes vm (*VM) which is the active interpreter instance.
// Takes argument (VarLocation) which is the location of the argument register to inspect.
// Takes registers (*Registers) which is the active register bank containing the value.
//
// Returns true when the dynamic value resolves to a pipit-synthesised type with a
// registered adapter; false for non-general-bank arguments or non-synthesised types.
func argDynamicValueRequiresAdapter(vm *VM, argument program.VarLocation, registers *Registers) bool {
	if argument.Kind != isa.RegisterGeneral {
		return false
	}
	value := registers.General[argument.Register]
	if !value.IsValid() {
		return false
	}
	probe := value
	if probe.Kind() == reflect.Interface && !probe.IsNil() {
		probe = probe.Elem()
	}
	if !isPipitSynthesisedReflectType(probe.Type()) {
		return false
	}
	typeName, ok := pipitTypeName(vm, probe)
	if !ok {
		return false
	}
	return typeNameHasUsableInterfaceAdapter(vm, typeName, probe.Kind() == reflect.Pointer)
}

// typeNameHasUsableInterfaceAdapter reports whether the named source-level type has at
// least one registered method that is usable given the value's pointer/value shape.
//
// Takes vm (*VM) which owns the method table.
// Takes typeName (string) which is the source-level type name.
// Takes isPointerValue (bool) which is true when the runtime value is a pointer (`*T`);
// false when it is the value type (`T`).
//
// Returns true when at least one method satisfies the receiver-kind rule for the value
// shape.
func typeNameHasUsableInterfaceAdapter(vm *VM, typeName string, isPointerValue bool) bool {
	if vm == nil || typeName == "" {
		return false
	}
	for _, methodName := range []string{".Error", ".String", ".MarshalJSON"} {
		methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+methodName)
		if !ok {
			continue
		}
		callee, _, ok := resolveAdapterCallee(vm, methodRoot, methodIndex)
		if !ok || callee == nil {
			continue
		}
		if callee.IsPointerReceiver && !isPointerValue {
			continue
		}
		return true
	}
	return false
}

// siteStaticTypeName returns the Compiler-recorded bare source-level type name of one
// argument, or "" when the site records none for it.
//
// Takes site (*CallSite) which carries the recorded names.
// Takes argumentIndex (int) which is the zero-based argument position.
//
// Returns string which is the bare named type, or "".
func siteStaticTypeName(site *program.CallSite, argumentIndex int) string {
	if argumentIndex < 0 || argumentIndex >= len(site.ArgumentStaticTypeNames) {
		return ""
	}
	return site.ArgumentStaticTypeNames[argumentIndex]
}

// adaptArgForNativeAny prepares an `any`-typed argument for fast-path calls, applying
// fmt/adapter wrapping for pipit-synthesised structs when the callee is a genuinely
// native Go function while letting MakeFunc closures pass through unchanged to preserve
// receiver identity.
//
// Takes vm (*VM) which is the active interpreter.
// Takes cached (any) which is the unwrapped native function value the dispatcher already
// type-asserted.
// Takes raw (any) which is the just-read argument from readAnyArg.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns the argument unchanged when no wrapping is needed; a pipitFmtValue or adapter
// when the wrap applies.
func adaptArgForNativeAny(vm *VM, cached any, raw any, staticTypeName string) any {
	if raw == nil {
		return nil
	}
	if calleeIsPipitMakeFunctionClosure(cached) {
		return raw
	}
	return wrapPipitSynthesisedFmtArg(vm, raw, staticTypeName)
}

// readNativeAnyArg reads one argument destined for a native interface ({}) parameter and
// re-clothes it with its source-level named type before adapting.
//
// Takes vm (*VM) which provides the symbol registry for the type restore.
// Takes cached (any) which is the native callee, used by adaptArgForNativeAny.
// Takes site (*CallSite) which carries the per-argument static type strings.
// Takes registers (*Registers) which holds the source value.
// Takes argumentIndex (int) which selects the argument and its static type string.
//
// Returns the prepared argument value ready to pass to the native function.
func readNativeAnyArg(vm *VM, cached any, site *program.CallSite, registers *Registers, argumentIndex int) any {
	raw := readAnyArg(registers, site.Arguments[argumentIndex])
	if argumentIndex < len(site.ArgumentStaticTypeStrings) {
		raw = restoreNamedTypeForFmt(vm, raw, site.ArgumentStaticTypeStrings[argumentIndex])
	}
	return adaptArgForNativeAny(vm, cached, raw, siteStaticTypeName(site, argumentIndex))
}

// calleeIsPipitMakeFunctionClosure reports whether the cached native function value is
// actually a pipit-side reflect.MakeFunc closure. All reflect.MakeFunc closures share one
// trampoline code pointer (captured at init in reflectMakeFuncStubPointer); native Go
// functions never report that pointer, so a single uintptr compare reliably distinguishes
// the two kinds.
//
// Takes cached (any) which is the dispatcher's cached function.
//
// Returns true when cached is a reflect.MakeFunc result.
func calleeIsPipitMakeFunctionClosure(cached any) bool {
	if cached == nil {
		return false
	}
	value := reflect.ValueOf(cached)
	if value.Kind() != reflect.Func {
		return false
	}
	return value.Pointer() == reflectMakeFuncStubPointer
}

// typeNameHasInterfaceAdapter reports whether the named source-level type has a
// registered method that one of the pipit interface adapters knows how to bridge (Error /
// String / MarshalJSON). When true, the caller bypasses the fast path so the slow path
// can build the adapter via tryBuildInterfaceAdapter.
//
// Takes vm (*VM) which owns the method table.
// Takes typeName (string) which is the source-level type name.
//
// Returns true when typeName has at least one bridgeable method.
func typeNameHasInterfaceAdapter(vm *VM, typeName string) bool {
	if vm == nil || typeName == "" {
		return false
	}

	for _, method := range [...]string{".Format", ".Error", ".String", ".MarshalJSON"} {
		if _, _, ok := lookupAdapterMethod(vm, typeName+method); ok {
			return true
		}
	}
	return false
}

// isPipitSynthesisedReflectType reports whether t is pipit-synthesised.
//
// Detects structs the Compiler created via reflect.StructOf with the `_pipitID_` sentinel
// field. Used to decide whether a value needs interface-adapter wrapping at native-call
// boundaries. The field walk is performed once per type and memoised, because the same
// handful of types recur across every native call.
//
// Takes t (reflect.Type) which is the candidate reflect type (pointers are unwrapped
// before checking).
//
// Returns true when t (or its pointee) carries a `_pipitID_`-prefixed field.
func isPipitSynthesisedReflectType(t reflect.Type) bool {
	if t == nil {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	if cached, ok := pipitSynthesisedTypeCache.Load(t); ok {
		if result, isBool := cached.(bool); isBool {
			return result
		}
	}
	result := false
	for field := range t.Fields() {
		if strings.HasPrefix(field.Name, pipitIDFieldPrefix) {
			result = true
			break
		}
	}
	pipitSynthesisedTypeCache.Store(t, result)
	return result
}

// typeContainsPipitSynthesised reports whether fmt walking a value of type t would
// descend into a pipit-synthesised struct and render its `_pipitID_` sentinel field,
// following non-pointer aggregates only because fmt prints nested pointers as addresses.
//
// Takes t (reflect.Type) which is the candidate type.
//
// Returns true when a fmt walk of t reaches a pipit-synthesised struct.
func typeContainsPipitSynthesised(t reflect.Type) bool {
	return typeContainsPipitSynthesisedWalk(t, map[reflect.Type]struct{}{})
}

// typeContainsPipitSynthesisedWalk is the memoised, cycle-guarded worker for
// typeContainsPipitSynthesised. inProgress breaks recursive types so the walk terminates.
//
// Takes t (reflect.Type) which is the type currently being examined.
// Takes inProgress (map[reflect.Type]struct{}) which records the types on the current
// walk path so a self-referential type does not recurse forever.
//
// Returns true when a fmt walk of t reaches a pipit-synthesised struct.
func typeContainsPipitSynthesisedWalk(t reflect.Type, inProgress map[reflect.Type]struct{}) bool {
	if t == nil {
		return false
	}
	if cached, ok := pipitContainsSynthCache.Load(t); ok {
		if result, isBool := cached.(bool); isBool {
			return result
		}
	}
	if _, cycling := inProgress[t]; cycling {
		return false
	}
	inProgress[t] = struct{}{}
	result := typeKindContainsPipitSynthesised(t, inProgress)
	delete(inProgress, t)
	pipitContainsSynthCache.Store(t, result)
	return result
}

// typeKindContainsPipitSynthesised dispatches typeContainsPipitSynthesisedWalk on t's
// kind: struct fields, slice/array elements and map key+element are followed; pointers,
// interfaces and scalars stop the walk (fmt prints a nested pointer's address without
// descending, and an interface's dynamic type is invisible to a static walk).
//
// Takes t (reflect.Type) which is the type whose kind selects the descent.
// Takes inProgress (map[reflect.Type]struct{}) which threads the walk's cycle guard.
//
// Returns true when a fmt walk of t reaches a pipit-synthesised struct.
func typeKindContainsPipitSynthesised(t reflect.Type, inProgress map[reflect.Type]struct{}) bool {
	if typemodel.IsNamedScalarPoolType(t) {
		return true
	}
	switch t.Kind() {
	case reflect.Struct:
		return structTypeContainsPipitSynthesised(t, inProgress)
	case reflect.Slice, reflect.Array:
		return typeContainsPipitSynthesisedWalk(t.Elem(), inProgress)
	case reflect.Map:
		return typeContainsPipitSynthesisedWalk(t.Key(), inProgress) ||
			typeContainsPipitSynthesisedWalk(t.Elem(), inProgress)
	default:
		return false
	}
}

// structTypeContainsPipitSynthesised reports whether a struct type is itself
// pipit-synthesised or has a field whose type reaches a pipit-synthesised struct.
//
// Takes t (reflect.Type) which must be a struct kind.
// Takes inProgress (map[reflect.Type]struct{}) which threads the walk's cycle guard.
//
// Returns true when t or one of its fields reaches a pipit-synthesised struct.
func structTypeContainsPipitSynthesised(t reflect.Type, inProgress map[reflect.Type]struct{}) bool {
	if isPipitSynthesisedReflectType(t) {
		return true
	}
	for field := range t.Fields() {
		if typeContainsPipitSynthesisedWalk(field.Type, inProgress) {
			return true
		}
	}
	return false
}

// dispatchNativeFastPathTagged dispatches a cached native call using the pre-resolved tag
// via an array lookup, avoiding the sequential itab comparisons of a full type switch.
// The dispatch is wrapped in a recover boundary so panics from native functions (sync.*
// misuse, channel ops on closed channels, etc.) become recoverable from interpreted
// defer/recover.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes tag (nativeFastPathTag) which is the pre-classified fast-path tag.
// Takes cached (any) which is the native function value extracted via Interface().
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file to read arguments from and
// write results to.
//
// Returns the recovered panic value, or nil if the dispatch completed without panicking.
func dispatchNativeFastPathTagged(vm *VM, tag nativeFastPathTag, cached any, site *program.CallSite, registers *Registers) (panicValue any) {
	defer func() {
		if r := recover(); r != nil {
			panicValue = r
		}
	}()
	if int(tag) < len(fastPathDispatchTable) {
		if d := fastPathDispatchTable[tag]; d != nil {
			d(vm, cached, site, registers)
		}
	}
	return nil
}

// fpDispatchStringIntError dispatches a native function with the signature func(string)
// (int, error) via direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchStringIntError(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func(string) (int, error))
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	value, err := f(readStringArg(registers, site.Arguments[0]))
	registers.Ints[site.Returns[0].Register] = int64(value)

	if len(site.Returns) > 1 {
		if err != nil {
			registers.General[site.Returns[1].Register] = reflect.ValueOf(err)
		} else {
			registers.General[site.Returns[1].Register] = reflect.Value{}
		}
	}
}

// fpDispatchAny2Any dispatches a native function with the signature func(any, any) any
// via direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchAny2Any(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func(any, any) any)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	result := f(readNativeAnyArg(vm, cached, site, registers, 0), readNativeAnyArg(vm, cached, site, registers, 1))
	if result != nil {
		registers.General[site.Returns[0].Register] = reflect.ValueOf(result)
	} else {
		registers.General[site.Returns[0].Register] = reflect.Value{}
	}
}

// fpDispatchRetError dispatches a native function with the signature func() error via
// direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchRetError(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func() error)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	result := f()
	if result != nil {
		registers.General[site.Returns[0].Register] = reflect.ValueOf(result)
	} else {
		registers.General[site.Returns[0].Register] = reflect.Value{}
	}
}

// fpDispatchStringError dispatches a native function with the signature func(string)
// error via direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchStringError(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func(string) error)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	result := f(readStringArg(registers, site.Arguments[0]))
	if len(site.Returns) > 0 {
		if result != nil {
			registers.General[site.Returns[0].Register] = reflect.ValueOf(result)
		} else {
			registers.General[site.Returns[0].Register] = reflect.Value{}
		}
	}
}

// fpDispatchSprintfString dispatches a native function with the signature func(string,
// ...any) string via direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchSprintfString(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func(string, ...any) string)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	format := readStringArg(registers, site.Arguments[0])
	nVarArgs := len(site.Arguments) - 1
	varArgs := reuseVarArgsBuf(site, nVarArgs)

	verbs := fmtArgumentVerbs(format, nVarArgs)
	for i := range nVarArgs {
		raw := readAnyArg(registers, site.Arguments[i+1])
		argIdx := i + 1
		if argIdx < len(site.ArgumentStaticTypeStrings) {
			raw = restoreNamedTypeForFmt(vm, raw, site.ArgumentStaticTypeStrings[argIdx])
		}
		varArgs[i] = wrapPipitSynthesisedFmtArgForVerb(vm, raw, verbs[i], siteStaticTypeName(site, argIdx))
	}

	if rewrittenFormat, rewrittenArgs, intercepted := interceptFmtFormat(site, 1, format, varArgs); intercepted {
		format = rewrittenFormat
		varArgs = rewrittenArgs
	}

	registers.Strings[site.Returns[0].Register] = f(format, varArgs...)
}

// fpDispatchSprintfError dispatches a native function with the signature func(string,
// ...any) error via direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchSprintfError(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func(string, ...any) error)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	format := readStringArg(registers, site.Arguments[0])
	nVarArgs := len(site.Arguments) - 1
	varArgs := reuseVarArgsBuf(site, nVarArgs)

	verbs := fmtArgumentVerbs(format, nVarArgs)
	for i := range nVarArgs {
		raw := readAnyArg(registers, site.Arguments[i+1])
		argIdx := i + 1
		if argIdx < len(site.ArgumentStaticTypeStrings) {
			raw = restoreNamedTypeForFmt(vm, raw, site.ArgumentStaticTypeStrings[argIdx])
		}
		varArgs[i] = wrapPipitSynthesisedFmtArgForVerb(vm, raw, verbs[i], siteStaticTypeName(site, argIdx))
	}

	if rewrittenFormat, rewrittenArgs, intercepted := interceptFmtFormat(site, 1, format, varArgs); intercepted {
		format = rewrittenFormat
		varArgs = rewrittenArgs
	}

	result := f(format, varArgs...)
	if result != nil {
		registers.General[site.Returns[0].Register] = reflect.ValueOf(result)
	} else {
		registers.General[site.Returns[0].Register] = reflect.Value{}
	}
}

// fpDispatchSprintVarargs dispatches a native function with the signature func(...any)
// string via direct type assertion on the cached value.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchSprintVarargs(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	f, ok := cached.(func(...any) string)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}

	varArgs := reuseVarArgsBuf(site, len(site.Arguments))
	for i := range site.Arguments {
		raw := readAnyArg(registers, site.Arguments[i])
		if i < len(site.ArgumentStaticTypeStrings) {
			raw = restoreNamedTypeForFmt(vm, raw, site.ArgumentStaticTypeStrings[i])
		}
		varArgs[i] = wrapPipitSynthesisedFmtArg(vm, raw, siteStaticTypeName(site, i))
	}

	registers.Strings[site.Returns[0].Register] = f(varArgs...)
}

// fpDispatchBytesArrN dispatches a digest-returning native call. Generic body shared by
// fpDispatchBytesArr16/20/32/64, monomorphised per digest-size type A.
//
// Takes vm (*VM) which provides the arena allocator.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
// Takes abiType (unsafe.Pointer) which is the result type's *abi.Type token (one of
// bytesArr16/20/32/64ABIType).
// Takes allocSize (uintptr) which is sizeof(A) for the arena slab reservation.
func fpDispatchBytesArrN[A any](
	vm *VM, cached any, site *program.CallSite, registers *Registers,
	abiType unsafe.Pointer, allocSize uintptr,
) {
	f, ok := cached.(func([]byte) A)
	if !ok {
		_ = RecordFastPathTypeMismatch(vm, -1, fastPathDispatcherTag, reflectTypeName(cached))
		return
	}
	result := f(readBytesArg(registers, site.Arguments[0]))
	if vm.Arena == nil {
		registers.General[site.Returns[0].Register] = reflect.ValueOf(result)
		return
	}
	slot := vm.Arena.AllocBytes(allocSize, 1)
	*(*A)(slot) = result
	registers.General[site.Returns[0].Register] = unsafeNewAt(abiType, slot, reflect.Array)
}

// fpDispatchBytesArr32 dispatches func([]byte) [32]byte.
//
// SHA-256 over thousands of lines is the canonical workload.
//
// Takes vm (*VM) which provides the arena allocator.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchBytesArr32(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	fpDispatchBytesArrN[[digestBytesSHA256]byte](vm, cached, site, registers, bytesArr32ABIType, digestBytesSHA256)
}

// fpDispatchBytesArr16 dispatches func([]byte) [16]byte.
//
// MD5 / UUID digest sizes.
//
// Takes vm (*VM) which provides the arena allocator.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchBytesArr16(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	fpDispatchBytesArrN[[digestBytesMD5]byte](vm, cached, site, registers, bytesArr16ABIType, digestBytesMD5)
}

// fpDispatchBytesArr20 dispatches func([]byte) [20]byte.
//
// SHA-1 digest size.
//
// Takes vm (*VM) which provides the arena allocator.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchBytesArr20(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	fpDispatchBytesArrN[[digestBytesSHA1]byte](vm, cached, site, registers, bytesArr20ABIType, digestBytesSHA1)
}

// fpDispatchBytesArr64 dispatches func([]byte) [64]byte.
//
// SHA-512 digest size.
//
// Takes vm (*VM) which provides the arena allocator.
// Takes cached (any) which is the native function value to assert.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpDispatchBytesArr64(vm *VM, cached any, site *program.CallSite, registers *Registers) {
	fpDispatchBytesArrN[[digestBytesSHA512]byte](vm, cached, site, registers, bytesArr64ABIType, digestBytesSHA512)
}

// readBytesArg reads a []byte argument from the appropriate register bank. Mirrors
// readStringArg / readIntArg shape.
//
// Takes registers (*Registers) which is the VM register file to read from.
// Takes argument (VarLocation) which describes the register bank and index to read.
//
// Returns the []byte from the slicesByte bank when kind is isa.RegisterSliceByte;
// otherwise reads via .Bytes() on the general bank reflect.Value.
func readBytesArg(registers *Registers, argument program.VarLocation) []byte {
	if argument.Kind == isa.RegisterSliceByte {
		return registers.slicesByte[argument.Register]
	}
	v := registers.General[argument.Register]
	if v.IsValid() && v.Kind() == reflect.Slice {
		return v.Bytes()
	}
	return nil
}

// readStringArg reads a string argument from the appropriate register bank based on the
// argument's kind.
//
// Takes registers (*Registers) which is the VM register file to read from.
// Takes argument (VarLocation) which describes the register bank and index to read.
//
// Returns the string value from the string register bank if the argument kind is
// isa.RegisterString, or converts from the general register bank otherwise.
func readStringArg(registers *Registers, argument program.VarLocation) string {
	if argument.Kind == isa.RegisterString {
		return registers.Strings[argument.Register]
	}

	return registers.General[argument.Register].String()
}

// readIntArg reads an int argument from the appropriate register bank.
//
// Takes registers (*Registers) which is the VM register file to read from.
// Takes argument (VarLocation) which describes the register bank and index to read.
//
// Returns the int64 value from the int register bank if the argument kind is
// isa.RegisterInt, or converts from the general register bank otherwise.
func readIntArg(registers *Registers, argument program.VarLocation) int64 {
	if argument.Kind == isa.RegisterInt {
		return registers.Ints[argument.Register]
	}

	return registers.General[argument.Register].Int()
}

// readFloatArg reads a float argument from the appropriate register bank.
//
// Takes registers (*Registers) which is the VM register file to read from.
// Takes argument (VarLocation) which describes the register bank and index to read.
//
// Returns the float64 value from the float register bank if the argument kind is
// isa.RegisterFloat, or converts from the general register bank otherwise.
func readFloatArg(registers *Registers, argument program.VarLocation) float64 {
	if argument.Kind == isa.RegisterFloat {
		return registers.Floats[argument.Register]
	}

	return registers.General[argument.Register].Float()
}

// readBoolArg reads a bool argument from the appropriate register bank.
//
// Takes registers (*Registers) which is the VM register file to read from.
// Takes argument (VarLocation) which describes the register bank and index to read.
//
// Returns the bool value from the bool register bank if the argument kind is
// isa.RegisterBool, or converts from the general register bank otherwise.
func readBoolArg(registers *Registers, argument program.VarLocation) bool {
	if argument.Kind == isa.RegisterBool {
		return registers.Bools[argument.Register]
	}

	return registers.General[argument.Register].Bool()
}

// readAnyArg reads an argument from any register bank and returns it as an interface{}
// value. Used for variadic fast paths where arguments must be boxed into []any.
//
// Takes registers (*Registers) which is the VM register file to read from.
// Takes argument (VarLocation) which describes the register bank and index to read.
//
// Returns the value from the appropriate typed register bank based on the argument kind,
// or nil if the kind is unrecognised or the value is invalid.
func readAnyArg(registers *Registers, argument program.VarLocation) any {
	switch argument.Kind {
	case isa.RegisterInt:
		return int(registers.Ints[argument.Register])
	case isa.RegisterFloat:
		return registers.Floats[argument.Register]
	case isa.RegisterString:
		return registers.Strings[argument.Register]
	case isa.RegisterBool:
		return registers.Bools[argument.Register]
	case isa.RegisterUint:
		return registers.Uints[argument.Register]
	case isa.RegisterComplex:
		return registers.Complex[argument.Register]
	case isa.RegisterSliceInt:
		return registers.SlicesInt[argument.Register]
	case isa.RegisterSliceFloat:
		return registers.slicesFloat[argument.Register]
	case isa.RegisterSliceString:
		return registers.slicesString[argument.Register]
	case isa.RegisterSliceBool:
		return registers.slicesBool[argument.Register]
	case isa.RegisterSliceUint:
		return registers.slicesUint[argument.Register]
	case isa.RegisterSliceByte:
		return registers.slicesByte[argument.Register]
	case isa.RegisterGeneral:
		v := registers.General[argument.Register]
		if v.IsValid() {
			return v.Interface()
		}

		return nil
	default:
		return nil
	}
}

func init() {
	fastPathDispatchTable[fastPathTagStringString] = fpDispatchStringString
	fastPathDispatchTable[fastPathTagStringInt] = fpDispatchStringInt
	fastPathDispatchTable[fastPathTagStringBool] = fpDispatchStringBool
	fastPathDispatchTable[fastPathTagStringRuneBool] = fpDispatchStringRuneBool
	fastPathDispatchTable[fastPathTagStringRuneInt] = fpDispatchStringRuneInt
	fastPathDispatchTable[fastPathTagString2Bool] = fpDispatchString2Bool
	fastPathDispatchTable[fastPathTagString2String] = fpDispatchString2String
	fastPathDispatchTable[fastPathTagString2Int] = fpDispatchString2Int
	fastPathDispatchTable[fastPathTagString3String] = fpDispatchString3String
	fastPathDispatchTable[fastPathTagIntString] = fpDispatchIntString
	fastPathDispatchTable[fastPathTagIntInt] = fpDispatchIntInt
	fastPathDispatchTable[fastPathTagIntBool] = fpDispatchIntBool
	fastPathDispatchTable[fastPathTagInt2Int] = fpDispatchInt2Int
	fastPathDispatchTable[fastPathTagInt2Bool] = fpDispatchInt2Bool
	fastPathDispatchTable[fastPathTagInt2String] = fpDispatchInt2String
	fastPathDispatchTable[fastPathTagInt64IntString] = fpDispatchInt64IntString
	fastPathDispatchTable[fastPathTagStringIntError] = fpDispatchStringIntError
	fastPathDispatchTable[fastPathTagFloat64Float64] = fpDispatchFloat64Float64
	fastPathDispatchTable[fastPathTagFloat642Float64] = fpDispatchFloat642Float64
	fastPathDispatchTable[fastPathTagAnyBool] = fpDispatchAnyBool
	fastPathDispatchTable[fastPathTagAnyString] = fpDispatchAnyString
	fastPathDispatchTable[fastPathTagAnyInt] = fpDispatchAnyInt
	fastPathDispatchTable[fastPathTagAnyInt64] = fpDispatchAnyInt64
	fastPathDispatchTable[fastPathTagAnyFloat64] = fpDispatchAnyFloat64
	fastPathDispatchTable[fastPathTagAny2Any] = fpDispatchAny2Any
	fastPathDispatchTable[fastPathTagRetString] = fpDispatchRetString
	fastPathDispatchTable[fastPathTagRetBool] = fpDispatchRetBool
	fastPathDispatchTable[fastPathTagRetInt] = fpDispatchRetInt
	fastPathDispatchTable[fastPathTagRetInt64] = fpDispatchRetInt64
	fastPathDispatchTable[fastPathTagRetFloat64] = fpDispatchRetFloat64
	fastPathDispatchTable[fastPathTagRetError] = fpDispatchRetError
	fastPathDispatchTable[fastPathTagVoid] = fpDispatchVoid
	fastPathDispatchTable[fastPathTagVoidString] = fpDispatchVoidString
	fastPathDispatchTable[fastPathTagVoidInt] = fpDispatchVoidInt
	fastPathDispatchTable[fastPathTagVoidInt64] = fpDispatchVoidInt64
	fastPathDispatchTable[fastPathTagVoidBool] = fpDispatchVoidBool
	fastPathDispatchTable[fastPathTagVoidString2] = fpDispatchVoidString2
	fastPathDispatchTable[fastPathTagStringError] = fpDispatchStringError
	fastPathDispatchTable[fastPathTagSprintfString] = fpDispatchSprintfString
	fastPathDispatchTable[fastPathTagSprintfError] = fpDispatchSprintfError
	fastPathDispatchTable[fastPathTagSprintVarargs] = fpDispatchSprintVarargs
	fastPathDispatchTable[fastPathTagBytesArr32] = fpDispatchBytesArr32
	fastPathDispatchTable[fastPathTagBytesArr16] = fpDispatchBytesArr16
	fastPathDispatchTable[fastPathTagBytesArr20] = fpDispatchBytesArr20
	fastPathDispatchTable[fastPathTagBytesArr64] = fpDispatchBytesArr64
}
