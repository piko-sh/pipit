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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sync/atomic"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

const (
	// maxCallDepth is the default maximum call stack depth before a stack overflow error is
	// raised. Overridable via WithMaxCallDepth.
	maxCallDepth = 10000

	// cancellationCheckMask is the bitmask applied to the operation counter to decide when
	// to check for context cancellation.
	cancellationCheckMask uint32 = 0x3FF

	// opcodeTableSize is the number of entries in the opcode dispatch table (one per
	// possible uint8 opcode value).
	opcodeTableSize = 256

	// checkpointFlagGCPending signals a pending arena MinorGC at the next safe point.
	checkpointFlagGCPending uint8 = 1 << 0

	// arenaConcatStringAvgBytes is the per-call byte budget assumed for an
	// isa.OpConcatString. Real-world string concatenations span orders of magnitude; 32
	// bytes is a defensive estimate that covers small-to-medium results without blowing out
	// the slab for programs that never concatenate large strings.
	arenaConcatStringAvgBytes = 32

	// arenaConcatRuneStringAvgBytes is the per-call byte budget for isa.OpConcatRuneString.
	// The base string contributes most of the length; the rune adds at most 4 UTF-8 bytes.
	arenaConcatRuneStringAvgBytes = 16

	// arenaRuneToStringMaxBytes is the worst-case UTF-8 length of a single rune.
	arenaRuneToStringMaxBytes = 4

	// arenaBytesToStringAvgBytes is the per-call byte budget for isa.SubOpBytesToString. The
	// actual byte slice length is dynamic; 64 bytes covers typical tokenisation /
	// small-string conversion patterns without bloating tiny programs.
	arenaBytesToStringAvgBytes = 64

	// arenaItoaMaxBytes is the worst-case byte length of an int64 formatted in base 10
	// (matches itoaMaxDigitsBase10 in register_arena_unsafe.go but expressed here so the
	// hint module can stand alone).
	arenaItoaMaxBytes = 20

	// arenaFormatIntMaxBytes is the worst-case byte length of an int64 formatted in any base
	// accepted by strconv.FormatInt (base 2 is the widest at 64 binary digits + 1 sign).
	arenaFormatIntMaxBytes = 65

	// arenaMakeSliceAvgCapacity is the per-occurrence element budget for makeSlice.
	arenaMakeSliceAvgCapacity = 16

	// inlineUpvalueCells is the number of upvalue-cell pointers stored inline in
	// RuntimeClosure to avoid a heap allocation per closure creation.
	inlineUpvalueCells = 4

	// snapshotChunkSize is the number of snapshotSliceHeader slots allocated per chunk.
	snapshotChunkSize = 4096

	// boundaryChunkSize is the slot-count cap of a per-type boundary-snapshot chunk.
	boundaryChunkSize = 4096

	// maxArenaPreSizeBacking caps each per-bank backing total so pathological hint counts
	// cannot overflow the pre-size multiplication.
	maxArenaPreSizeBacking = 1 << 26

	// maxFrameStackPreSize bounds the compile-time frame-stack pre-allocation. Programs that
	// genuinely recurse beyond this limit still grow lazily via growCallStack; the cap
	// exists to keep a hostile bytecode whose call graph fans out aggressively from
	// demanding gigabytes of frame storage up-front.
	maxFrameStackPreSize = 4096

	// maxBoxSlabPreSize bounds each of the five per-kind box slabs (int / uint / float /
	// string / complex). 65536 slots per kind covers realistic pack-heavy workloads while
	// leaving the cap well below the saturating product overflow point.
	maxBoxSlabPreSize = 1 << 16

	// maxUpvalueSlabPreSize bounds the UpvalueCell and upvalueReference slabs.
	maxUpvalueSlabPreSize = 1 << 16

	// maxGenericBytesPreSize bounds the genericBytesSlab pre-size in bytes (4 MiB ceiling).
	maxGenericBytesPreSize = 4 << 20

	// maxSliceHeaderPreSize bounds the sliceHeaderSlab pre-size in slots (65536 slots ~= 1.5
	// MiB at 24 bytes per slot).
	maxSliceHeaderPreSize = 1 << 16

	// frameStackSafetyMargin is the bumper added on top of the static max call depth before
	// clamping to maxFrameStackPreSize.
	frameStackSafetyMargin = 16

	// boxSlabPerOccurrenceFactor multiplies each isa.OpPackInterface or isa.OpPackTyped
	// count to derive the per-kind box slab budget. Conservative 4x absorbs amplification
	// from loop bodies.
	boxSlabPerOccurrenceFactor = 4

	// arenaMakeSliceGenericAvgBytes is the per-occurrence byte charge for isa.OpMakeSlice on
	// the general bank (slice-header + minimal element budget).
	arenaMakeSliceGenericAvgBytes = 32

	// arenaMakeMapAvgBytes is the per-occurrence byte charge for isa.SubOpTier2MakeMap
	// (covers the hmap header allocation routed through the generic byte slab).
	arenaMakeMapAvgBytes = 48

	// arenaAllocIndirectAvgBytes is the per-occurrence byte charge for isa.OpAllocIndirect
	// (heap-promoted local storage; conservative estimate of a small struct).
	arenaAllocIndirectAvgBytes = 64
)

