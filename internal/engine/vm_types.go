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
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

const (
	// upvalueAliasLayoutAssert fails compilation if upvalue ever grows beyond its single
	// cell-pointer field, which would break the []*UpvalueCell aliasing in
	// initialiseUpvalues.
	upvalueAliasLayoutAssert = unsafe.Sizeof(upvalue{Value: nil}) - unsafe.Sizeof((*program.UpvalueCell)(nil))
)

var (
	_ [upvalueAliasLayoutAssert]struct{} = [0]struct{}{}
)

// CallFrame holds the runtime state for a single function invocation.
//
// Field order is pinned by the ASM dispatch's CF_* byte-offset defines.
//
//nolint:govet // Field order pinned by ASM offsets.
type CallFrame struct {
	// Function is the compiled function being executed in this frame.
	Function *program.CompiledFunction

	// sharedCells deduplicates UpvalueCell pointers across closures.
	sharedCells *sharedCellMap

	// simpleDefer holds the trivial-defer fast-path slot for this frame.
	simpleDefer *SimpleDeferRecord

	// Registers holds the typed register banks for this frame.
	Registers Registers

	// upvalues holds the captured variables for closures.
	upvalues []upvalue

	// returnDestination records where to place return values in the CALLER's frame on
	// return. Set by opCall.
	returnDestination []program.VarLocation

	// arenaSave records the arena state before this frame's registers were allocated.
	// Restored on popFrame to reclaim arena space, turning the bump allocator into a stack
	// allocator.
	arenaSave ArenaSavePoint

	// ProgramCounter is the program counter (next instruction index to execute).
	ProgramCounter int

	// deferBase is the index into vm.deferStack at which this frame's deferred calls start.
	// When the frame returns, all defers from deferBase onwards are executed in LIFO order.
	deferBase int

	// hasGeneralAlloc records whether this frame allocated general-bank slots from the
	// arena.
	hasGeneralAlloc bool

	// rootSwapped records that entering this frame swapped the VM's root to the callee's
	// bundle, so the snapshot must be restored when the frame pops.
	rootSwapped bool
}

// initialiseUpvalues sets up the frame's upvalue slice from closure cells by aliasing the
// cell array directly. upvalue is a single *UpvalueCell field, so the memory layouts
// match (compile-time asserted above).
//
// Takes cells ([]*UpvalueCell) which provides the upvalue cells captured by the closure.
// Takes _ (*RegisterArena) which is unused but retained for call-site uniformity.
func (f *CallFrame) initialiseUpvalues(cells []*program.UpvalueCell, _ *RegisterArena) {
	if len(cells) == 0 {
		f.upvalues = nil
		return
	}
	f.upvalues = unsafe.Slice((*upvalue)(unsafe.Pointer(&cells[0])), len(cells))
}

// SimpleDeferRecord captures the registration state for a trivial defer.
type SimpleDeferRecord struct { //nolint:govet // ASM offsets fixed
	// target holds the deferred pipit closure when non-nil. The classifier guarantees the
	// closure has no captures beyond already-live registers, so the target survives frame
	// teardown when handed to executeDeferredCall.
	target *RuntimeClosure

	// nativeFunction holds the deferred native callable when target is nil. Zero
	// reflect.Value otherwise.
	nativeFunction reflect.Value

	// arguments holds the eagerly-evaluated arguments. Sized to the classifier-accepted
	// maximum (typically zero for the defer-method-value pattern); reused across re-arming.
	arguments []reflect.Value

	// active reports whether this record holds a live registration.
	active bool

	// consumed is true once the return handler has pushed the deferred call as an ordinary
	// frame. active stays true so the assembly return guard routes the re-executed return
	// through the Go handler, which clears the record instead of running it again.
	consumed bool

	// direct reports whether the arguments were captured bank to bank into the typed slots
	// below instead of into arguments. Only compiled-closure targets whose arguments all fit
	// the direct slots use this path; native targets and typed-slice arguments keep the
	// reflect path.
	direct bool

	// ArgumentCount is the number of positional arguments captured directly.
	ArgumentCount uint8

	// argumentKinds records, per positional argument, the bank the value was captured in.
	argumentKinds [isa.MaxTrivialDeferArgs]isa.RegisterKind

	// argumentSlots records, per positional argument, its slot within that bank.
	argumentSlots [isa.MaxTrivialDeferArgs]uint8

	// directBools backs the bool bank of banks; it sits with the byte-sized fields so the
	// record packs without padding.
	directBools [directDeferSlotsPerKind]bool

	// banks views the direct slots as register banks so the call argument copy helpers
	// capture and place them without going through reflect.Value.
	banks Registers

	// directStrings backs the string bank of the direct capture slots.
	directStrings [directDeferSlotsPerKind]string

	// directGenerals backs the general bank of the direct capture slots.
	directGenerals [directDeferSlotsPerKind]reflect.Value

	// directInts backs the int bank of the direct capture slots.
	directInts [directDeferSlotsPerKind]int64

	// directFloats backs the float bank of the direct capture slots.
	directFloats [directDeferSlotsPerKind]float64

	// directUints backs the uint bank of the direct capture slots.
	directUints [directDeferSlotsPerKind]uint64

	// directComplexes backs the complex bank of the direct capture slots.
	directComplexes [directDeferSlotsPerKind]complex128
}

