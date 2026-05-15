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

package program

import (
	"go/ast"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

const (
	// TypeArgsTagSeparator separates type arguments inside the sentinel tag value.
	TypeArgsTagSeparator = ";"

	// SentinelTypeArgsTagKey is the struct tag key on a synthesised struct's sentinel field
	// that records type arguments. It participates in reflect.StructOf identity so
	// instantiations with identical field layouts stay distinct types.
	SentinelTypeArgsTagKey = "pipitTypeArgs"

	// AsmMethodRejectedCacheSize is the number of rejected receiver type words an ASM inline
	// method-call site remembers before it stops probing assembly dispatch.
	AsmMethodRejectedCacheSize = 4

	// MaxSpecialisationTypeArgs caps the number of type-args supported per generic
	// specialisation. Bigger arities fall back to the type-erased path.
	MaxSpecialisationTypeArgs = 8

	// AbiTypeKindByteOffset is the byte offset of abi.Type.Kind_ within the runtime abi.Type
	// layout.
	AbiTypeKindByteOffset = 23

	// AbiTypePointerElemOffset is the byte offset of the element type pointer within
	// abi.PtrType and abi.SliceType.
	AbiTypePointerElemOffset = 48

	// EfaceDataPointerOffset is the byte offset of the data word within a runtime eface or
	// iface pair.
	EfaceDataPointerOffset = 8

	// InlineDescriptorVictimMask masks the per-site round-robin victim counter down to a
	// slot index. Must equal inlineDescriptorSlotCount - 1.
	InlineDescriptorVictimMask = inlineDescriptorSlotCount - 1

	// InnerCalleeSlotCount sets the inner-Eval callee cache slot count per InlineDescriptor.
	// Eight slots cover the typical working set of concrete node types.
	InnerCalleeSlotCount = 8

	// InnerCalleeVictimMask masks InnerCalleeVictim down to a slot index. Must equal
	// InnerCalleeSlotCount - 1.
	InnerCalleeVictimMask = InnerCalleeSlotCount - 1

	// MethodICVictimMask masks MethodICVictim down to a slot index. Must match
	// methodICSlotCount - 1.
	MethodICVictimMask = methodICSlotCount - 1

	// RangeCheckWindowSize is the instruction count of the fuseRangeCheckUintJumpFalse
	// window.
	RangeCheckWindowSize = 8

	// RangeCheckFirstJumpOffset is the position of the first JumpIfFalse relative to the
	// window start.
	RangeCheckFirstJumpOffset = 3

	// RangeCheckSecondLoadOffset is the position of the high-bound LoadUintConstSmall
	// relative to the window start.
	RangeCheckSecondLoadOffset = 4

	// RangeCheckSecondMoveOffset is the position of the second MoveInt sub-op relative to
	// the window start.
	RangeCheckSecondMoveOffset = 6

	// RangeCheckSecondJumpOffset is the position of the second JumpIfFalse relative to the
	// window start.
	RangeCheckSecondJumpOffset = 7

	// RangeCheckFirstNopOffset is the starting slot from which nop padding replaces consumed
	// instructions inside the window.
	RangeCheckFirstNopOffset = 3

	// RangeCheckFirstJumpDelta is the expected signed offset on the first JumpIfFalse
	// chaining into the second jump.
	RangeCheckFirstJumpDelta = 3

	// ThreeInstructionWindow is the minimum body remaining required to match any of the
	// load+compare+jump triples (e.g. fuseEqUintConstJumpFalse, fuseMapIndexOkJumpFalse).
	ThreeInstructionWindow = 3

	// interfaceMethodRequirementSeparator separates the name and arity fields of an encoded
	// interface method requirement.
	interfaceMethodRequirementSeparator = "/"

	// requirementFieldCount is the number of fields in a fully encoded interface method
	// requirement: name, parameter count, result count and shape.
	requirementFieldCount = 4

	// requirementArityFieldCount is the number of fields in a requirement that records the
	// arities but no shape.
	requirementArityFieldCount = 3

	// asciiLimit is the first byte value outside ASCII; identifiers may continue with any
	// non-ASCII byte (the start of a multi-byte rune).
	asciiLimit = 0x80

	// inlineDescriptorSlotCount sets the IC slot count per call site. Smaller than
	// methodICSlotCount because inline-eligible sites tend to be less polymorphic.
	inlineDescriptorSlotCount = 8

	// methodICSlotCount sets the inline-cache slot count per isa.SubOpCallMethod call site.
	// Sixteen slots cover typical polymorphic receiver-type sets.
	methodICSlotCount = 16
)

const (
	// InlineShapeNone marks a descriptor that did not match any known fused shape. The
	// runtime falls back to pushCompiledFrame or runTinyLeafInline for the cached callee.
	InlineShapeNone InlineShape = iota

	// InlineShapeBinopUint marks a fused uint64 binop-Eval shape of the form `return
	// (recv.left.Eval(env) OP recv.right.Eval(env)) & mask`.
	InlineShapeBinopUint
)

// CompiledFunction holds the bytecode and metadata for a single function.
//
//exhaustruct:ignore
type CompiledFunction struct {
	// VariableInitFunction holds bytecode for package-level variable initialisers. When
	// non-nil, Execute runs it before the main body to reset globals to their declared
	// values on each invocation.
	VariableInitFunction *CompiledFunction

	// GlobalBases shifts global-access operands by a per-kind offset at dispatch time so a
	// loaded bundle's globals land in the correct target slots, shared across all functions
	// in a bundle. nil for source-compiled functions.
	GlobalBases *SlotAllocation

	// GenericTypeParams is the *types.TypeParamList from the generic signature, retained so
	// call-site type-argument resolution can build a substitution map by zipping these
	// against c.Info.Instances[ident].TypeArgs. Nil for non-generic functions.
	GenericTypeParams *types.TypeParamList

	// GenericRecvTypeParams is signature.RecvTypeParams() from the declaration, not from an
	// instantiated signature, because go/types freshens type parameters on substitution and
	// the cloned objects would silently miss the method body's identifiers. Nil unless the
	// receiver's base type is generic.
	GenericRecvTypeParams *types.TypeParamList

	// ASMCallInfoTables caches the pre-computed AsmCallInfo tables keyed by compiled
	// function, built once on first execution under ASMCallInfoTablesOnce. Held as any
	// because the concrete type belongs to the VM.
	ASMCallInfoTables any

	// ComplexConstIndex accelerates constant deduplication during compilation. Nil at
	// runtime after Optimise() releases it.
	ComplexConstIndex map[complex128]uint16

	// FloatConstIndex accelerates constant deduplication during compilation. Nil at runtime
	// after Optimise() releases it.
	FloatConstIndex map[float64]uint16

	// StringConstIndex accelerates constant deduplication during compilation. Nil at runtime
	// after Optimise() releases it.
	StringConstIndex map[string]uint16

	// UintConstIndex accelerates constant deduplication during compilation. Nil at runtime
	// after Optimise() releases it.
	UintConstIndex map[uint64]uint16

	// TypeRefIndex accelerates type table deduplication during compilation. Nil at runtime
	// after Optimise() releases it.
	TypeRefIndex map[reflect.Type]uint16

	// TypeNames maps reflect.Type to the source-level type name for types created via
	// reflect.StructOf (which have an empty Name()). Used by handleCallMethod to resolve
	// method table keys at runtime.
	TypeNames map[reflect.Type]string

	// DebugSourceMap maps program counter offsets to source file positions. Nil when debug
	// info is disabled.
	DebugSourceMap *SourceMap

	// PeepholeProvenance records, per rewritten PC, the kind of rewrite the optimiser
	// applied, used by the disassembler to annotate CSE moves and LICM hoists. nil until the
	// first rewrite is recorded.
	PeepholeProvenance map[int]PeepholeAnnotation

	// FieldStoreArenaSafePCs lists the bytecode PCs of struct-field stores whose receiver is
	// arena-allocated, sole-referenced and frame-confined. nil means no store qualified.
	FieldStoreArenaSafePCs map[int]bool

	// ArenaSafeAllocPCs lists opAllocIndirect PCs whose target local provably does not
	// escape the call frame. MUST be populated only by the escape analysis, because
	// speculative entries cause use-after-free on arena Reset.
	ArenaSafeAllocPCs map[int]bool

	// AliasInfo holds the per-PC pointer-alias environments computed by
	// RunPointerAliasAnalysis, consulted by the CSE and in-place-append passes via MayAlias.
	// nil before analysis, when empty, or after LICM invalidation.
	AliasInfo *PointerAliasInfo

	// IntConstIndex accelerates constant deduplication during compilation. Nil at runtime
	// after Optimise() releases it.
	IntConstIndex map[int64]uint16

	// DebugVarTable holds variable debug information (names, liveness ranges). Nil when
	// debug info is disabled.
	DebugVarTable *DebugVarTable

	// GenericDeclaration is the AST FuncDecl retained for body re-compilation when a call
	// site triggers specialisation. Nil for non-generic functions and for specialisations
	// (which do not re-specialise further).
	GenericDeclaration *ast.FuncDecl

	// methodTable maps "TypeName.MethodName" to a function index in functions. Used for
	// runtime dispatch of interface method calls where the concrete type is unknown at
	// compile time.
	methodTable map[string]uint16

	// GetMethodReceiverTypeNames carries the source-level type name the Compiler resolved at
	// each isa.SubOpGetMethod site, keyed by PC, needed for named non-struct receivers whose
	// type cannot be recovered from reflect at runtime. nil in the common case.
	GetMethodReceiverTypeNames map[uint32]string

	// SpecialisationOrigin points back at the generic CompiledFunction that produced this
	// specialised body. nil for non-specialised functions.
	SpecialisationOrigin *CompiledFunction

	// Specialisations maps concrete type-arg tuples to specialised body indices, living on
	// the generic callee and populated lazily. nil for non-generic functions.
	Specialisations map[SpecialisationKey]uint16

	// DebugEmitHook is called after each instruction is emitted during compilation, used by
	// the Compiler to record source positions.
	DebugEmitHook func(pc int)

	// LayoutTypeWords caches per-TypeTable-entry runtime type words of the struct type and
	// its pointer type.
	LayoutTypeWords atomic.Pointer[LayoutTypeWordSlice]

	// MarshalShadowMemo memoises whether a reflect.Type can hold a leaf whose type declares
	// MarshalJSON in this program's method tables.
	MarshalShadowMemo sync.Map

	// InPlaceHeaderReusePCs lists append PCs whose source register provably holds the only
	// reference to its slice header, allowing in-place length bumps. nil means no site
	// classified.
	InPlaceHeaderReusePCs map[int]bool

	// SourceFile is the source file path for error reporting.
	SourceFile string

	// Name is the function's qualified name (e.g., "main.BuildAST").
	Name string

	// Body is the bytecode instruction sequence.
	Body []isa.Instruction

	// UpvalueDescriptors describes captured variables for closures. Each entry tells the VM
	// how to initialise an upvalue when creating a closure from the function.
	UpvalueDescriptors []UpvalueDescriptor

	// ResultKinds maps each return value position to its register kind.
	ResultKinds []isa.RegisterKind

	// NamedResultLocations holds the register locations of named return values, if any. Used
	// by bare return statements to copy named result variables to return positions.
	NamedResultLocations []VarLocation

	// NamedResultNames holds the source-level identifier of each named return, parallel to
	// NamedResultLocations. Used at Emit-return time to re-lookup the current variable state
	// so heap promotions and closure captures are reflected.
	NamedResultNames []string

	// StringConstants holds string constants referenced by opLoadStringConst.
	StringConstants []string

	// Functions holds nested function literals (closures) defined within the enclosing
	// function. Referenced by opMakeClosure via index.
	Functions []*CompiledFunction

	// GeneralConstants holds reflect.Value constants referenced by opLoadGeneralConst. These
	// include type values, function values, and complex constants.
	GeneralConstants []reflect.Value

	// TypeTable holds reflect.Type values referenced by type operation instructions
	// (opTypeAssert, opConvert, opMakeSlice, etc.).
	TypeTable []reflect.Type

	// GeneralConstantDescriptors records how each GeneralConstants entry was created so it
	// can be reconstructed from a serialised representation.
	GeneralConstantDescriptors []descriptor.GeneralConstantDescriptor

	// TypeTableDescriptors records the serialisable description of each TypeTable entry as
	// it was read from bytecode. A function compiled in this process leaves it empty; the
	// codec derives the descriptors from TypeTable through TypeTableDescriptorsOf() when it
	// serialises the function.
	TypeTableDescriptors []descriptor.TypeDescriptor

	// TypeTableInterfaceMethods records required method-name sets per TypeTable entry,
	// letting handleTypeAssert distinguish interface types that pipit collapsed to `any`
	// (reflect has no InterfaceOf). nil or empty means the genuine empty interface.
	TypeTableInterfaceMethods [][]string

	// BoolConstants holds bool constants referenced by opLoadBoolConst.
	BoolConstants []bool

	// CallSites describes each function call in the bytecode. opCall references call sites
	// by index.
	CallSites []CallSite

	// ParameterEscapes records, per parameter, whether it escapes the owning function's
	// frame. nil means "everything escapes" (conservative default before analysis); the
	// caller-side escape analysis consults this to decide whether passing &local escapes.
	ParameterEscapes []bool

	// ComplexConstants holds complex128 constants referenced by opLoadComplexConst.
	ComplexConstants []complex128

	// UintConstants holds uint64 constants referenced by opLoadUintConst.
	UintConstants []uint64

	// IntConstants holds int64 constants referenced by opLoadIntConst.
	IntConstants []int64

	// FloatConstants holds float64 constants referenced by opLoadFloatConst.
	FloatConstants []float64

	// ParameterTypeRefs records each parameter's declared types.Type, used by
	// compileSpecialisedBody to recompute ParameterKinds under substitution. nil for
	// non-generic functions.
	ParameterTypeRefs []types.Type

	// ParameterKinds maps each parameter position to its register kind.
	ParameterKinds []isa.RegisterKind

	// ParameterRegisters maps each parameter position to the register index the caller must
	// write the argument into. Needed because address-taken parameters shift later
	// general-bank indices past the naive per-bank counter.
	ParameterRegisters []uint8

	// ParameterIsGeneric reports whether each parameter is the instantiation of a TypeParam.
	// nil for non-generic callees.
	ParameterIsGeneric []bool

	// ParameterTypedSlicePromoted records whether each parameter kept its typed-slice bank
	// after the survivor walk. nil when no parameter was a typed-slice candidate.
	ParameterTypedSlicePromoted []bool

	// ResultTypeRefs is the result-side analogue of ParameterTypeRefs.
	ResultTypeRefs []types.Type

	// StructLiteralTable holds per-TypeTable-index words the struct-literal allocation
	// handler needs for arena carving.
	StructLiteralTable []StructLiteralEntry

	// StructLayoutTable holds resolved struct field layouts for fast-path field accessors,
	// enabling direct unsafe-pointer typed loads and stores without entering reflect.
	StructLayoutTable []StructFieldLayout

	// PrecomputedAllocCounts is the typedSlabCounts derived from NumRegisters, cached on the
	// function so AllocRegistersInto does not have to convert per call. Populated lazily on
	// first use via ensurePrecomputedAllocCounts.
	PrecomputedAllocCounts TypedSlabCounts

	// maxConstantPoolSize caps the entry count of each constant pool (int, float, string,
	// bool, uint, complex, general) and the type table for the function. Zero means use the
	// package default (defaultMaxConstantPoolSize).
	maxConstantPoolSize int

	// maxSpecialisations caps the number of generic instantiations the function may
	// accumulate. Zero means use the package default (defaultMaxSpecialisations).
	maxSpecialisations int

	// ASMCallInfoBase holds the function's AsmCallInfo table base pointer. Populated once
	// under ASMCallInfoTablesOnce and read on every frame change.
	ASMCallInfoBase uintptr

	// StructLiteralTableBase is the published address of StructLiteralTable's first entry,
	// read by the struct-literal allocation handler through CF_FUNCTION. Stored before
	// StructLiteralTableLength so a reader that observes the length also observes the base;
	// a zero base or length sends the handler to the Go path.
	StructLiteralTableBase uintptr

	// StructLiteralTableLength is the published entry count of StructLiteralTable.
	StructLiteralTableLength int64

	// maxMethods caps the number of entries that may be registered in the methodTable for
	// the function. Zero means use the package default (defaultMaxMethods).
	maxMethods int

	// NumRegisters tracks peak register usage per bank [int, float, string, general, bool,
	// uint, complex]. Used to allocate the register file for each call frame.
	NumRegisters [isa.NumRegisterKinds]uint32

	// EvalChildALayout, EvalChildBLayout and EvalChildCLayout are the struct-field layouts
	// of the interface children the shaped body fetches, in evaluation order.
	EvalChildALayout StructFieldLayout

	// TinyLeafLayout is the StructFieldLayout for the leaf's primary field read. Valid only
	// when TinyLeafShape != TinyLeafNone.
	TinyLeafLayout StructFieldLayout

	// EvalChildBLayout is the struct-field layout of the second interface child fetched by
	// the shaped body.
	EvalChildBLayout StructFieldLayout

	// EvalChildCLayout is the struct-field layout of the third interface child fetched by
	// the shaped body.
	EvalChildCLayout StructFieldLayout

	// EvalShapeOnce guards one-time classification of EvalShape and the evalChild* layouts.
	// Readers must call classifyFusedEvalShape rather than testing EvalShape directly,
	// because only Do establishes the happens-before edge to the layouts.
	EvalShapeOnce sync.Once

	// ASMCallInfoTablesOnce guards one-time computation of ASMCallInfoTables.
	ASMCallInfoTablesOnce sync.Once

	// ASMCallInfoBases holds each function's AsmCallInfo table base indexed like Functions,
	// plus one trailing zero slot for callees from another root. Built once under
	// ASMCallInfoTablesOnce and shared read-only.
	ASMCallInfoBases []uintptr

	// LayoutTypeWordsOnce guards one-time computation of LayoutTypeWords.
	LayoutTypeWordsOnce sync.Once

	// StructLiteralTableOnce guards one-time construction of StructLiteralTable.
	StructLiteralTableOnce sync.Once

	// cachedMaxCallDepth memoises EstimateMaxCallDepth(root), encoded as depth+1 so that 0
	// means not computed and only meaningful on the root CompiledFunction. atomic.Int32
	// because concurrent goroutine launches race-safely populate the same deterministic
	// value.
	cachedMaxCallDepth atomic.Int32

	// observedCallDepth is the deepest frame stack a run of this root has reached, used to
	// pre-size the arena on later runs.
	observedCallDepth atomic.Int32

	// NonZeroBankMask is a bitmask of which register banks have non-zero counts. Bit i is
	// set when NumRegisters[i] > 0.
	NonZeroBankMask uint16

	// NonEmptyConstantMask is a bitmask of non-empty per-function constant pools. Bit
	// positions map to the constMask* constants.
	NonEmptyConstantMask uint16

	// EvalSiteC indexes the call site for the third child dispatch.
	EvalSiteC uint16

	// EvalSiteB indexes the call site for the second child dispatch.
	EvalSiteB uint16

	// EvalSiteA indexes the call site for the first child dispatch.
	EvalSiteA uint16

	// RequiresSpecialisation is true when the erased body cannot execute correctly because
	// it calls a generic method whose type arguments are this declaration's own type
	// parameters. Propagates outward until a caller supplies concrete type arguments.
	RequiresSpecialisation bool

	// HasReceiver is true when the compiled function is a method, with the receiver
	// prepended as parameter 0 of ParameterKinds.
	HasReceiver bool

	// CachedInlineRefusal records the inliner's verdict on this callee. Zero value
	// (InlineRefusalUnknown) forces a fresh scan on first probe.
	CachedInlineRefusal InlineRefusal

	// TinyLeafShape classifies the function as a recognised tiny-leaf shape for inline
	// execution in the caller's frame. TinyLeafNone by default.
	TinyLeafShape TinyLeafShape

	// HeapMutationClass records whether the function and its transitive callees may mutate
	// heap-resident state, consulted by the CSE and LICM passes at statically-resolved call
	// sites. Zero before RunHeapPurityAnalysis.
	HeapMutationClass heapMutationClassification

	// HasRecover is true when the body (including nested literals) calls recover. A deferred
	// callee without recover can run as an ordinary frame instead of a nested dispatch.
	HasRecover bool

	// EvalShape classifies the body as one of the fused expression-evaluator families
	// executed by runFusedEvalShape without frame pushes. fusedEvalNone by default.
	EvalShape FusedEvalShape

	// JumpRangeExceeded is set by EncodeJumpOffset when a relative jump distance overflows
	// the signed 16-bit encoding (the body grew past the addressable range). Optimise
	// rejects such a function with ErrCompileJumpRange so the Compiler reports a clean error
	// instead of panicking.
	JumpRangeExceeded bool

	// EmittedInlineBlocker records the first inline-blocking opcode observed by Emit().
	// InlineRefusalUnknown when no blocker was emitted, enabling O(1) eligibility checks.
	EmittedInlineBlocker InlineRefusal

	// IsVariadic is true when the last parameter is variadic (...T).
	IsVariadic bool

	// IsPointerReceiver is true when the method's declared receiver type is *T rather than
	// T. Used to decide whether a call site needs a compile-time address-take.
	IsPointerReceiver bool

	// IsGenericFunction is true when the function declared TypeParams.
	IsGenericFunction bool

	// InRecursionCycle is true when the function participates in a non-trivial SCC.
	// Recursive callees cannot be inlined (would expand infinitely).
	InRecursionCycle bool

	// PrecomputedAllocCountsValid tracks whether PrecomputedAllocCounts has been
	// initialised. Lazily set on first AllocRegistersInto call to avoid an extra
	// compile-time pass through every function in the program.
	PrecomputedAllocCountsValid bool

	// ResultReflectTypes records the exact reflect.Type each return value carries, or nil
	// when the bank's canonical type is exact. In-memory only.
	ResultReflectTypes []reflect.Type

	// MethodSignatures maps a method table key ("T.M") to its SignatureShapeString.
	// In-memory only.
	MethodSignatures map[string]string

	// typeRefMethodsIndex dedups method-bearing type-table entries by reflect type and
	// requirement list (see AddTypeRefWithMethods). Compile-time only.
	typeRefMethodsIndex map[typeRefMethodsKey]uint16

	// SignatureReflectType is the function's static Go type, without the receiver for
	// methods. In-memory only.
	SignatureReflectType reflect.Type

	// VariadicSliceType is the slice type of a variadic function's final parameter.
	// In-memory only.
	VariadicSliceType reflect.Type

	// InspectsCallStack is true when the body uses runtime.Caller, runtime.Callers,
	// runtime/debug.Stack or runtime/debug.PrintStack, which read the interpreter's frames:
	// the compiler then keeps a call to the function as a real frame instead of a tail call,
	// and never inlines a caller of it. Compile-time only.
	InspectsCallStack bool

	// MethodReceiverReflectType is the receiver's exact reflect type for a method. nil for
	// functions and loaded bundles.
	MethodReceiverReflectType reflect.Type

	// RuntimeName is the name runtime.FuncForPC and stack dumps report. Empty for loaded
	// bundles.
	RuntimeName string
}

// NewNamedFunction returns an empty compiled function carrying only a name.
//
// Takes name (string) which labels the function in diagnostics and disassembly.
//
// Returns *CompiledFunction with every other field at its zero value.
func NewNamedFunction(name string) *CompiledFunction {
	return &CompiledFunction{Name: name}
}

// ShareFunctionsWith points the receiver's table at another function's table, used by the
// REPL so each submission resolves against everything compiled so far.
//
// Takes source (*CompiledFunction) whose table is adopted.
func (compiledFunction *CompiledFunction) ShareFunctionsWith(source *CompiledFunction) {
	compiledFunction.Functions = source.Functions
}

// TruncateFunctions shortens the function table to length n, used by the REPL to roll
// back functions from a failed submission.
//
// Takes n (int) which is the length to keep; values outside the current range are
// ignored.
func (compiledFunction *CompiledFunction) TruncateFunctions(n int) {
	if n < 0 || n > len(compiledFunction.Functions) {
		return
	}
	compiledFunction.Functions = compiledFunction.Functions[:n]
}

// SetVariableInitFunction records the package-level variable initialiser for this
// function.
//
// Takes function (*CompiledFunction) which initialises package-level variables.
func (compiledFunction *CompiledFunction) SetVariableInitFunction(function *CompiledFunction) {
	compiledFunction.VariableInitFunction = function
}

// ApplyResourceLimits stamps per-function caps onto the function. A zero value leaves the
// existing cap untouched.
//
// Takes maxConstantPoolSize (int) which caps the constant pool.
// Takes maxSpecialisations (int) which caps generic specialisations.
// Takes maxMethods (int) which caps the method table.
func (compiledFunction *CompiledFunction) ApplyResourceLimits(maxConstantPoolSize, maxSpecialisations, maxMethods int) {
	if maxConstantPoolSize > 0 {
		compiledFunction.maxConstantPoolSize = maxConstantPoolSize
	}
	if maxSpecialisations > 0 {
		compiledFunction.maxSpecialisations = maxSpecialisations
	}
	if maxMethods > 0 {
		compiledFunction.maxMethods = maxMethods
	}
}

// SetMethodTable installs the method-name -> function-index table, used when a compiled
// function is rebuilt from serialised bytecode.
//
// Takes table (map[string]uint16) which becomes the function's method table.
func (compiledFunction *CompiledFunction) SetMethodTable(table map[string]uint16) {
	compiledFunction.methodTable = table
}

// MethodTable returns the function's method-name -> function-index table.
//
// Returns map[string]uint16 which is the live table; callers must not mutate it.
func (compiledFunction *CompiledFunction) MethodTable() map[string]uint16 {
	return compiledFunction.methodTable
}

// SetFunctions replaces the function table wholesale, used by tests that assemble a
// program by hand.
//
// Takes functions ([]*CompiledFunction) which becomes the table.
func (compiledFunction *CompiledFunction) SetFunctions(functions []*CompiledFunction) {
	compiledFunction.Functions = functions
}

// AppendFunction adds a child function to the receiver's table and returns its index.
//
// Takes child (*CompiledFunction) which is appended to the table.
//
// Returns uint16 which is the child's index in the table.
func (compiledFunction *CompiledFunction) AppendFunction(child *CompiledFunction) uint16 {
	compiledFunction.Functions = append(compiledFunction.Functions, child)

	return safeconv.IntToUint16(len(compiledFunction.Functions) - 1)
}

// FunctionName returns the function's qualified name.
//
// Returns string which is the fully qualified function name.
func (compiledFunction *CompiledFunction) FunctionName() string { return compiledFunction.Name }

// BodyLen returns the number of bytecode instructions.
//
// Returns int which is the instruction count in the body.
func (compiledFunction *CompiledFunction) BodyLen() int { return len(compiledFunction.Body) }

// SubFunctions returns the nested function literals defined within the function.
//
// Returns []*CompiledFunction which holds the closure definitions nested inside the
// function.
func (compiledFunction *CompiledFunction) SubFunctions() []*CompiledFunction {
	return compiledFunction.Functions
}

// RegisterCounts returns the peak register usage per bank.
//
// Returns [NumRegisterKinds]uint32 which holds the maximum register index used in each
// register bank.
func (compiledFunction *CompiledFunction) RegisterCounts() [isa.NumRegisterKinds]uint32 {
	return compiledFunction.NumRegisters
}

// ReflectFuncType returns the reflect.Type for the method's signature excluding the
// receiver parameter. Used for creating bound method values via reflect.MakeFunc.
//
// Returns the function type and true if the function has parameters, or nil and false
// otherwise.
func (compiledFunction *CompiledFunction) ReflectFuncType() (reflect.Type, bool) {
	if len(compiledFunction.ParameterKinds) == 0 {
		return nil, false
	}
	if signature := compiledFunction.SignatureReflectType; signature != nil && signature.Kind() == reflect.Func {
		return signature, true
	}
	parameterKinds := compiledFunction.ParameterKinds[1:]
	inTypes := make([]reflect.Type, len(parameterKinds))
	for i, k := range parameterKinds {
		inTypes[i] = KindDefaultReflectType(k)
	}
	outTypes := make([]reflect.Type, len(compiledFunction.ResultKinds))
	for i, k := range compiledFunction.ResultKinds {
		outTypes[i] = compiledFunction.ResultReflectType(i, k)
	}
	return reflect.FuncOf(compiledFunction.VariadicSafeInTypes(inTypes), outTypes, compiledFunction.IsVariadic), true
}

// VariadicSafeInTypes makes an erased parameter list acceptable to reflect.FuncOf for a
// variadic function by replacing a bank-derived `any` final parameter with the recorded
// VariadicSliceType, or []any when the function was loaded from bytecode.
//
// Takes inTypes ([]reflect.Type) which is the erased parameter list.
//
// Returns []reflect.Type which is inTypes itself unless the last entry was replaced.
func (compiledFunction *CompiledFunction) VariadicSafeInTypes(inTypes []reflect.Type) []reflect.Type {
	if !compiledFunction.IsVariadic || len(inTypes) == 0 || inTypes[len(inTypes)-1].Kind() == reflect.Slice {
		return inTypes
	}
	sliceType := compiledFunction.VariadicSliceType
	if sliceType == nil {
		sliceType = reflect.TypeFor[[]any]()
	}
	out := append([]reflect.Type(nil), inTypes...)
	out[len(out)-1] = sliceType
	return out
}

// ReflectMethodExprType returns the reflect.Type for the method's signature including the
// receiver as the first parameter. General-register params use interface{} so concrete
// struct types can pass through reflect.Call without type mismatches.
//
// Returns the function type and true if the function has parameters, or nil and false
// otherwise.
func (compiledFunction *CompiledFunction) ReflectMethodExprType() (reflect.Type, bool) {
	if len(compiledFunction.ParameterKinds) == 0 {
		return nil, false
	}
	if precise, ok := compiledFunction.preciseMethodExprType(); ok {
		return precise, true
	}
	inTypes := make([]reflect.Type, len(compiledFunction.ParameterKinds))
	for i, k := range compiledFunction.ParameterKinds {
		if k == isa.RegisterGeneral {
			inTypes[i] = reflect.TypeFor[any]()
		} else {
			inTypes[i] = KindDefaultReflectType(k)
		}
	}
	outTypes := make([]reflect.Type, len(compiledFunction.ResultKinds))
	for i, k := range compiledFunction.ResultKinds {
		outTypes[i] = compiledFunction.ResultReflectType(i, k)
	}
	return reflect.FuncOf(compiledFunction.VariadicSafeInTypes(inTypes), outTypes, compiledFunction.IsVariadic), true
}

// ResultReflectType returns the exact reflect.Type recorded for result position index,
// falling back to the bank's canonical type when none was recorded. Function types built
// for reflect.MakeFunc must agree with the values extractResult produces, which honour
// the same record.
//
// Takes index (int) which is the result position.
// Takes kind (isa.RegisterKind) which is the result's register bank.
//
// Returns reflect.Type which is never nil.
func (compiledFunction *CompiledFunction) ResultReflectType(index int, kind isa.RegisterKind) reflect.Type {
	if index < len(compiledFunction.ResultReflectTypes) && compiledFunction.ResultReflectTypes[index] != nil {
		return compiledFunction.ResultReflectTypes[index]
	}
	return KindDefaultReflectType(kind)
}

// preciseMethodExprType builds a method expression's type from the recorded receiver type
// and signature, so the value assigns to a func-typed slot declared with the same
// parameter types.
//
// Returns reflect.Type which is func(receiver, params...) results.
// Returns bool which is true when both the receiver type and the signature were recorded.
func (compiledFunction *CompiledFunction) preciseMethodExprType() (reflect.Type, bool) {
	signature := compiledFunction.SignatureReflectType
	receiver := compiledFunction.MethodReceiverReflectType
	if signature == nil || receiver == nil || signature.Kind() != reflect.Func {
		return nil, false
	}
	inTypes := make([]reflect.Type, 0, signature.NumIn()+1)
	inTypes = append(inTypes, receiver)
	for in := range signature.Ins() {
		inTypes = append(inTypes, in)
	}
	outTypes := make([]reflect.Type, 0, signature.NumOut())
	for out := range signature.Outs() {
		outTypes = append(outTypes, out)
	}
	return reflect.FuncOf(inTypes, outTypes, signature.IsVariadic()), true
}

// MonomorphicCacheEntry is an immutable atomic snapshot of a call site's resolved
// monomorphic dispatch. Once published, an entry is never mutated.
type MonomorphicCacheEntry struct {
	// ReceiverType is the dereferenced concrete receiver type the cache observed. nil when
	// the entry is the disabled sentinel.
	ReceiverType reflect.Type

	// Callee caches the resolved *CompiledFunction so the hot path skips the function-table
	// indirection. Atomically published as part of the entry.
	Callee *CompiledFunction

	// ReceiverTypeWord is ReceiverType's *abi.Type data word, letting the dispatch hot path
	// compare a single machine word instead of a reflect.Type interface equality.
	ReceiverTypeWord uintptr

	// FunctionIndex is the methodTable-resolved FunctionIndex for ReceiverType. Only valid
	// when ReceiverType is non-nil.
	FunctionIndex uint16
}

// InnerCalleeCacheEntry is an immutable atomic snapshot of an inner-Eval callee
// resolution within an InlineDescriptor. Once published, never mutated.
type InnerCalleeCacheEntry struct {
	// ReceiverType is the dereferenced concrete receiver type this entry was resolved for.
	// nil entries are never published.
	ReceiverType reflect.Type

	// Callee is the resolved *CompiledFunction for the inner Eval dispatch on ReceiverType.
	Callee *CompiledFunction
}

// InlineShape selects the runtime inlining path for a call site. InlineShapeNone is the
// default.
type InlineShape uint8

// InlineDescriptor caches the classification of a call-site receiver pair. Atomically
// published; tearing only costs a re-classification, never correctness.
type InlineDescriptor struct {
	// RecvType is the concrete receiver type this descriptor was classified against. nil
	// entries are never published.
	RecvType reflect.Type

	// Callee is the resolved compiled function for this (site, type). Always populated - the
	// fallback path needs it even when the inline shape is none.
	Callee *CompiledFunction

	// InnerCalleeSlots memoises inner Eval callee resolutions, using atomic load/store so
	// entries cannot tear with round-robin eviction via InnerCalleeVictim.
	InnerCalleeSlots [InnerCalleeSlotCount]unsafe.Pointer

	// MaskValue captures the trailing `& const` mask in the binop body.
	MaskValue uint64

	// LeftLayout is the field-read layout for the binop method's left operand access. Valid
	// only when shape == InlineShapeBinopUint.
	LeftLayout StructFieldLayout

	// RightLayout is the field-read layout for the binop method's right operand access.
	// Valid only when shape == InlineShapeBinopUint.
	RightLayout StructFieldLayout

	// InnerCalleeVictim is the round-robin eviction counter for InnerCalleeSlots. Not
	// atomically updated; tearing only costs a sub-optimal eviction, never correctness.
	InnerCalleeVictim uint32

	// BinopOpcode encodes which arithmetic op the binop body applies (opAddUint, opSubUint,
	// opMulUint, opModUint). Valid only when shape == InlineShapeBinopUint.
	BinopOpcode isa.Opcode

	// Shape selects the inlined runtime path. InlineShapeNone means "fall back to standard
	// pushCompiledFrame or runTinyLeafInline".
	Shape InlineShape

	// MaskApplies is true when the binop body has a trailing `& MaskValue` masking step.
	MaskApplies bool
}

// SpecialisationKey is a fixed-size tuple of reflect.Type used as a generic instantiation
// key, with the Names field disambiguating distinct named types that erase to the same
// reflect.Type (such as `Celsius` vs `Fahrenheit`, both float64).
type SpecialisationKey struct {
	// Types holds the reflect.Type of each generic type-argument in instantiation order.
	Types [MaxSpecialisationTypeArgs]reflect.Type

	// Names holds the fully-qualified source type string per type-argument, disambiguating
	// named types that erase to the same reflect.Type.
	Names [MaxSpecialisationTypeArgs]string
}

// UpvalueDescriptor describes a single captured variable in a closure.
type UpvalueDescriptor struct {
	// Index is the register index in the enclosing scope, or the upvalue index if IsLocal is
	// false (transitive capture).
	Index uint8

	// Kind is the register bank of the captured variable. When the variable is heap-promoted
	// (IsIndirect is true) this is registerGeneral because the parent's register holds the
	// *T pointer to the heap cell.
	Kind isa.RegisterKind

	// IsLocal is true when the upvalue captures a register directly from the immediately
	// enclosing function. When false, the upvalue is captured transitively from the
	// enclosing function's own upvalue table.
	IsLocal bool

	// IsIndirect is true when the captured variable is heap-promoted in the enclosing scope.
	// The closure reads and writes through the *T pointer rather than per-kind snapshots.
	IsIndirect bool

	// OriginalKind names the register bank the variable had before heap promotion.
	// Meaningful only when IsIndirect is true.
	OriginalKind isa.RegisterKind
}

// CallSite describes a function call in the compiled bytecode. It stores all the
// information the VM needs to set up arguments and retrieve return values without
// additional opcodes.
//
//exhaustruct:ignore
type CallSite struct {
	// MethodICSlots is the polymorphic inline cache for opCallMethod dispatch, read and
	// published via atomic load/store so concurrent goroutines cannot tear an entry and
	// wrong-eviction only costs a re-resolve, never correctness. Runtime-only.
	MethodICSlots [methodICSlotCount]unsafe.Pointer

	// InlineDescriptorSlots caches per-receiver-type InlineDescriptor classifications for
	// the inlineable method-call fast path. Atomic load/store, round-robin eviction.
	InlineDescriptorSlots [inlineDescriptorSlotCount]unsafe.Pointer

	// RuntimeVariadicSliceType is the reflect.Type of the variadic parameter's slice (such
	// as []int for ...int). nil for non-variadic sites and spread-slice sites.
	RuntimeVariadicSliceType reflect.Type

	// nativeParams caches the callee's parameter types and variadic flag for native calls as
	// a raw unsafe.Pointer (not atomic.Pointer) because CallSite values are copied and
	// atomic.Pointer embeds noCopy. Use NativeParamCacheLoad / NativeParamCacheStore.
	nativeParams unsafe.Pointer

	// NativeFastPath publishes the fast-path classification for native call sites, with nil
	// meaning unprobed. Read and written atomically because call sites are shared across
	// per-goroutine VMs.
	NativeFastPath unsafe.Pointer

	// CachedClosureRoot is the root *CompiledFunction extracted from the last-seen closure,
	// paired with CachedClosurePtr.
	CachedClosureRoot *CompiledFunction

	// LastReceiverCallee partners with LastReceiverTypeWord; the pair is written together on
	// every IC resolve. Tearing safety: see LastReceiverTypeWord below.
	LastReceiverCallee *CompiledFunction

	// CachedCallee is the pre-bound *CompiledFunction for the FunctionIndex, avoiding the
	// function-table lookup on the hot path. nil for closure calls and unresolved callees.
	CachedCallee *CompiledFunction

	// CachedClosurePtr caches the last-seen *RuntimeClosure raw pointer at a closure call
	// site. Only read and written when the family has no goroutines, because CallSites are
	// shared and a concurrent multi-word update would tear.
	CachedClosurePtr unsafe.Pointer

	// CachedClosureCallee is the *CompiledFunction extracted from the last-seen closure
	// value, paired with CachedClosurePtr.
	CachedClosureCallee *CompiledFunction

	// NativeFunctionPath caches the dotted symbol identifier (such as "net/http.Get") for
	// CapabilityHook consultation, resolved lazily via runtime.FuncForPC. Runtime-only.
	NativeFunctionPath string

	// ParameterInterfaceFlags marks each fixed parameter that has interface kind, with nil
	// when no interface parameters exist. Index 0 is the first fixed parameter (after the
	// receiver for methods).
	ParameterInterfaceFlags []bool

	// VariadicArgumentsBuffer is a pre-allocated buffer for variadic fast-path calls. Avoids
	// make([]any, n) per call for functions like fmt.Sprintf that take ...interface{}
	// parameters.
	VariadicArgumentsBuffer []any

	// CachedClosureUpvalues holds the upvalue cells extracted from the last-seen closure,
	// paired with CachedClosurePtr.
	CachedClosureUpvalues []*UpvalueCell

	// ArgCopyProgram is the pre-computed per-argument copy plan for this call site. nil for
	// sites where the callee was not resolved at compile time.
	ArgCopyProgram []CallArgCopy

	// ArgumentStaticTypeNames holds each argument's bare source-level named type (such as
	// "Colour"), with an empty string for unnamed types. Populated only for native call
	// sites.
	ArgumentStaticTypeNames []string

	// ArgumentStaticTypeStrings holds each argument's Go-syntax type representation (such as
	// "int", "*main.Bomb"), used by the %T fmt interceptor.
	ArgumentStaticTypeStrings []string

	// Arguments records where each argument lives in the CALLER's frame.
	Arguments []VarLocation

	// LinkedTypeArgs holds the instantiated type arguments for a //piko:link-routed generic
	// call, resolved at compile time from types.Info.Instances. When non-empty the native
	// call handler prepends one reflect.Type value per element before the regular arguments
	// and skips the NativeFastPath / nativeParams caches.
	LinkedTypeArgs []reflect.Type

	// Returns records where to put each return value in the CALLER's frame after the call
	// completes.
	Returns []VarLocation

	// AsmMethodRejectedTypeWords is a small negative cache of rejected receiver type words
	// at this call site.
	AsmMethodRejectedTypeWords [AsmMethodRejectedCacheSize]uintptr

	// LastReceiverTypeWord is the raw *abi.Type word of the most recently seen receiver for
	// the single-compare hot path.
	LastReceiverTypeWord uintptr

	// InlineDescriptorVictim is the round-robin eviction counter for InlineDescriptorSlots.
	// Not atomically updated; tearing only costs a sub-optimal eviction, never correctness.
	InlineDescriptorVictim uint32

	// MethodICVictim is the round-robin eviction counter for MethodICSlots. Incremented
	// atomically on every cache miss.
	MethodICVictim uint32

	// FunctionIndex is the index into the enclosing function's functions slice for the
	// callee. Ignored when IsClosure is true.
	FunctionIndex uint16

	// ClosureRegister is the general register holding the closure value. Only used when
	// IsClosure is true.
	ClosureRegister uint8

	// NativeRegister is the general register holding the native function value. Only used
	// when IsNative is true.
	NativeRegister uint8

	// IsClosure is true when the callee is a closure stored in a general register rather
	// than a static function reference.
	IsClosure bool

	// IsNative is true when the callee is a native Go function stored in a general register
	// (not a compiled function).
	IsNative bool

	// IsMethod is true when the callee is a bound method obtained via isa.SubOpGetMethod,
	// where handleCallNative validates the cached fast path against the current receiver
	// address before reuse.
	IsMethod bool

	// MethodReceiverRegister is the general register holding the receiver for method calls
	// (only valid when IsMethod is true). Used to validate the cached fast path by comparing
	// the receiver address across invocations.
	MethodReceiverRegister uint8

	// RuntimeVariadicNumFixed counts the fixed (non-variadic) parameters that precede the
	// variadic slice parameter in the callee signature. Only meaningful when
	// RuntimeVariadicSliceType is non-nil.
	RuntimeVariadicNumFixed uint8

	// IsEllipsisSpread is true when the source call used the `...` ellipsis spread on the
	// final argument, requiring dispatch via reflect.Value.CallSlice instead of Call (so the
	// trailing slice is passed as the variadic parameter rather than spread into individual
	// values). Only meaningful for native calls.
	IsEllipsisSpread bool

	// BlocksHostGoroutine marks a native call that parks the host goroutine (such as
	// sync.WaitGroup.Wait, sync.Mutex.Lock). In safe mode the family lock must be released
	// around such a call to avoid deadlock.
	BlocksHostGoroutine bool

	// TailReuseFrameInPlace marks a tail-call site that can reuse the caller's register file
	// in place because the callee's register counts match in every bank. Only meaningful for
	// opTailCall sites.
	TailReuseFrameInPlace bool

	// TailArgsAlias marks a tail-call site whose argument copy needs a snapshot to avoid
	// source-destination aliasing. Only meaningful when TailReuseFrameInPlace is true.
	TailArgsAlias bool

	// RecursionUnrolled marks a self-recursive call site already spliced once, preventing
	// infinite expansion.
	RecursionUnrolled bool
}

// NativeParamCacheLoad returns the site's published native parameter cache.
//
// Returns nil until cacheParamTypes has published one.
func (site *CallSite) NativeParamCacheLoad() *NativeParamCache {
	return (*NativeParamCache)(atomic.LoadPointer(&site.nativeParams))
}

// NativeParamCacheStore publishes cache as the site's native parameter cache.
//
// Takes cache (*NativeParamCache) which must be fully built and never mutated afterwards.
func (site *CallSite) NativeParamCacheStore(cache *NativeParamCache) {
	atomic.StorePointer(&site.nativeParams, unsafe.Pointer(cache))
}

// NativeParamTypes returns the site's cached native parameter types, or nil before
// cacheParamTypes has run.
//
// Returns the parameter types in declaration order, or nil when the cache is unpopulated.
func (site *CallSite) NativeParamTypes() []reflect.Type {
	if cache := site.NativeParamCacheLoad(); cache != nil {
		return cache.Types
	}
	return nil
}

// NativeParamCache is a call site's resolved native-call parameter information. Immutable
// once stored.
type NativeParamCache struct {
	// Types are the callee's declared parameter types, in declaration order.
	Types []reflect.Type

	// IsVariadic is the callee's reflect.Type.IsVariadic result.
	IsVariadic bool

	// Sleep is true for time.Sleep (or the wall clock's Sleep the service installs in its
	// place), which the VM runs as an interruptible wait so a deadline can end it.
	Sleep bool

	// FmtArguments is true for a variadic ...any function of package fmt: its operands are
	// formatted, so synthesised struct values are wrapped to hide the sentinel field.
	FmtArguments bool

	// FmtScan is true for the scanning functions of package fmt (Scan, Sscanf, Fscanln,
	// ...): their operands are pointers to fill, which take the ordinary interface adapters
	// (a script Scan method) rather than the per-verb printing wrappers.
	FmtScan bool

	// EncodingArguments is true for an encoding marshaller (json.Marshal, xml.Marshal and
	// their Indent and Encoder forms). Their `any` arguments are rebuilt so nested script
	// values with a MarshalJSON method reach the encoder as marshalers.
	EncodingArguments bool
}

// InterfaceMethodRequirement is a decoded interface method requirement.
type InterfaceMethodRequirement struct {
	// Name is the method name.
	Name string

	// Shape is the SignatureShapeString of the required method, or "" when unrecorded.
	Shape string

	// Params is the required parameter count, or -1 when unrecorded.
	Params int

	// Results is the required result count, or -1 when unrecorded.
	Results int
}

// HeldKindIsDirectPointer reports whether a reflect.Kind held in an interface value's
// eface stores its data slot as the pointer value itself (rather than as a pointer to
// heap storage for the value).
//
// Takes kind (reflect.Kind) which is the held value's kind to classify.
//
// Returns true when the kind packs into the eface data slot directly.
func HeldKindIsDirectPointer(kind reflect.Kind) bool {
	switch kind {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Chan, reflect.Map, reflect.Func:
		return true
	default:
	}
	return false
}

// EncodeInterfaceMethodRequirement renders one interface method as the string a type
// assertion checks a dynamic value against.
//
// Takes name (string) which is the method name.
// Takes signature (*types.Signature) which is the method's signature.
//
// Returns string which is the encoded requirement.
func EncodeInterfaceMethodRequirement(name string, signature *types.Signature) string {
	if signature == nil {
		return name
	}
	return name + interfaceMethodRequirementSeparator + strconv.Itoa(signature.Params().Len()) +
		interfaceMethodRequirementSeparator + strconv.Itoa(signature.Results().Len()) +
		interfaceMethodRequirementSeparator + SignatureShapeString(signature)
}

// EncodeInterfaceMethodRequirementArity renders a requirement with the arity but no
// shape, for interface methods whose signature still mentions a type parameter.
//
// Takes name (string) which is the method name.
// Takes signature (*types.Signature) which is the method's signature.
//
// Returns string which is the encoded requirement.
func EncodeInterfaceMethodRequirementArity(name string, signature *types.Signature) string {
	return name + interfaceMethodRequirementSeparator + strconv.Itoa(signature.Params().Len()) +
		interfaceMethodRequirementSeparator + strconv.Itoa(signature.Results().Len())
}

// DecodeInterfaceMethodRequirement splits an encoded requirement into its parts. A bare
// name (nothing recorded) yields -1 counts and an empty shape, meaning "unchecked".
//
// Takes requirement (string) which is the encoded requirement.
//
// Returns InterfaceMethodRequirement which is the decoded requirement.
func DecodeInterfaceMethodRequirement(requirement string) InterfaceMethodRequirement {
	parts := strings.SplitN(requirement, interfaceMethodRequirementSeparator, requirementFieldCount)
	decoded := InterfaceMethodRequirement{Name: parts[0], Shape: "", Params: -1, Results: -1}
	if len(parts) < requirementArityFieldCount {
		return decoded
	}
	params, paramsErr := strconv.Atoi(parts[1])
	results, resultsErr := strconv.Atoi(parts[2])
	if paramsErr != nil || resultsErr != nil {
		return decoded
	}
	decoded.Params, decoded.Results = params, results
	if len(parts) == requirementFieldCount {
		decoded.Shape = parts[requirementFieldCount-1]
	}
	return decoded
}

// SignatureShapeString renders a signature's parameter and result types without the
// receiver or parameter names, package-qualifying named types so two signatures render
// identically exactly when Go considers them identical.
//
// Takes signature (*types.Signature) which is the signature to render.
//
// Returns string such as "func(*main.T1) (int, error)" or "func($0) $0".
func SignatureShapeString(signature *types.Signature) string {
	if signature == nil {
		return ""
	}
	placeholders := receiverTypeParamPlaceholders(signature)
	qualifier := func(pkg *types.Package) string { return pkg.Name() }
	var builder strings.Builder
	builder.WriteString("func(")
	writeShapeParams(&builder, signature, qualifier, placeholders)
	builder.WriteString(")")
	writeShapeResults(&builder, signature.Results(), qualifier, placeholders)
	return builder.String()
}

// SubstituteShapeTypeArgs replaces the "$i" placeholders of a generic method's shape with
// the instantiation's type arguments, in the order the sentinel tag lists them.
//
// Takes shape (string) which is a SignatureShapeString with placeholders.
// Takes typeArgs ([]string) which are the instantiation's rendered type arguments.
//
// Returns string which is the concrete shape.
func SubstituteShapeTypeArgs(shape string, typeArgs []string) string {
	if !strings.Contains(shape, "$") {
		return shape
	}
	for i, typeArg := range slices.Backward(typeArgs) {
		shape = strings.ReplaceAll(shape, "$"+strconv.Itoa(i), typeArg)
	}
	return shape
}

// writeShapeParams renders the parameter types of signature, the variadic one as `...E`.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes signature (*types.Signature) which is the rendered signature.
// Takes qualifier (types.Qualifier) which names packages.
// Takes placeholders (map[*types.TypeParam]string) which substitutes receiver type
// parameters.
func writeShapeParams(builder *strings.Builder, signature *types.Signature, qualifier types.Qualifier, placeholders map[*types.TypeParam]string) {
	params := signature.Params()
	for i := range params.Len() {
		if i > 0 {
			builder.WriteString(", ")
		}
		if signature.Variadic() && i == params.Len()-1 {
			if slice, ok := params.At(i).Type().(*types.Slice); ok {
				builder.WriteString("...")
				builder.WriteString(shapeTypeString(slice.Elem(), qualifier, placeholders))
				continue
			}
		}
		builder.WriteString(shapeTypeString(params.At(i).Type(), qualifier, placeholders))
	}
}

// writeShapeResults renders the result types: nothing, ` T`, or ` (T, U)`.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes results (*types.Tuple) which are the result variables.
// Takes qualifier (types.Qualifier) which names packages.
// Takes placeholders (map[*types.TypeParam]string) which substitutes receiver type
// parameters.
func writeShapeResults(builder *strings.Builder, results *types.Tuple, qualifier types.Qualifier, placeholders map[*types.TypeParam]string) {
	switch {
	case results.Len() == 1:
		builder.WriteString(" ")
		builder.WriteString(shapeTypeString(results.At(0).Type(), qualifier, placeholders))
	case results.Len() > 1:
		builder.WriteString(" (")
		for i := range results.Len() {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(shapeTypeString(results.At(i).Type(), qualifier, placeholders))
		}
		builder.WriteString(")")
	default:
	}
}

// receiverTypeParamPlaceholders maps each type parameter of a generic receiver to its
// placeholder ("$0", "$1", ...) in declaration order.
//
// Takes signature (*types.Signature) which is the method signature.
//
// Returns map[*types.TypeParam]string which is empty for a non-generic receiver.
func receiverTypeParamPlaceholders(signature *types.Signature) map[*types.TypeParam]string {
	placeholders := map[*types.TypeParam]string{}
	if signature.RecvTypeParams() != nil {
		for i := range signature.RecvTypeParams().Len() {
			placeholders[signature.RecvTypeParams().At(i)] = "$" + strconv.Itoa(i)
		}
	}
	return placeholders
}

// shapeTypeString renders one type for a signature shape, with receiver type parameters
// replaced by their placeholders.
//
// Takes t (types.Type) which is the type to render.
// Takes qualifier (types.Qualifier) which names packages.
// Takes placeholders (map[*types.TypeParam]string) which maps type parameters.
//
// Returns string which is the rendering.
func shapeTypeString(t types.Type, qualifier types.Qualifier, placeholders map[*types.TypeParam]string) string {
	if len(placeholders) == 0 {
		return types.TypeString(t, qualifier)
	}
	rendered := types.TypeString(t, qualifier)
	for param, placeholder := range placeholders {
		rendered = replaceTypeParamName(rendered, param.Obj().Name(), placeholder)
	}
	return rendered
}

// replaceTypeParamName replaces whole-identifier occurrences of name in rendered with
// replacement, leaving identifiers that merely contain it (main.Tree for T) alone.
//
// Takes rendered (string) which is a rendered type.
// Takes name (string) which is the type parameter's name.
// Takes replacement (string) which is its placeholder.
//
// Returns string which is the rewritten rendering.
func replaceTypeParamName(rendered, name, replacement string) string {
	var builder strings.Builder
	for i := 0; i < len(rendered); {
		if strings.HasPrefix(rendered[i:], name) && !isIdentifierByte(byteBefore(rendered, i)) && !isIdentifierByte(byteAt(rendered, i+len(name))) && byteBefore(rendered, i) != '.' {
			builder.WriteString(replacement)
			i += len(name)
			continue
		}
		builder.WriteByte(rendered[i])
		i++
	}
	return builder.String()
}

// isIdentifierByte reports whether b can continue a Go identifier.
//
// Takes b (byte) which is the byte to test.
//
// Returns bool which is true for letters, digits and underscore.
func isIdentifierByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= asciiLimit
}

// byteBefore returns the byte before index i, or 0 at the start.
//
// Takes s (string) which is the source string.
// Takes i (int) which is the position.
//
// Returns byte which is s[i-1] or 0 when i is 0.
func byteBefore(s string, i int) byte {
	if i == 0 {
		return 0
	}
	return s[i-1]
}

// byteAt returns the byte at index i, or 0 past the end.
//
// Takes s (string) which is the source string.
// Takes i (int) which is the position.
//
// Returns byte which is s[i] or 0 when i >= len(s).
func byteAt(s string, i int) byte {
	if i >= len(s) {
		return 0
	}
	return s[i]
}