// VM is the pipit bytecode interpreter, created fresh via NewVM for each invocation of a
// compiled program and borrowing a register arena that is pooled and reset between runs.
//
//nolint:govet // related fields kept together
//exhaustruct:ignore
type VM struct {
	// evalResult holds the return value from handleReturn when the base frame returns.
	// Extracted by the dispatch loop after opDone.
	evalResult any

	// evalError holds the error from a handler that returns opPanicError. Extracted by the
	// dispatch loop after opPanicError.
	evalError error

	// ctx is the execution context, read at blocking points and goroutine spawn via
	// executionContext().
	ctx context.Context

	// StderrWriter is the writer for print/println output, defaulting to os.Stderr but
	// overridable for testing.
	StderrWriter io.Writer

	// liveCtx points at the dispatch context currently executing on this VM, letting Go-path
	// handlers that retarget per-frame pointers (the closure AsmCallInfo fill's
	// copy-on-write) sync the live context immediately instead of waiting for the next
	// frame-change rebuild. Set alongside ctx.vm at population; only read while dispatching.
	liveCtx *dispatchContext

	// symbols provides access to pre-registered native functions and values.
	symbols *symtab.SymbolRegistry

	// rootFunction is the top-level compiled function that owns the method table and
	// function table. Set during execute().
	rootFunction *program.CompiledFunction

	// AsmCallInfoTables holds pre-computed AsmCallInfo tables for each function, keyed by
	// *CompiledFunction. Built during execute() for ASM-inlined call/return.
	AsmCallInfoTables map[*program.CompiledFunction][]AsmCallInfo

	// PrivateACITables marks functions whose AsmCallInfo table this VM has cloned for
	// runtime closure fills; nil until the first fill. The shared per-root tables are
	// read-only, so the map itself is also shallow-cloned before the first mutation.
	PrivateACITables map[*program.CompiledFunction]bool

	// AsmMethodEntryTables owns the per-receiver-type AsmCallInfo entries that private
	// call-info tables reference, keeping them GC-reachable at stable addresses.
	AsmMethodEntryTables map[*program.CallSite]*asmMethodEntryTable

	// callbackVMs caches one idle callback VM for the reflect.MakeFunc wrappers made on this
	// VM (makeClosureWrapperFunc); drained whenever this VM releases its arena.
	callbackVMs *callbackVMCache

	// asmCallInfoRoot is the root function asmCallInfoBasesByFunction was built for; the
	// populators bail while a cross-bundle closure has swapped vm.RootFunction elsewhere.
	asmCallInfoRoot *program.CompiledFunction

	// Arena provides pooled register bank allocation. When set, PushFrame uses
	// arena.AllocRegisters instead of NewRegisters, avoiding per-frame heap allocations.
	Arena *RegisterArena

	// Globals holds package-level variables shared across all functions in the program.
	Globals *GlobalStore

	// modulePath identifies the loaded module owning the execution. Empty for main-program
	// code, set to the module's canonical path inside a LoadModule'd bundle.
	modulePath string

	// asmCallInfoBasesByFunction holds the AsmCallInfo table base for each function index.
	// Aliases the root's shared array until a runtime fill clones it into a private copy.
	asmCallInfoBasesByFunction []uintptr

	// selectCasesBuffer is a reusable buffer for handleSelect reflect cases.
	selectCasesBuffer []reflect.SelectCase

	// selectInfosBuffer is a reusable buffer for handleSelect case metadata.
	selectInfosBuffer []selectCaseInfo

	// functions is the program-level function table. All compiled functions are stored here,
	// referenced by index from CallSites.
	functions []*program.CompiledFunction

	// tailCallArgBuffer is a reusable buffer for snapshotTailCallArgs. Safe to reuse because
	// tail-call processing is synchronous within a single dispatch.
	tailCallArgBuffer []tailCallArgument

	// asmDispatchSaves is a parallel array to CallStack, holding the dispatch register
	// values (codeBase, codeLength, intConstantsBase, floatConstantsBase) saved by the ASM
	// call handler for restoration by the return handler.
	asmDispatchSaves []asmDispatchSave

	// CallStack holds the call frames. The current frame is at CallStack[fp].
	CallStack []CallFrame

	// rootSnapshots runs parallel to CallStack; entry i holds the dispatch state to restore
	// when frame i returns, or nil when no swap happened at push time.
	rootSnapshots []*frameRootSnapshot

	// EvalAllResults holds all return values from handleReturn when the base frame returns,
	// used by callClosureReflect to preserve multi-return values.
	EvalAllResults []any

	// asmCallInfoBases is a parallel array to CallStack, holding the AsmCallInfo table base
	// pointer for the function at each frame. Used by ASM to switch tables on call/return.
	asmCallInfoBases []uintptr

	// Limits holds resource constraints for DoS protection.
	Limits VMLimits

	// nativeState groups the native-call caches; its fields are promoted.
	nativeState

	// goroutineState groups the goroutine and interpreter-lock state; its fields are
	// promoted.
	goroutineState

	// deferState groups the defer and panic-unwind state; its fields are promoted.
	deferState

	// DispatchEntries counts the top-level and nested dispatch loops this VM has entered, so
	// tests can prove that deferred calls run inside the frame's own dispatch rather than
	// re-entering it.
	DispatchEntries int

	// inlineDispatchUintResult is the typed-fast-path return slot, avoiding `any` boxing on
	// the inline dispatch path.
	inlineDispatchUintResult uint64

	// MethodCallGoDispatches counts inline-cache hits served by the Go method dispatch path.
	// Under assembly dispatch a populated site is served in assembly, so the count stays at
	// the handful of fills; tests use it to prove the fast path engaged.
	MethodCallGoDispatches int

	// CostRemaining is the VM's local cost chunk, drawn from the shared tracker budget by
	// refillCostChunk. Non-positive means the next metered instruction must refill.
	CostRemaining int64

	// FramePointer is the current frame pointer (index into CallStack).
	FramePointer int

	// baseFramePointer is the base frame pointer for the current run() invocation, used by
	// opReturn/opReturnVoid to detect when the base frame returns rather than hardcoding
	// zero.
	baseFramePointer int

	// ReturnGoDispatches counts opReturn instructions served by the Go return path after an
	// assembly exit. Returns the inline handlers accept never reach it, so tests use it to
	// prove the multi-value return fast path engaged.
	ReturnGoDispatches int

	// StructLiteralGoAllocations counts subOpAllocStructLiteral instructions served by the
	// Go handler. The first execution in a function publishes the struct-literal table and
	// every later allocation of a qualifying type stays in assembly, so tests use it to
	// prove the fast path engaged.
	StructLiteralGoAllocations int

	// yieldCounter counts instructions for Gosched() yielding.
	yieldCounter uint32

	// Cancelled is set to 1 by a background goroutine when the execution context is done.
	// Checked via a cheap atomic load instead of the more expensive ctx.Err() in the hot
	// loop.
	Cancelled atomic.Uint32

	// inlineDispatchExpectUintResult signals that the active vm.run invocation came from an
	// inline-dispatch path that expects a uint64 result. handleReturn checks this flag at
	// baseFramePointer to decide whether to populate inlineDispatchUintResult (fast path, no
	// alloc) or fall back to vm.evalResult (slow path, any boxing).
	inlineDispatchExpectUintResult bool

	// checkpointFlags is a bitset of pending checkpoint signals raised by the VM's own
	// goroutine, checked at the dispatch loop's safe-point.
	checkpointFlags uint8

	// usesTypedSliceBanks reports whether any reachable function uses a typed-slice or
	// complex register bank, set monotonically and never cleared so
	// repairRegisterBasesFromCallers is a no-op when false.
	usesTypedSliceBanks bool
}

// NewVM creates a new virtual machine ready to execute bytecode. Concurrent use of the
// returned VM is not safe; each goroutine must use its own VM instance.
//
// Takes globals (*GlobalStore) which holds the package-level variable store shared across
// functions.
// Takes symbols (*symtab.SymbolRegistry) which provides access to pre-registered native
// functions and values.
//
// Returns the initialised VM.
func NewVM(ctx context.Context, globals *GlobalStore, symbols *symtab.SymbolRegistry) *VM {
	vm := &VM{
		FramePointer:         -1,
		goroutineID:          rootGoroutineID,
		panicDeferFrames:     nil,
		panicUnwound:         nil,
		panicFrames:          nil,
		recoverEligibleFrame: -1,
		Globals:              globals,
		symbols:              symbols,
		ctx:                  ctx,
		callbackVMs:          new(callbackVMCache),
		debugParkDone:        ctx.Done(),
	}
	if ctx.Done() != nil {
		flag := &vm.Cancelled
		vm.stopWatcher = context.AfterFunc(ctx, func() { flag.Store(1) })
	}
	return vm
}

// SeedExecutionBudgets resets the shared per-execution budgets on the tracker at the
// start of a top-level execution. The tracker outlives individual executions, so the cost
// budget and the shared arena byte total must both start from a known state.
func (vm *VM) SeedExecutionBudgets() {
	vm.SeedCostBudget()
	if vm.Limits.Tracker != nil {
		vm.Limits.Tracker.ArenaBytes.Store(0)
	}
}

// EnsureCallStack sets up an arena and call stack for cold-path VMs (varinit, init) that
// were not created via execute(). The caller must defer vm.ReleaseArena() to return the
// arena to the pool.
func (vm *VM) EnsureCallStack() {
	if vm.Arena == nil {
		vm.Arena = vm.acquireArena()
	}
	if vm.CallStack == nil {
		vm.CallStack = vm.Arena.frameStack()
	}
	if cap(vm.rootSnapshots) < len(vm.CallStack) {
		vm.rootSnapshots = make([]*frameRootSnapshot, len(vm.CallStack))
	} else {
		vm.rootSnapshots = vm.rootSnapshots[:len(vm.CallStack)]
	}
}

// ReleaseArena returns the VM's arena to the global pool and clears the reference. The
// arena is abandoned to the garbage collector instead when the VM's goroutines leaked.
func (vm *VM) ReleaseArena() {
	vm.drainCallbackVMs()
	_ = vm.JoinSpawnedGoroutines()
	if vm.Arena != nil {
		vm.CallStack = nil
		vm.releaseArenaUnlessLeaked(vm.Arena)
		vm.Arena = nil
	}
}

// Execute runs a compiled function and returns its result.
//
// Takes compiledFunction (*CompiledFunction) which is the function to execute.
//
// Returns any which is the result of the function.
// Returns error when execution fails.
func (vm *VM) Execute(compiledFunction *program.CompiledFunction) (result any, err error) {
	if vm.Limits.SafeMode {
		vm.resetPointerProvenance()
	}
	defer vm.FinishWatcher()
	ownArena := false
	defer func() {
		if budgetErr := ClassifyRecoveredPanic(recover()); budgetErr != nil {
			if ownArena {
				vm.releaseOwnedArena()
			}
			result = nil
			err = budgetErr
		}
	}()
	ownArena = vm.prepareForExecution(compiledFunction)

	if err := vm.runVariableInit(compiledFunction, ownArena); err != nil {
		return nil, err
	}

	vm.PushFrame(compiledFunction)
	result, err = vm.RunDispatchedGuarded(0)
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil && err == nil {
		err = joinErr
	}

	if ownArena {
		result = vm.finaliseOwnedArena(result)
	} else if vm.Arena != nil {
		vm.Arena.ownerVM = nil
	}

	return result, err
}