// VMLimits holds resource constraints enforced during execution.
type VMLimits struct {
	// CapabilityHook is the optional capability hook consulted before every gated native
	// operation; nil is treated as permissive.
	CapabilityHook policy.CapabilityHook

	// Tracker holds shared atomic counters for resource usage across VMs.
	Tracker *ResourceTracker

	// Debug is the debugger session every VM of the execution registers with, or nil when no
	// debugger is attached. Child VMs copy it with the rest of the limits, so goroutine,
	// callback and bound-method VMs are debugged without further wiring.
	Debug *DebugSession

	// ArenaFactory provides a custom RegisterArena constructor for testing.
	ArenaFactory func() *RegisterArena

	// CostTable is the per-opcode cost table for cost metering. Nil when cost metering is
	// disabled.
	CostTable *isa.CostTable

	// Diagnostics is the per-VM counters block for fast-path anomalies. Lazily allocated on
	// first read so VMs that never trigger an anomaly pay no memory cost.
	Diagnostics *FastPathDiagnostics

	// MaxCallDepth is the maximum call stack depth before stack overflow.
	MaxCallDepth int

	// MaxStringSize is the maximum string length in bytes that a concatenation may produce.
	// Zero means unlimited.
	MaxStringSize int

	// MaxOutputSize is the maximum total output bytes allowed for print statements.
	MaxOutputSize int

	// CostBudget is the total computation cost budget. Zero means cost metering is disabled.
	CostBudget int64

	// MaxArenaBytes caps the cumulative bytes the register arena may grow to within a single
	// Execute; zero selects the default.
	MaxArenaBytes uint64

	// CallbackArenaRetireBytes caps the bytes a cached callback VM's arena may accumulate
	// across the host callbacks it serves before the VM is discarded and its escaped slabs
	// left to the collector (see VM.CallbackVMStats); zero selects the default of 4 MiB.
	CallbackArenaRetireBytes uint64

	// MaxAllocSize is the maximum allocation size in bytes for a single object.
	MaxAllocSize int

	// MaxGoroutines is the maximum number of concurrent goroutines allowed.
	MaxGoroutines int32

	// YieldInterval is the number of instructions between runtime.Gosched() calls.
	YieldInterval uint32

	// DeadlockGrace is how long every VM goroutine may stay parked on program channels
	// before the execution is reported as deadlocked (see deadlock.go). Zero disables the
	// check.
	DeadlockGrace time.Duration

	// ForceGoDispatch forces the pure Go dispatch loop even on architectures with ASM
	// threaded dispatch (amd64, arm64). Used for testing dispatch parity.
	ForceGoDispatch bool

	// SafeMode enables the runtime guard mode for untrusted code (the WithSafeMode option),
	// distinct from the "safe" build tag. It implies ForceGoDispatch so the guards run on
	// the pure Go path, leaving the ASM fast path untouched.
	SafeMode bool

	// DisableMinorGC keeps the register arena's minor collection from running. Memory grows
	// until MaxArenaBytes is hit.
	DisableMinorGC bool
}

// ResourceTracker holds shared atomic counters for resource tracking across parent and
// child VMs.
//
//exhaustruct:ignore
type ResourceTracker struct {
	// executionCancel ends the execution with a cause; the deadlock check uses it to report
	// fault.ErrDeadlock. Set through VM.SetExecutionCancel.
	executionCancel atomic.Pointer[context.CancelCauseFunc]

	// spawned counts every goroutine launched by interpreted code that has not yet exited,
	// so the main goroutine can join its children when it returns.
	spawned sync.WaitGroup

	// GoroutineCount tracks the number of active goroutines spawned by the VM.
	GoroutineCount atomic.Int32

	// GoroutinesUnscheduled counts goroutines that have been registered but whose body has
	// not yet started running, so the join can tell a goroutine the Go scheduler has not
	// reached yet from one that is stuck.
	GoroutinesUnscheduled atomic.Int32

	// outputBytes tracks the total bytes written to stderr by print statements.
	outputBytes atomic.Int64

	// CostRemaining is the shared computation cost budget for the current execution. Every
	// VM (the main goroutine and each spawned child) draws chunks of CostChunk from it into
	// a local counter, so a single budget bounds the work of the whole goroutine tree.
	CostRemaining atomic.Int64

	// ArenaBytes is the working-set byte total of every register arena attached to the
	// current execution, so the arena budget bounds the goroutine tree as a whole rather
	// than each arena independently.
	ArenaBytes atomic.Int64

	// LeakedGoroutines counts goroutines that were still running when a top-level execution
	// gave up joining them. A Service with a non-zero count must not be reused.
	LeakedGoroutines atomic.Int64

	// blocked counts VM goroutines parked on an operation only another VM goroutine can
	// complete; the deadlock check compares it with the live goroutine count.
	blocked atomic.Int32

	// blockEpoch advances every time a counted goroutine resumes, so a deadlock grace timer
	// can tell "still stuck" from "stuck again".
	blockEpoch atomic.Uint64
}