// PushFrame pushes a new call frame for the given function.
//
// Takes compiledFunction (*CompiledFunction) which specifies the compiled function to
// create a frame for.
func (vm *VM) PushFrame(compiledFunction *program.CompiledFunction) {
	vm.guardCallDepth()
	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}
	f := &vm.CallStack[vm.FramePointer]
	if vm.Arena != nil {
		if depth := vm.FramePointer + 1; depth > vm.Arena.framesUsed {
			vm.Arena.framesUsed = depth
		}
		vm.Arena.SaveInto(&f.arenaSave)
		compiledFunction.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&f.Registers, compiledFunction.PrecomputedAllocCounts, compiledFunction.NonZeroBankMask)
	} else {
		f.arenaSave = ArenaSavePoint{}
		f.Registers = NewRegisters(compiledFunction.NumRegisters)
	}
	f.Function = compiledFunction
	f.ProgramCounter = 0
	f.deferBase = len(vm.deferStack)
	if f.simpleDefer != nil {
		f.simpleDefer.active = false
	}
	f.upvalues = nil
	f.returnDestination = nil
	releaseSharedCellMap(f.sharedCells)
	f.sharedCells = nil
}

// BindFileSet points the VM at a compiled file set's function table and root.
//
// Takes functions ([]*CompiledFunction) which is the file set's function table.
// Takes rootFunction (*CompiledFunction) which owns the dispatch context.
func (vm *VM) BindFileSet(functions []*program.CompiledFunction, rootFunction *program.CompiledFunction) {
	vm.forgetClosureCacheForRoot(rootFunction)
	vm.functions = functions
	vm.rootFunction = rootFunction
}

// MaterialiseGlobalStrings copies arena-backed global strings onto the Go heap.
//
// Called once variable initialisation finishes, before the arena that backs those strings
// is released.
func (vm *VM) MaterialiseGlobalStrings() {
	materialiseGlobalStrings(vm.Globals, vm.Arena)
}

// PrepareForFileSet wires the VM's arena-backed frame stack and ASM call-info tables for
// a file set, and returns the acquired arena.
//
// Takes cfs (*CompiledFileSet) whose root sizes the arena and supplies the function
// table.
// Takes entrypointFunction (*CompiledFunction) whose call-info table seeds base slot
// zero.
//
// Returns *RegisterArena which the caller must pass to ReleaseFileSetArena.
func (vm *VM) PrepareForFileSet(cfs *program.CompiledFileSet, entrypointFunction *program.CompiledFunction) *RegisterArena {
	vm.BindFileSet(program.ExportFunctions(cfs.Root()), cfs.Root())
	vm.reopenCallbackVMs()
	arena := vm.acquireArena()
	vm.Arena = arena

	vm.sizeArenaFromFunctions(cfs.Root())
	vm.CallStack = arena.frameStack()
	vm.AsmCallInfoTables = EnsureASMCallInfoTables(cfs.Root())
	vm.buildCallInfoBasesByFunction(cfs.Root())
	vm.asmCallInfoBases = arena.callInfoBases()
	vm.asmDispatchSaves = arena.dispatchSaves()
	if table := vm.AsmCallInfoTables[entrypointFunction]; len(table) > 0 {
		vm.asmCallInfoBases[0] = uintptr(unsafe.Pointer(&table[0]))
	}
	return arena
}

// ReleaseFileSetArena drops everything PrepareForFileSet installed and releases the
// arena.
//
// Takes arena (*RegisterArena) which PrepareForFileSet returned.
func (vm *VM) ReleaseFileSetArena(arena *RegisterArena) {
	vm.drainCallbackVMs()
	vm.CallStack = nil
	vm.AsmCallInfoTables = nil
	vm.dropRuntimeCallInfoFills()
	vm.asmCallInfoBases = nil
	vm.asmDispatchSaves = nil
	vm.releaseArenaUnlessLeaked(arena)
}

// SetExecutionCancel records the cancel function that tears down the VM's execution
// context.
//
// Takes cancel (context.CancelCauseFunc) which cancels the VM and every goroutine it
// spawned.
func (vm *VM) SetExecutionCancel(cancel context.CancelCauseFunc) {
	vm.executionCancel = cancel
	if tracker := vm.Limits.Tracker; tracker != nil {
		tracker.executionCancel.Store(&cancel)
	}
}

// SetStderrWriter redirects the VM's stderr stream.
//
// Takes writer (io.Writer) which receives interpreted stderr output; nil restores the
// default.
func (vm *VM) SetStderrWriter(writer io.Writer) { vm.StderrWriter = writer }

// GoroutinePanic returns the panic recorded by a spawned goroutine, if any.
//
// Returns *GoroutinePanicInfo describing the panic, or nil when none was recorded.
func (vm *VM) GoroutinePanic() *GoroutinePanicInfo { return vm.Globals.goroutinePanic.Load() }

// FinishWatcher deregisters the context.AfterFunc callback that NewVM installs. Safe to
// call more than once and on a VM with no callback.
func (vm *VM) FinishWatcher() {
	if vm.stopWatcher != nil {
		vm.stopWatcher()
	}
}

// acquireArena returns a RegisterArena, using the custom factory if configured or the
// global pool otherwise.
//
// Returns a RegisterArena from the custom factory or the global pool.
func (vm *VM) acquireArena() *RegisterArena {
	var arena *RegisterArena
	if vm.Limits.ArenaFactory != nil {
		arena = vm.Limits.ArenaFactory()
	} else {
		arena = GetRegisterArena()
	}
	arena.MaxArenaBytes = vm.Limits.MaxArenaBytes
	arena.MaxAllocSize = vm.Limits.MaxAllocSize
	arena.disableMinorGC = vm.Limits.DisableMinorGC
	arena.AttachBudgetTracker(vm.Limits.Tracker)
	return arena
}

// releaseArenaUnlessLeaked returns arena to the pool, or abandons it to the garbage
// collector when the execution's goroutines could not be joined.
//
// Takes arena (*RegisterArena) which is the arena to release.
func (vm *VM) releaseArenaUnlessLeaked(arena *RegisterArena) {
	if arena != nil && vm.rootFunction != nil {
		vm.rootFunction.RecordObservedCallDepth(arena.frameStackPeak())
	}
	if vm.goroutinesLeaked {
		arena.detachBudgetTracker()
		return
	}
	if vm.reentrantInterpreterVM {
		putRegisterArenaAbandoningEscapes(arena)
		return
	}
	PutRegisterArena(arena)
}

// sizeArenaFromFunctions pre-sizes the arena for an execution that starts at root, so
// that AllocRegisters never triggers a grow during normal execution. See
// SizeArenaForEntry for the sizing model.
//
// Takes root (*CompiledFunction) which specifies the top-level compiled function whose
// function table is used to compute the arena capacity.
func (vm *VM) sizeArenaFromFunctions(root *program.CompiledFunction) {
	vm.sizeArenaForEntry(root, root)
}

// updateASMCallInfoBase sets the asmCallInfoBases entry for the current frame after a
// Go-side frame change (push/pop).
//
// Grows the parallel arrays if the CallStack has grown. Functions whose call-info table
// was cloned for runtime closure fills use the clone rather than the shared base.
func (vm *VM) updateASMCallInfoBase() {
	if vm.asmCallInfoBases == nil {
		return
	}
	if vm.FramePointer >= len(vm.asmCallInfoBases) {
		vm.growCallStack()
	}
	if vm.FramePointer >= 0 {
		function := vm.CallStack[vm.FramePointer].Function
		base := function.ASMCallInfoBase
		if vm.PrivateACITables != nil && vm.PrivateACITables[function] {
			if table := vm.AsmCallInfoTables[function]; len(table) > 0 {
				base = uintptr(unsafe.Pointer(&table[0]))
			}
		}
		vm.asmCallInfoBases[vm.FramePointer] = base
	}
}

// stderr returns the writer for print/println output.
//
// Returns the configured StderrWriter, or os.Stderr if none was set.
func (vm *VM) stderr() io.Writer {
	if vm.StderrWriter != nil {
		return vm.StderrWriter
	}
	return os.Stderr
}

// callDepthLimit returns the effective maximum call stack depth.
//
// Returns the configured maxCallDepth limit, or the default maxCallDepth constant if
// unset.
func (vm *VM) callDepthLimit() int {
	if vm.Limits.MaxCallDepth > 0 {
		return vm.Limits.MaxCallDepth
	}
	return maxCallDepth
}

// ensureGoroutineLimit returns the effective concurrent-goroutine cap and guarantees a
// resource tracker exists so the cap is enforceable.
//
// A non-positive MaxGoroutines selects the policy.DefaultMaxGoroutines ceiling, and a
// missing tracker is lazily installed so parent and child VMs share the same atomic
// counter.
//
// Returns the effective maximum number of concurrent goroutines.
func (vm *VM) ensureGoroutineLimit() int32 {
	if vm.Limits.Tracker == nil {
		vm.Limits.Tracker = &ResourceTracker{}
	}
	if vm.Limits.MaxGoroutines > 0 {
		return vm.Limits.MaxGoroutines
	}
	return policy.DefaultMaxGoroutines
}

// stackOverflowError builds the error returned when call depth is exceeded.
//
// Returns the wrapped stack-overflow error.
func (vm *VM) stackOverflowError() error {
	return fmt.Errorf("%w: call depth %d exceeded limit %d (raise via WithMaxCallDepth)",
		fault.ErrStackOverflow, vm.FramePointer, vm.callDepthLimit())
}

// guardCallDepth raises a stack-overflow panic when the call depth limit is reached.
func (vm *VM) guardCallDepth() {
	if vm.FramePointer >= vm.callDepthLimit() {
		panic(vm.stackOverflowError())
	}
}

// limitedStderr returns a writer that counts bytes written when an output size limit is
// configured.
//
// Returns a countingWriter wrapping stderr when limits are active, or the plain stderr
// writer otherwise.
func (vm *VM) limitedStderr() io.Writer {
	if vm.Limits.MaxOutputSize > 0 && vm.Limits.Tracker != nil {
		return &countingWriter{writer: vm.stderr(), Tracker: vm.Limits.Tracker}
	}
	return vm.stderr()
}

// checkOutputLimit checks whether the output size limit has been exceeded and sets
// vm.evalError if so.
//
// Returns true when the limit has been exceeded, or false otherwise.
func (vm *VM) checkOutputLimit() bool {
	if vm.Limits.MaxOutputSize > 0 && vm.Limits.Tracker != nil {
		if vm.Limits.Tracker.outputBytes.Load() > int64(vm.Limits.MaxOutputSize) {
			vm.evalError = fmt.Errorf("%w: limit %d bytes", fault.ErrOutputLimit, vm.Limits.MaxOutputSize)
			return true
		}
	}
	return false
}

// initialiseASMDispatch allocates the parallel ASM dispatch arrays (callInfoBases,
// dispatchSaves) from the arena. This must be called after EnsureCallStack and after
// AsmCallInfoTables has been set.
func (vm *VM) initialiseASMDispatch() {
	if vm.Arena == nil {
		return
	}
	vm.asmCallInfoBases = vm.Arena.callInfoBases()
	vm.asmDispatchSaves = vm.Arena.dispatchSaves()
}

// forgetClosureCacheForRoot drops the zero-upvalue closure cache when the VM is about to
// execute a different root: the cache is indexed by function index of the current root,
// so a closure cached for one program would otherwise be handed out for another program's
// function at the same index.
//
// Takes rootFunction (*CompiledFunction) which is the root about to execute.
func (vm *VM) forgetClosureCacheForRoot(rootFunction *program.CompiledFunction) {
	if vm.rootFunction != rootFunction {
		vm.closureCache = nil
	}
}

// prepareForExecution wires the VM's execution-scoped state from compiledFunction and
// acquires an arena and call stack when none has been supplied externally.
//
// Takes compiledFunction (*CompiledFunction) which is the function about to execute.
//
// Returns true when execute owns the arena it just acquired and must release it on
// return.
func (vm *VM) prepareForExecution(compiledFunction *program.CompiledFunction) bool {
	vm.forgetClosureCacheForRoot(compiledFunction)
	vm.functions = compiledFunction.Functions
	vm.rootFunction = compiledFunction
	vm.usesTypedSliceBanks = vm.usesTypedSliceBanks || bundleUsesTypedSliceBanks(compiledFunction)
	vm.SeedExecutionBudgets()
	vm.reopenCallbackVMs()

	ownArena := vm.Arena == nil
	if ownArena {
		vm.Arena = vm.acquireArena()
		vm.sizeArenaFromFunctions(compiledFunction)
	}
	vm.Arena.ownerVM = vm

	if vm.CallStack == nil {
		vm.CallStack = vm.Arena.frameStack()
	}

	vm.AsmCallInfoTables = EnsureASMCallInfoTables(compiledFunction)
	vm.buildCallInfoBasesByFunction(compiledFunction)
	vm.asmCallInfoBases = vm.Arena.callInfoBases()
	vm.asmDispatchSaves = vm.Arena.dispatchSaves()
	if table := vm.AsmCallInfoTables[compiledFunction]; len(table) > 0 {
		vm.asmCallInfoBases[0] = uintptr(unsafe.Pointer(&table[0]))
	}
	return ownArena
}

// runVariableInit executes the function's variable-initialiser body when present,
// releasing the arena on failure.
//
// Takes compiledFunction (*CompiledFunction) whose variableInitFunction is consulted.
// Takes ownArena (bool) which indicates whether execute owns the arena and must release
// it on error.
//
// Returns the wrapped variable-init error or nil when no initialiser is configured.
func (vm *VM) runVariableInit(compiledFunction *program.CompiledFunction, ownArena bool) error {
	if compiledFunction.VariableInitFunction == nil {
		return nil
	}
	vm.PushFrame(compiledFunction.VariableInitFunction)
	if _, err := vm.RunGuarded(0); err != nil {
		if ownArena {
			vm.releaseOwnedArena()
		}
		return fmt.Errorf("varinit: %w", err)
	}
	return nil
}

// finaliseOwnedArena materialises the dispatched result and any pending Eval-all results
// onto the heap before releasing the execute-owned arena.
//
// Takes result (any) which is the value returned by runDispatched.
//
// Returns the heap-materialised result.
func (vm *VM) finaliseOwnedArena(result any) any {
	if !vm.hasGoroutines {
		materialiseGlobalStrings(vm.Globals, vm.Arena)
	}
	result = MaterialiseAnyForArena(vm.Arena, result)
	for index, allResult := range vm.EvalAllResults {
		vm.EvalAllResults[index] = MaterialiseAnyForArena(vm.Arena, allResult)
	}
	vm.releaseOwnedArena()
	return result
}

// releaseOwnedArena joins spawned goroutines, tears down dispatch-context pointers, and
// returns the arena to the pool. Unjoined stragglers cause the arena to be dropped to the
// garbage collector instead of recycled.
func (vm *VM) releaseOwnedArena() {
	vm.drainCallbackVMs()
	_ = vm.JoinSpawnedGoroutines()
	vm.CallStack = nil
	vm.AsmCallInfoTables = nil
	vm.dropRuntimeCallInfoFills()
	vm.asmCallInfoBases = nil
	vm.asmDispatchSaves = nil
	vm.Arena.ownerVM = nil
	vm.releaseArenaUnlessLeaked(vm.Arena)
	vm.Arena = nil
}

// popFrame pops the current call frame and restores the previous one. If the arena is in
// use, restores it to the save point recorded when the frame was pushed, reclaiming the
// frame's register slots.
func (vm *VM) popFrame() {
	frame := &vm.CallStack[vm.FramePointer]
	if vm.FramePointer < len(vm.rootSnapshots) {
		if snapshot := vm.rootSnapshots[vm.FramePointer]; snapshot != nil {
			vm.restoreRootSnapshot(snapshot)
			vm.rootSnapshots[vm.FramePointer] = nil
		}
	}
	frame.rootSwapped = false
	if vm.Arena != nil {
		vm.Arena.Restore(frame.arenaSave)
	}

	releaseSharedCellMap(frame.sharedCells)
	frame.sharedCells = nil
	vm.FramePointer--
}

// currentFrame returns a pointer to the current call frame.
//
// Returns the CallFrame at the current frame pointer position.
func (vm *VM) currentFrame() *CallFrame {
	return &vm.CallStack[vm.FramePointer]
}

// extractResult extracts the return value from the final frame.
//
// Takes frame (*CallFrame) which specifies the call frame to extract the result from.
//
// Returns the result value from the first result register, or nil if no results are
// declared.
func (vm *VM) extractResult(frame *CallFrame) (any, error) {
	if len(frame.Function.ResultKinds) == 0 {
		return nil, nil
	}
	kind := frame.Function.ResultKinds[0]
	if hint := resultReflectTypeHint(frame.Function, 0); hint != nil {
		if value, ok := extractRegisterValueAs(vm.Arena, &frame.Registers, kind, 0, hint); ok {
			return value, nil
		}
	}
	return vm.extractRegisterValue(&frame.Registers, kind, 0), nil
}