// RegisterGoroutine records a newly launched goroutine on both the concurrency counter
// and the join group. Must be paired with ReleaseGoroutine on every exit path.
func (t *ResourceTracker) RegisterGoroutine() {
	t.spawned.Add(1)
	t.GoroutinesUnscheduled.Add(1)
}

// MarkGoroutineStarted records that a registered goroutine's body has begun running, so
// the join no longer treats it as waiting for the scheduler.
func (t *ResourceTracker) MarkGoroutineStarted() {
	t.GoroutinesUnscheduled.Add(-1)
}

// AbandonGoroutine releases a registered goroutine that was rejected before its body was
// ever launched.
func (t *ResourceTracker) AbandonGoroutine() {
	t.MarkGoroutineStarted()
	t.ReleaseGoroutine()
}

// ReleaseGoroutine marks a goroutine as finished: the join group is released before the
// concurrency counter drops so that a zero GoroutineCount always implies the join group
// has been fully released too.
func (t *ResourceTracker) ReleaseGoroutine() {
	t.spawned.Done()
	t.GoroutineCount.Add(-1)
}

// deferredCall records a single deferred function call.
type deferredCall struct {
	// Function is the pipit closure to call, or nil when the target is a native callable.
	Function *RuntimeClosure

	// direct is the frame's trivial-defer record when its arguments were captured bank to
	// bank; nil for records that use arguments.
	direct *SimpleDeferRecord

	// nativeFunction is the reflect.Value of a callable Go func to invoke via reflect.Call
	// when the target is not a pipit closure. Holds reflect.Value{} when function is
	// non-nil.
	nativeFunction reflect.Value

	// arguments holds the eagerly evaluated argument values.
	arguments []reflect.Value

	// frameIndex is the call frame that registered this defer.
	frameIndex int

	// spread is true for `defer f(xs...)`: the last argument is the whole variadic slice, so
	// a native target is invoked with CallSlice.
	spread bool

	// started is true while the call runs as an ordinary frame pushed by the return handler.
	// The entry stays on the defer stack so the assembly return guard keeps routing the
	// parent's return through the Go handler, and every defer walk skips it.
	started bool

	// builtin is the isa.Builtin* identifier when the deferred call is a builtin (close,
	// delete, panic, recover, copy, print, println, clear); zero otherwise.
	builtin uint8
}

// upvalue holds a reference to a captured variable in a closure.
type upvalue struct {
	// Value holds the captured value. For heap-escaped variables, this is shared across all
	// closures that captured the same variable.
	Value *program.UpvalueCell
}

// tailCallArgument snapshots a single argument value before a tail call reclaims the
// current frame's registers. Instances must not live in the register arena.
//
//exhaustruct:ignore
type tailCallArgument struct {
	// GeneralValue holds the snapshotted value when the kind is registerGeneral.
	GeneralValue reflect.Value

	// StringValue holds the snapshotted value when the kind is registerString.
	StringValue string

	// sliceIntValue holds the snapshotted slice header when the kind is registerSliceInt;
	// the header survives arena restore because Go slice headers are value types.
	sliceIntValue []int64

	// sliceFloatValue holds the snapshotted slice header for registerSliceFloat.
	sliceFloatValue []float64

	// sliceStringValue holds the snapshotted slice header for registerSliceString.
	sliceStringValue []string

	// sliceBoolValue holds the snapshotted slice header for registerSliceBool.
	sliceBoolValue []bool

	// sliceUintValue holds the snapshotted slice header for registerSliceUint.
	sliceUintValue []uint64

	// sliceByteValue holds the snapshotted slice header for registerSliceByte.
	sliceByteValue []byte

	// IntValue holds the snapshotted value when the kind is registerInt.
	IntValue int64

	// FloatValue holds the snapshotted value when the kind is registerFloat.
	FloatValue float64

	// UintValue holds the snapshotted value when the kind is registerUint.
	UintValue uint64

	// ComplexValue holds the snapshotted value when the kind is registerComplex.
	ComplexValue complex128

	// BoolValue holds the snapshotted value when the kind is registerBool.
	BoolValue bool

	// Kind identifies which register bank this argument belongs to.
	Kind isa.RegisterKind
}

// countingWriter wraps an io.Writer to count bytes written via the shared
// ResourceTracker.
type countingWriter struct {
	// writer is the underlying writer that receives the output bytes.
	writer io.Writer

	// Tracker is the shared resource tracker that accumulates byte counts.
	Tracker *ResourceTracker
}

// Write writes p to the underlying writer and adds the byte count to the tracker.
//
// Takes p ([]byte) which specifies the bytes to write.
//
// Returns the number of bytes written and any write error.
func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.writer.Write(p)
	c.Tracker.outputBytes.Add(int64(n))
	return n, err
}