// extractRegisterValue reads the boxed value held in the given register bank at the
// supplied index, returning nil when the index is past the bank's populated length or the
// general-bank slot holds an invalid reflect.Value. String banks are materialised against
// the VM arena so the returned value outlives frame teardown.
//
// Takes registers (*Registers) which is the frame's typed register file.
// Takes kind (isa.RegisterKind) which selects the bank to read from.
// Takes index (int) which is the register slot within that bank.
//
// Returns the boxed register value, or nil when no value is present.
func (vm *VM) extractRegisterValue(registers *Registers, kind isa.RegisterKind, index int) any {
	switch kind {
	case isa.RegisterString:
		return extractStringRegisterValue(vm.Arena, registers, index)
	case isa.RegisterGeneral:
		return extractGeneralRegisterValue(registers, index)
	default:
		return extractScalarRegisterValue(registers, kind, index)
	}
}

// extractAllResults extracts all return values from the final frame.
//
// Takes frame (*CallFrame) which specifies the call frame to extract results from.
//
// Returns a slice of all result values.
func (vm *VM) extractAllResults(frame *CallFrame) []any {
	resultKinds := frame.Function.ResultKinds
	if len(resultKinds) == 0 {
		return nil
	}

	results := make([]any, len(resultKinds))
	var bankCounters [isa.NumRegisterKinds]uint8
	registers := &frame.Registers

	for i, kind := range resultKinds {
		sourceRegister := int(bankCounters[kind])
		bankCounters[kind]++
		if hint := resultReflectTypeHint(frame.Function, i); hint != nil {
			if value, ok := extractRegisterValueAs(vm.Arena, registers, kind, sourceRegister, hint); ok {
				results[i] = value
				continue
			}
		}
		results[i] = vm.extractRegisterValue(registers, kind, sourceRegister)
	}

	return results
}

// callClosureReflect executes a compiled closure via reflect, pushing a new frame,
// assigning arguments, and running the function to completion.
//
// Takes closure (*RuntimeClosure) which specifies the closure to call.
// Takes arguments ([]reflect.Value) which provides the reflect.Value arguments to pass.
// Takes functionType (reflect.Type) which defines the expected function signature for
// result packaging.
//
// Returns the result values packaged as reflect.Values matching functionType's output
// signature.
func (vm *VM) callClosureReflect(closure *RuntimeClosure, arguments []reflect.Value, functionType reflect.Type) []reflect.Value {
	callee := closure.Function

	snapshot := vm.swapToClosureRoot(closure.RootFunction)

	vm.guardCallDepth()
	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}
	closureFp := vm.FramePointer
	f := &vm.CallStack[vm.FramePointer]
	if vm.Arena != nil {
		vm.Arena.SaveInto(&f.arenaSave)
		callee.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&f.Registers, callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
	} else {
		f.Registers = NewRegisters(callee.NumRegisters)
	}
	f.Function = callee
	f.ProgramCounter = 0
	f.returnDestination = nil
	f.deferBase = len(vm.deferStack)
	if f.simpleDefer != nil {
		f.simpleDefer.active = false
	}
	f.upvalues = nil
	f.hasGeneralAlloc = callee.NumRegisters[isa.RegisterGeneral] > 0
	releaseSharedCellMap(f.sharedCells)
	f.sharedCells = nil
	vm.recordFrameSnapshot(closureFp, snapshot)
	if closure.upvalues != nil {
		f.initialiseUpvalues(closure.upvalues, vm.Arena)
	}
	vm.updateASMCallInfoBase()

	assignReflectParams(&f.Registers, callee.ParameterKinds, callee.ParameterRegisters, arguments)

	_, err := vm.RunDispatchedGuarded(closureFp)
	if err != nil {
		vm.evalError = err
	}

	allResults := vm.EvalAllResults
	vm.EvalAllResults = nil
	return buildReflectResults(allResults, functionType)
}

// writeRangeValue writes a reflect.Value to the appropriate register.
//
// A range assigns an element, whose kind is one of these seven. A typed-slice bank is the
// collection's kind, never the element's.
//
// Takes registers (*Registers) which specifies the register banks to write into.
// Takes value (reflect.Value) which specifies the reflect.Value to store.
// Takes register (uint8) which specifies the register index within the bank.
// Takes kind (isa.RegisterKind) which specifies which register bank to target.
func (*VM) writeRangeValue(registers *Registers, value reflect.Value, register uint8, kind isa.RegisterKind) {
	if kind != isa.RegisterGeneral {
		value = unwrapInterfaceElement(value)
	}
	switch kind {
	case isa.RegisterInt:
		switch value.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			registers.Ints[register] = safeconv.Uint64ToInt64Reinterpret(value.Uint())
		default:
			registers.Ints[register] = value.Int()
		}
	case isa.RegisterFloat:
		registers.Floats[register] = value.Float()
	case isa.RegisterString:
		registers.Strings[register] = value.String()
	case isa.RegisterGeneral:
		registers.General[register] = ValueCopyForBoundary(value)
	case isa.RegisterBool:
		registers.Bools[register] = value.Bool()
	case isa.RegisterUint:
		registers.Uints[register] = value.Uint()
	case isa.RegisterComplex:
		registers.Complex[register] = value.Complex()
	default:
	}
}

// syncNamedResults syncs upvalue cells back to registers for captured named return
// variables, then re-copies from named result registers to return positions for Go spec
// compliance.
//
// Takes frame (*CallFrame) which specifies the call frame whose named results are synced.
func (*VM) syncNamedResults(frame *CallFrame) {
	namedLocations := frame.Function.NamedResultLocations
	if len(namedLocations) == 0 {
		return
	}

	if frame.sharedCells != nil {
		for _, location := range namedLocations {
			if location.IsIndirect {
				continue
			}
			key := isa.JoinWide(uint8(location.Kind), location.Register)
			if cell, ok := frame.sharedCells.get(key); ok {
				syncCellToRegister(&frame.Registers, cell, location)
			}
		}
	}

	var bankCounters [isa.NumRegisterKinds]uint8
	for _, location := range namedLocations {
		if location.IsIndirect {
			syncIndirectNamedResult(&frame.Registers, location, &bankCounters)
			continue
		}
		destinationRegister := bankCounters[location.Kind]
		bankCounters[location.Kind]++
		if destinationRegister != location.Register {
			copyRegisterSlot(&frame.Registers, location.Kind, destinationRegister, location.Register)
		}
	}
}

// acquireSliceSnapshot bump-allocates the next slice-header buffer.
//
// When an arena is attached, the slot is drawn from arena.SliceHeaderSlab and cleared on
// arena.Reset. When no arena is attached, the fallback uses the per-VM chunked slab.
//
// Returns a pointer to the freshly bump-allocated slot.
//
//go:nosplit
func (vm *VM) acquireSliceSnapshot() *snapshotSliceHeader {
	if vm.Arena != nil {
		slot := vm.Arena.allocSliceHeader()
		return (*snapshotSliceHeader)(slot)
	}
	if vm.sliceSnapshotNext >= len(vm.sliceSnapshotChunk) {
		vm.sliceSnapshotChunk = make([]snapshotSliceHeader, snapshotChunkSize)
		vm.sliceSnapshotNext = 0
	}
	slot := &vm.sliceSnapshotChunk[vm.sliceSnapshotNext]
	vm.sliceSnapshotNext++
	return slot
}

// growCallStack doubles the call stack capacity, growing the arena slabs or independent
// arrays to keep the parallel arrays (frames, CallInfoBases, asmDispatchSaves) in sync.
//
// When an arena is available, the arena's slabs are grown so all three parallel arrays
// stay in sync. Without an arena, the CallStack and parallel arrays are grown
// independently.
//
//go:noinline
func (vm *VM) growCallStack() {
	newCap := len(vm.CallStack) * 2
	if vm.rootFunction != nil {
		if observed := vm.rootFunction.ObservedCallDepth(); observed > 0 {
			newCap = max(newCap, vm.observedFrameStackTarget(observed))
		}
	}
	if vm.Arena != nil {
		frames, ci, disp := vm.Arena.growFrameStack(newCap)
		vm.CallStack = frames
		vm.asmCallInfoBases = ci
		vm.asmDispatchSaves = disp
	} else {
		newStack := make([]CallFrame, newCap)
		copy(newStack, vm.CallStack)
		vm.CallStack = newStack
		if vm.asmCallInfoBases != nil {
			newCI := make([]uintptr, newCap)
			copy(newCI, vm.asmCallInfoBases)
			vm.asmCallInfoBases = newCI
			newDisp := make([]asmDispatchSave, newCap)
			copy(newDisp, vm.asmDispatchSaves)
			vm.asmDispatchSaves = newDisp
		}
	}
	if cap(vm.rootSnapshots) < len(vm.CallStack) {
		grown := make([]*frameRootSnapshot, len(vm.CallStack))
		copy(grown, vm.rootSnapshots)
		vm.rootSnapshots = grown
	} else {
		vm.rootSnapshots = vm.rootSnapshots[:len(vm.CallStack)]
	}
}

// copyReturnValueAt copies a return value from the callee frame at the given source
// register to the caller frame at the destination.
//
// Takes calleeFrame (*CallFrame) which specifies the frame containing the source value.
// Takes kind (isa.RegisterKind) which specifies the register bank of the source value.
// Takes sourceRegister (uint8) which specifies the source register index within the
// callee.
// Takes dest (VarLocation) which specifies the destination location in the caller frame.
func (vm *VM) copyReturnValueAt(calleeFrame *CallFrame, kind isa.RegisterKind, sourceRegister uint8, dest program.VarLocation) {
	callerFrame := &vm.CallStack[vm.FramePointer-1]
	if kind == dest.Kind {
		copySameKind(&callerFrame.Registers, &calleeFrame.Registers, kind, dest.Register, sourceRegister)
	} else if kind == isa.RegisterGeneral && dest.Kind != isa.RegisterGeneral {
		copyReturnFromGeneral(callerFrame, calleeFrame.Registers.General[sourceRegister], dest)
	} else if dest.Kind == isa.RegisterGeneral && kind != isa.RegisterGeneral {
		copyReturnToGeneral(callerFrame, &calleeFrame.Registers, kind, sourceRegister, dest.Register)
	}
}

// setExecutionContext replaces the context the VM checks for cancellation.
//
// Exposed for tests that need to cancel a VM mid-run.
func (vm *VM) setExecutionContext(ctx context.Context) { vm.ctx = ctx }

// executionContext returns the context the VM checks for cancellation.
//
// Returns context.Context which is the VM's cancellation source.
func (vm *VM) executionContext() context.Context { return vm.ctx }

// RuntimeClosure binds a compiled function to its captured upvalue cells.
type RuntimeClosure struct {
	// inlineCells stores the first inlineUpvalueCells captures inline to avoid a separate
	// make([]*UpvalueCell, N) per closure.
	inlineCells [inlineUpvalueCells]*program.UpvalueCell

	// Function is the compiled body this closure invokes.
	Function *program.CompiledFunction

	// RootFunction is the top-level function whose dispatch context the closure belongs to;
	// restored when the closure is reflectively invoked from outside its defining program.
	RootFunction *program.CompiledFunction

	// upvalues references the captured cells; aliases inlineCells when the count fits,
	// otherwise points at an external slice.
	upvalues []*program.UpvalueCell

	// namedType is the bare name of the named func type this closure was converted to, or ""
	// for a plain func value. Method dispatch and %T key on it.
	namedType string
}

// NewRuntimeClosure builds a closure value over a compiled function.
//
// Takes function (*CompiledFunction) which is the body to invoke.
// Takes rootFunction (*CompiledFunction) which owns the dispatch context.
//
// Returns *RuntimeClosure ready to be handed to reflect.ValueOf.
func NewRuntimeClosure(function, rootFunction *program.CompiledFunction) *RuntimeClosure {
	return &RuntimeClosure{Function: function, RootFunction: rootFunction, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: ""}
}

// Format prints a closure the way Go prints a func value: as its address. %T never
// reaches here; the fmt intercept rewrites it to the closure's static type first.
//
// Takes state (fmt.State) which receives the output.
// Takes verb (rune) which is the format verb.
func (closure *RuntimeClosure) Format(state fmt.State, verb rune) {
	switch verb {
	case 'v', 'p', 's':
		fmt.Fprintf(state, "%p", closure)
	default:
		fmt.Fprintf(state, "%%!%c(func=%p)", verb, closure)
	}
}

// deferState is the defer and panic-unwind state of one VM: the deferred-call stack, the
// frame allowed to recover, and the value and flag of the panic being unwound.
type deferState struct {
	// panicValue holds the current panic value during unwinding.
	panicValue any

	// callbackPanicStack holds the rendered frames of the callback VM the current panic came
	// from, "" when the panic started in this VM. Rendered ahead of this VM's frames in the
	// uncaught-panic report and cleared when the panic is recovered.
	callbackPanicStack string

	// deferStack holds deferred function calls. Each frame records its deferBase so that
	// only defers from the current frame are run when the frame returns.
	deferStack []deferredCall

	// recoverEligibleFrame records the FramePointer of the running deferred function, where
	// recover succeeds only when FramePointer matches and -1 means no defer is active.
	recoverEligibleFrame int

	// panicking is true when the VM is unwinding due to a panic.
	panicking bool
}

// goroutineState is the spawned-goroutine and interpreter-lock state of one VM: the join
// outcome, the safe-mode GIL handoff, the execution-scoped cancellation and the flags
// that record whether goroutines were spawned, joined or leaked.
type goroutineState struct {
	// goroutineJoinError records the outcome of joinSpawnedGoroutines so repeated calls
	// return the same result without waiting again.
	goroutineJoinError error

	// pendingGilHandoff is the safe-mode GIL handoff published for the currently marshalling
	// reflect native call, or nil.
	//
	// pendingGilHandoff lets closure wrappers created during argument marshalling re-acquire
	// the interpreter lock for their callback body. Always nil in fast mode.
	pendingGilHandoff *gilHandoff

	// executionCancel cancels the execution-scoped context shared with spawned goroutines.
	// Set only on the VM running a top-level execution; nil on child and nested VMs.
	executionCancel context.CancelCauseFunc

	// debugThread is this VM's registration with Limits.Debug while it is inside a dispatch
	// loop; nil otherwise and always nil without a debugger.
	debugThread *DebugThread

	// debugParkDone is closed when the execution this VM belongs to is cancelled, so a VM
	// parked in the debugger wakes. For a callback VM it is the wrapping VM's cancellable
	// context, not the callback's own uncancellable one.
	debugParkDone <-chan struct{}

	// stopWatcher deregisters the context.AfterFunc callback that sets the cancelled flag.
	stopWatcher func() bool

	// panicDeferFrames lists the deferred calls currently running on behalf of a panic,
	// innermost last, each with the frames the panic had already unwound when the call
	// started; the caller-inspection intrinsics show Go's runtime.gopanic frame and those
	// frames beneath each.
	panicDeferFrames []panicDeferRecord

	// panicUnwound lists the frames the current panic has popped, innermost first, cleared
	// when a recover completes.
	panicUnwound []unwoundFrame

	// panicFrames is the stack when the current panic started, innermost first, so an
	// uncaught panic can be reported the way Go reports one.
	panicFrames []stackFrame

	// goroutineID is the number stack dumps report for this VM's goroutine: 1 for the root
	// VM, a fresh number per go statement, and the parent's for a VM that runs a callback on
	// the parent's behalf.
	goroutineID uint64

	// goroutinesJoined is set once joinSpawnedGoroutines has run for this execution.
	goroutinesJoined bool

	// goroutinesLeaked is set when the join grace period elapsed with goroutines still
	// running; the execution's arena must then be dropped rather than recycled.
	goroutinesLeaked bool

	// asmCallInfoBasesPrivate is set once publishCallInfoBaseFor() has copied
	// asmCallInfoBasesByFunction out of the root's shared array, so later publishes write
	// the private copy in place. Cleared with the array by dropRuntimeCallInfoFills().
	asmCallInfoBasesPrivate bool

	// hasGoroutines is set when the first opGo executes or at construction of a reentrant
	// VM. When true, store barriers and closure caching switch to concurrent-safe paths.
	hasGoroutines bool

	// holdsInterpreterLock is true while this VM holds its family's interpreter lock in safe
	// mode. Always false in fast mode.
	holdsInterpreterLock bool

	// reentrantInterpreterVM marks a VM created to run a closure or method invoked by native
	// code. Such a VM must never acquire the interpreter lock.
	reentrantInterpreterVM bool

	// debugRunDepth counts nested run() loops so the VM registers with the debugger once.
	debugRunDepth int

	// debugWokenByCancel is set when a VM parked in the debugger was woken by cancellation
	// rather than by a resume, so the dispatch loop reports the cancellation.
	debugWokenByCancel bool

	// debugKind and debugRole classify this VM for the debugger: how it came to run and
	// which execution phase it serves.
	debugKind DebugThreadKind

	// debugRole is the execution phase this VM serves, as shown by the debugger.
	debugRole DebugRole
}

// unwoundFrame is a frame a panic popped, kept for stack inspection.
type unwoundFrame struct {
	// function is the frame's compiled function.
	function *program.CompiledFunction

	// pc is the index of the instruction that was executing.
	pc int
}

// panicDeferRecord describes a deferred call running on behalf of a panic.
type panicDeferRecord struct {
	// unwound holds the frames the panic had popped before this call started.
	unwound []unwoundFrame

	// frameIndex is the deferred call's position in the call stack.
	frameIndex int
}

// nativeState holds the caches that calls across the Go boundary use: pointer-type and
// map-access one-slot caches, pointer provenance, the method and typed-handle caches, the
// erasure-boundary pointee set, and the reusable buffers for snapshots and builtin
// arguments.
type nativeState struct {
	// ptrTypeCacheKey is the cached element type T for the one-slot LRU that bypasses
	// reflect.PointerTo's sync.Map lookup for the hot ADDR-chain pattern (`&Struct{}` in
	// tight constructors). A constructor that repeatedly allocates the same struct type hits
	// the cache on every allocation after the first.
	ptrTypeCacheKey reflect.Type

	// ptrTypeCacheValue holds *T paired with ptrTypeCacheKey.
	ptrTypeCacheValue reflect.Type

	// mapAccessCacheLastType is a one-slot LRU for map reflect.Type, paired with
	// mapAccessCacheLastEntry to avoid repeated Key/Elem lookups.
	mapAccessCacheLastType reflect.Type

	// boundarySnapshotChunks caches per-type chunked slabs for pointer-containing boundary
	// copies, amortising reflect.New across multiple snapshots per type.
	boundarySnapshotChunks map[unsafe.Pointer]*boundaryChunk

	// pointerProvenance records the valid byte window per unsafe pointer for safe-mode
	// bounds checking. Nil in fast mode.
	pointerProvenance map[uintptr]pointerBound

	// methodCache maps (reflect.Type, method name) to the method index so that
	// handleGetMethod can use reflect.Value.Method(index) on repeat calls instead of the
	// allocating MethodByName lookup.
	methodCache map[methodCacheKey]int

	// typedHandleCache maps a chan/map pointer to its Go-typed handle, bypassing
	// reflect.Value dispatch on repeat accesses.
	typedHandleCache map[uintptr]any

	// mapKeyScratch caches an addressable reflect.Value per key type so typed map fast paths
	// reuse the scratch rather than allocating a fresh wrapper per call.
	mapKeyScratch map[reflect.Type]reflect.Value

	// nativeBackedErasurePointees holds valid erasure-boundary pointees, lazily built from
	// registered NativeBackedGenericType sentinels. Nil until first use.
	nativeBackedErasurePointees map[reflect.Type]struct{}

	// mapAccessCacheLastEntry holds the key/elem types and fast-path eligibility for
	// mapAccessCacheLastType. Read after a successful type-pointer equality check against
	// mapAccessCacheLastType; a miss recomputes and refreshes both fields.
	mapAccessCacheLastEntry mapAccessCacheEntry

	// pointerProvenanceKeepAlive pins the pointees recorded in pointerProvenance so their
	// addresses remain valid for the whole execution (the map holds only uintptrs, invisible
	// to the GC). Safe mode only.
	pointerProvenanceKeepAlive []reflect.Value

	// closureCache caches reflect.Value wrappers for zero-upvalue closures, indexed by
	// functionIndex.
	closureCache []reflect.Value

	// sliceSnapshotChunk is a chunked slab for detaching slice-field reads from their
	// backing struct, amortising mallocgc across snapshotChunkSize allocations.
	sliceSnapshotChunk []snapshotSliceHeader

	// builtinArgumentsBuffer is a reusable buffer for handleCallBuiltin arguments. Safe
	// because VM is per-goroutine and builtins are synchronous.
	builtinArgumentsBuffer []any

	// sliceSnapshotNext is the bump index into the active sliceSnapshotChunk slab.
	sliceSnapshotNext int
}

// arenaBytecodeHints counts byteSlab-writing and MakeSlice opcodes to drive arena
// pre-sizing at warmup.
type arenaBytecodeHints struct {
	// Bytes is the cumulative byte budget for byteSlab writers.
	Bytes int

	// MakeSliceInt counts subOpMakeSliceInt occurrences.
	MakeSliceInt int

	// MakeSliceFloat counts subOpMakeSliceFloat occurrences.
	MakeSliceFloat int

	// MakeSliceString counts subOpMakeSliceString occurrences.
	MakeSliceString int

	// MakeSliceBool counts subOpMakeSliceBool occurrences.
	MakeSliceBool int

	// MakeSliceUint counts subOpMakeSliceUint occurrences.
	MakeSliceUint int

	// genericBytes is the byte budget for the composite snapshot slab.
	genericBytes int

	// sliceHeaders is the cumulative slot budget for arena-routed reflect.Value slice
	// headers. Each opcode that wraps a typed slice into the general bank
	// (opSubOpBoxSliceByte's arena form, pack-typed-slice paths, append fast-paths, slice
	// snapshots) consumes one slot.
	sliceHeaders int

	// boxInts counts opPackInterface/opPackTyped sites whose source register kind feeds the
	// int box slab. Multiplied by boxSlabPerOccurrenceFactor inside sizeArenaFromFunctions.
	boxInts int

	// boxUints counts pack sites feeding the uint box slab.
	boxUints int

	// boxFloats counts pack sites feeding the float box slab.
	boxFloats int

	// boxStrings counts pack sites feeding the string box slab.
	boxStrings int

	// boxComplexes counts pack sites feeding the complex box slab.
	boxComplexes int

	// upvalueCells is the cumulative descriptor count summed across every opMakeClosure
	// occurrence, looked up via the instruction's function-index operand and the callee's
	// upvalueDescriptors.
	upvalueCells int

	// upvalueReferences mirrors upvalueCells under the current ABI; kept separate so future
	// shape changes (e.g. cell vs reference fanout) do not require a hint-struct churn.
	upvalueReferences int

	// appendOccurrences counts opAppend / opAppendSpread / opAppendInPlace /
	// opAppendSpreadInPlace instructions.
	appendOccurrences int

	// makeSliceGeneric counts opMakeSlice instructions targeting the general bank (one
	// slice-header slot per occurrence).
	makeSliceGeneric int

	// makeMapOccurrences counts isa.SubOpTier2MakeMap instructions.
	makeMapOccurrences int

	// allocIndirectOccurrences counts opAllocIndirect instructions (each charges one
	// slice-header slot plus an estimated bytes budget on the generic byte slab).
	allocIndirectOccurrences int
}

// rangeIterator holds the state for a for-range loop over a slice, array, map, or
// channel.
type rangeIterator struct {
	// mapIterator drives map ranges; nil for non-map collections.
	mapIterator *reflect.MapIter

	// collection is the reflect-typed source for general-kind ranges.
	collection reflect.Value

	// valueScratch is the reusable value destination for map ranges.
	valueScratch reflect.Value

	// keyScratch is the reusable key destination for map ranges.
	keyScratch reflect.Value

	// StringSource is the source string when ranging over a string; indexed by index to
	// yield (rune, byte-offset) pairs.
	StringSource string

	// boolSlice is the source when ranging over a []bool fast path.
	boolSlice []bool

	// floatSlice is the source when ranging over a []float64 fast path.
	floatSlice []float64

	// stringSlice is the source when ranging over a []string fast path.
	stringSlice []string

	// intSlice is the source when ranging over a []int fast path.
	intSlice []int

	// index is the next slot to read; bumped on each Next.
	index int

	// isMap reports whether the iterator drives mapIterator.
	isMap bool

	// isChannel reports whether the iterator drives a channel recv.
	isChannel bool

	// IsString reports whether the iterator drives a string range.
	IsString bool
}

// ClassifyRecoveredPanic inspects a value obtained from a directly-deferred recover()
// call.
//
// Surfaces arena exhaustion as a normal evaluation error; any other non-nil panic is
// re-raised to preserve host-visible behaviour, and a nil recovered value yields nil.
// Call it only from a function that was itself deferred, so the recover result is
// evaluated in that frame.
//
// Takes recovered (any) which is the value returned by recover().
//
// Returns error when the recovered panic is the arena-budget error, nil when nothing was
// recovered.
//
// Panics when recovered is non-nil and is not the arena-budget error.
func ClassifyRecoveredPanic(recovered any) error {
	if recovered == nil {
		return nil
	}
	if budgetErr, ok := recovered.(error); ok && errors.Is(budgetErr, fault.ErrArenaBudgetExceeded) {
		return budgetErr
	}
	panic(recovered)
}

// RecoverArenaBudgetError converts a recovered arena-budget panic into the caller's
// returned error, leaving err untouched when nothing was recovered.
//
// Takes err (*error) which receives the arena-budget error on exhaustion.
func RecoverArenaBudgetError(err *error) {
	if budgetErr := ClassifyRecoveredPanic(recover()); budgetErr != nil {
		*err = budgetErr
	}
}

// bundleUsesTypedSliceBanks reports whether a bundle uses typed banks.
//
// Reads only the immutable nonZeroBankMask fields (set at compile time), so it is safe to
// call concurrently from per-goroutine VMs that share a function table.
//
// Takes root (*CompiledFunction) which heads the bundle to inspect.
//
// Returns bool which is true when root or any sibling function uses a typed-slice or
// complex register bank.
func bundleUsesTypedSliceBanks(root *program.CompiledFunction) bool {
	if root == nil {
		return false
	}
	if root.NonZeroBankMask&typedSliceBankMask != 0 {
		return true
	}
	for _, fn := range root.Functions {
		if fn != nil && fn.NonZeroBankMask&typedSliceBankMask != 0 {
			return true
		}
	}
	return false
}

// saturatingProduct returns a*b clamped at maxArenaPreSizeBacking. Both inputs are
// non-negative bytecode counts; negative values from pathological inputs are clamped to
// zero.
//
// Takes a (int) which is the first multiplicand.
// Takes b (int) which is the second multiplicand.
//
// Returns the saturated product.
func saturatingProduct(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > maxArenaPreSizeBacking/b {
		return maxArenaPreSizeBacking
	}
	return a * b
}

// saturatingProduct3 returns a*b*c clamped at maxArenaPreSizeBacking. Computed in two
// saturating steps so neither intermediate can wrap.
//
// Takes a (int) which is the first multiplicand.
// Takes b (int) which is the second multiplicand.
// Takes c (int) which is the third multiplicand.
//
// Returns the saturated product.
func saturatingProduct3(a, b, c int) int {
	return saturatingProduct(saturatingProduct(a, b), c)
}

// clampAtMost returns the smaller of value and ceiling, normalising negative values to
// zero.
//
// Takes value (int) which is the hint-derived budget.
// Takes ceiling (int) which is the per-slab safety cap.
//
// Returns value when value <= ceiling, otherwise ceiling.
func clampAtMost(value, ceiling int) int {
	if value < 0 {
		return 0
	}
	if value > ceiling {
		return ceiling
	}
	return value
}

// extractStringRegisterValue reads the arena string register at index, materialising it
// against the arena so the value outlives the frame.
//
// Takes arena (*RegisterArena) which backs the string storage.
// Takes registers (*Registers) which is the frame's register file.
// Takes index (int) which is the string-bank slot to read.
//
// Returns the materialised string, or nil when index is out of range.
func extractStringRegisterValue(arena *RegisterArena, registers *Registers, index int) any {
	if index < len(registers.Strings) {
		return materialiseString(arena, registers.Strings[index])
	}
	return nil
}

// extractGeneralRegisterValue reads the general-bank register at index, boxing the held
// reflect.Value.
//
// Takes registers (*Registers) which is the frame's register file.
// Takes index (int) which is the general-bank slot to read.
//
// Returns the boxed value, or nil when index is out of range or the slot holds an invalid
// reflect.Value.
func extractGeneralRegisterValue(registers *Registers, index int) any {
	if index >= len(registers.General) {
		return nil
	}
	if v := registers.General[index]; v.IsValid() {
		return v.Interface()
	}
	return nil
}

// extractScalarRegisterValue reads a register at index from the bank selected by kind,
// returning a boxed Go value.
//
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the slot to read.
//
// Returns the boxed value, or nil when index is out of range or kind is not a supported
// bank.
func extractScalarRegisterValue(registers *Registers, kind isa.RegisterKind, index int) any {
	switch kind {
	case isa.RegisterInt:
		if index < len(registers.Ints) {
			return int(registers.Ints[index])
		}
		return nil
	case isa.RegisterFloat:
		return boxRegisterAt(registers.Floats, index)
	case isa.RegisterBool:
		return boxRegisterAt(registers.Bools, index)
	case isa.RegisterUint:
		return boxRegisterAt(registers.Uints, index)
	case isa.RegisterComplex:
		return boxRegisterAt(registers.Complex, index)
	case isa.RegisterSliceInt:
		return boxRegisterAt(registers.SlicesInt, index)
	case isa.RegisterSliceFloat:
		return boxRegisterAt(registers.slicesFloat, index)
	case isa.RegisterSliceString:
		return boxRegisterAt(registers.slicesString, index)
	case isa.RegisterSliceBool:
		return boxRegisterAt(registers.slicesBool, index)
	case isa.RegisterSliceUint:
		return boxRegisterAt(registers.slicesUint, index)
	case isa.RegisterSliceByte:
		return boxRegisterAt(registers.slicesByte, index)
	default:
		return nil
	}
}

// boxRegisterAt boxes bank[index].
//
// Takes bank ([]T) which is one typed register bank.
// Takes index (int) which is the slot to read.
//
// Returns the boxed value, or nil when index is out of range.
func boxRegisterAt[T any](bank []T, index int) any {
	if index < len(bank) {
		return bank[index]
	}
	return nil
}

// syncIndirectNamedResult dereferences a heap-promoted named result into its slot.
//
// The declaring frame heap-promoted the result so its register holds a *T pointer; this
// writes the pointed-to value into the return slot, recovering the post-defer state of
// named results that were mutated through the heap pointer. This mirrors Go's spec:
// defers can modify named results, and the caller must observe those modifications.
//
// Takes registers (*Registers) which is the frame's register set.
// Takes location (VarLocation) which holds the indirect register and originalKind
// metadata.
// Takes bankCounters (*[NumRegisterKinds]uint8) which tracks the next free return-slot
// register per bank; mutated to reserve the slot this named result claims.
func syncIndirectNamedResult(registers *Registers, location program.VarLocation, bankCounters *[isa.NumRegisterKinds]uint8) {
	kind := location.OriginalKind
	destinationRegister := bankCounters[kind]
	bankCounters[kind]++
	pointer := registers.General[location.Register]
	if !pointer.IsValid() {
		return
	}
	target := pointer.Elem()
	if !target.IsValid() {
		return
	}
	if unpackInterfaceSlice(registers, kind, destinationRegister, target) {
		return
	}
	switch kind {
	case isa.RegisterInt:
		registers.Ints[destinationRegister] = target.Int()
	case isa.RegisterFloat:
		registers.Floats[destinationRegister] = target.Float()
	case isa.RegisterString:
		registers.Strings[destinationRegister] = target.String()
	case isa.RegisterGeneral:
		registers.General[destinationRegister] = target
	case isa.RegisterBool:
		registers.Bools[destinationRegister] = target.Bool()
	case isa.RegisterUint:
		registers.Uints[destinationRegister] = target.Uint()
	case isa.RegisterComplex:
		registers.Complex[destinationRegister] = target.Complex()
	default:
	}
}

// basicReflectTypeForKind returns the predeclared type of a scalar kind.
//
// Takes kind (reflect.Kind) which must be a scalar kind.
//
// Returns reflect.Type which is the predeclared type (int for Int, string for String...).
func basicReflectTypeForKind(kind reflect.Kind) reflect.Type {
	switch kind {
	case reflect.Bool:
		return reflect.TypeFor[bool]()
	case reflect.Int:
		return reflect.TypeFor[int]()
	case reflect.Int8:
		return reflect.TypeFor[int8]()
	case reflect.Int16:
		return reflect.TypeFor[int16]()
	case reflect.Int32:
		return reflect.TypeFor[int32]()
	case reflect.Int64:
		return reflect.TypeFor[int64]()
	case reflect.Uint:
		return reflect.TypeFor[uint]()
	case reflect.Uint8:
		return reflect.TypeFor[uint8]()
	case reflect.Uint16:
		return reflect.TypeFor[uint16]()
	case reflect.Uint32:
		return reflect.TypeFor[uint32]()
	case reflect.Uint64:
		return reflect.TypeFor[uint64]()
	case reflect.Uintptr:
		return reflect.TypeFor[uintptr]()
	case reflect.Float32:
		return reflect.TypeFor[float32]()
	case reflect.Float64:
		return reflect.TypeFor[float64]()
	case reflect.Complex64:
		return reflect.TypeFor[complex64]()
	case reflect.Complex128:
		return reflect.TypeFor[complex128]()
	case reflect.String:
		return reflect.TypeFor[string]()
	default:
		return reflect.TypeFor[any]()
	}
}
