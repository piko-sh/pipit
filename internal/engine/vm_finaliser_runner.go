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
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/logging"
)

const (
	// finaliserQueueCapacity bounds the guest finaliser and cleanup calls waiting for the
	// finaliser worker. A call that arrives while the queue is full is dropped and counted,
	// so the Go runtime's own goroutines never wait for guest code.
	finaliserQueueCapacity = 4096

	// defaultFinaliserTimeout bounds one run of a guest finaliser or cleanup when
	// VMLimits.FinaliserTimeout is zero.
	defaultFinaliserTimeout = 30 * time.Second
)

var (
	// guestFinalisers runs every guest finaliser and cleanup in the process.
	guestFinalisers = &finaliserWorker{
		mu:          sync.Mutex{},
		queue:       nil,
		running:     false,
		dropped:     atomic.Int64{},
		panicked:    atomic.Int64{},
		ran:         atomic.Int64{},
		dropLogged:  atomic.Bool{},
		panicLogged: atomic.Bool{},
	}
)

// finaliserJob is one call of a guest finaliser or cleanup that the Go runtime asked for.
type finaliserJob struct {
	// inExecution runs a body with the detached context of the execution that registered the
	// call, which carries its logger.
	inExecution func(body func(ctx context.Context))

	// owner is the global store of the programs that registered the call.
	owner *GlobalStore

	// function is the finaliser or cleanup, bounded by the finaliser deadline when it is
	// guest code.
	function reflect.Value

	// arguments are the values the Go runtime passed.
	arguments []reflect.Value
}

// finaliserWorker runs guest finalisers and cleanups on a goroutine the interpreter owns.
//
// The Go runtime runs every finaliser of the process on one goroutine and every cleanup
// on a few. Guest code run there could stall them all with a loop, or end the process
// with a panic. The runtime instead calls a trampoline (finaliserTrampoline,
// cleanupTrampoline) that only queues the call here. One goroutine at a time drains the
// queue, starting when a call arrives and ending when the queue is empty, so an idle
// worker holds no goroutine. Each call runs with its panics recovered and, for guest
// code, under the finaliser deadline (boundedFinaliserFunction).
type finaliserWorker struct {
	// queue holds the calls waiting to run, oldest first. The mu guards it.
	queue []finaliserJob

	// mu guards queue and running.
	mu sync.Mutex

	// dropped counts calls dropped because the queue was full or their store was closed.
	dropped atomic.Int64

	// panicked counts calls that panicked.
	panicked atomic.Int64

	// ran counts calls that ran to the end without panicking.
	ran atomic.Int64

	// running is true while a goroutine is draining the queue. The mu guards it.
	running bool

	// dropLogged is set once a dropped call has been logged.
	dropLogged atomic.Bool

	// panicLogged is set once a panicking call has been logged.
	panicLogged atomic.Bool
}

// enqueue queues job and starts a goroutine to drain the queue when none is running.
//
// Takes job (finaliserJob) which is the call to run.
//
// Returns bool which is false when the call was dropped.
//
// Safe for concurrent use. It never blocks on guest code.
func (w *finaliserWorker) enqueue(job finaliserJob) bool {
	if job.owner != nil && job.owner.finalisersClosed.Load() {
		w.drop(job, "its service is closed")
		return false
	}
	w.mu.Lock()
	if len(w.queue) >= finaliserQueueCapacity {
		w.mu.Unlock()
		w.drop(job, "the finaliser queue is full")
		return false
	}
	w.queue = append(w.queue, job)
	start := !w.running
	w.running = true
	w.mu.Unlock()
	if start {
		go w.drain()
	}
	return true
}

// drain runs queued calls until the queue is empty.
func (w *finaliserWorker) drain() {
	for {
		job, ok := w.next()
		if !ok {
			return
		}
		w.run(job)
	}
}

// next takes the oldest queued call, or marks the worker stopped when there is none.
//
// Returns the call and true, or false when the queue is empty.
//
// Safe for concurrent use. The worker mutex guards the queue and running flag.
func (w *finaliserWorker) next() (finaliserJob, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queue) == 0 {
		w.running = false
		w.queue = nil
		return finaliserJob{inExecution: nil, owner: nil, function: reflect.Value{}, arguments: nil}, false
	}
	job := w.queue[0]
	w.queue[0] = finaliserJob{inExecution: nil, owner: nil, function: reflect.Value{}, arguments: nil}
	w.queue = w.queue[1:]
	return job, true
}

// run calls job's function under the registering execution's context.
//
// Takes job (finaliserJob) which is the call.
func (w *finaliserWorker) run(job finaliserJob) {
	if job.owner != nil && job.owner.finalisersClosed.Load() {
		w.drop(job, "its service is closed")
		return
	}
	job.inExecution(func(ctx context.Context) { w.call(ctx, job) })
}

// call calls job's function, recovering and logging a panic.
//
// Takes job (finaliserJob) which is the call.
func (w *finaliserWorker) call(ctx context.Context, job finaliserJob) {
	defer func() {
		if recovered := recover(); recovered != nil {
			w.panicked.Add(1)
			if w.panicLogged.CompareAndSwap(false, true) {
				_, logger := logging.From(ctx)
				logger.Warn("a finaliser or cleanup panicked; the panic was dropped",
					logging.ErrAttr(fmt.Errorf("%w: %v", errFinaliserPanicked, recovered)))
			}
		}
	}()
	job.function.Call(job.arguments)
	w.ran.Add(1)
}

// drop counts a dropped call and logs the first under the registering execution's
// context.
//
// Takes job (finaliserJob) which is the dropped call.
// Takes reason (string) which says why the call was dropped.
func (w *finaliserWorker) drop(job finaliserJob, reason string) {
	w.dropped.Add(1)
	if w.dropLogged.CompareAndSwap(false, true) {
		job.inExecution(func(ctx context.Context) {
			_, logger := logging.From(ctx)
			logger.Warn("a finaliser or cleanup was dropped", "reason", reason)
		})
	}
}

// discard removes the queued calls of owner.
//
// Takes owner (*GlobalStore) which is the closed store.
//
// Safe for concurrent use. The worker mutex guards removal of queued calls.
func (w *finaliserWorker) discard(owner *GlobalStore) {
	w.mu.Lock()
	defer w.mu.Unlock()
	before := len(w.queue)
	w.queue = slices.DeleteFunc(w.queue, func(job finaliserJob) bool { return job.owner == owner })
	w.dropped.Add(int64(before - len(w.queue)))
}

// CloseFinalisers stops the guest finalisers and cleanups that programs on g registered.
// Those queued are dropped, and those the Go runtime calls later do not run.
//
// Repeated calls have no effect. The worker serialises removal of queued calls.
func (g *GlobalStore) CloseFinalisers() {
	g.finalisersClosed.Store(true)
	guestFinalisers.discard(g)
}

// finaliserTimeout returns the deadline of one guest finaliser or cleanup run under
// limits.
//
// Takes limits (VMLimits) which are the registering VM's limits.
//
// Returns time.Duration which is FinaliserTimeout, or defaultFinaliserTimeout when that
// is zero, or CallbackTimeout when that is positive and shorter.
func finaliserTimeout(limits VMLimits) time.Duration {
	timeout := limits.FinaliserTimeout
	if timeout <= 0 {
		timeout = defaultFinaliserTimeout
	}
	if limits.CallbackTimeout > 0 && limits.CallbackTimeout < timeout {
		timeout = limits.CallbackTimeout
	}
	return timeout
}

// finaliserCandidate is the closure wrapper a VM most recently made in a program that
// sets finalisers, so that a finaliser or cleanup argument made from a closure can be
// recognised and run under the finaliser deadline.
type finaliserCandidate struct {
	// wrapper is what the function value runs.
	wrapper *closureWrapper

	// value is the wrapper's function value.
	value reflect.Value
}

// noteFinaliserCandidate records the closure wrapper vm just made, when vm's program sets
// finalisers.
//
// Takes value (reflect.Value) which is the wrapper's function value.
// Takes wrapper (*closureWrapper) which is what it runs.
func (vm *VM) noteFinaliserCandidate(value reflect.Value, wrapper *closureWrapper) {
	if vm.allocatesForFinalisers() {
		vm.finaliserCandidate = finaliserCandidate{value: value, wrapper: wrapper}
	}
}

// boundedFinaliserFunction returns the function the finaliser worker runs for function.
//
// When function is the closure wrapper vm last made, the result runs the same closure
// under the finaliser deadline (finaliserTimeout) on a fresh callback VM, so a guest loop
// stops with fault.ErrCallbackTimeout. Any other function is returned as it is.
//
// Takes function (reflect.Value) which is the finaliser or cleanup.
//
// Returns reflect.Value which is the function to run.
func (vm *VM) boundedFinaliserFunction(function reflect.Value) reflect.Value {
	candidate := vm.finaliserCandidate
	vm.finaliserCandidate = finaliserCandidate{value: reflect.Value{}, wrapper: nil}
	if candidate.wrapper == nil || !sameFunctionValue(candidate.value, function) {
		return function
	}
	bounded := candidate.bounded()
	wrapper := reflect.MakeFunc(bounded.functionType, bounded.reflectCall)
	registerClosureWrapper(vm, wrapper, bounded.closure)
	return wrapper
}

// bounded returns a wrapper of the candidate's closure whose runs stop at the finaliser
// deadline.
//
// Returns *closureWrapper which is the bounded wrapper.
func (c finaliserCandidate) bounded() *closureWrapper {
	spec := *c.wrapper.spec
	spec.limits.CallbackTimeout = finaliserTimeout(spec.limits)
	spec.cache = nil
	spec.link = endedExecutionLink(spec.link.detached)
	return &closureWrapper{
		spec:         &spec,
		closure:      c.wrapper.closure,
		functionType: c.wrapper.functionType,
		handoff:      nil,
	}
}

// sameFunctionValue reports whether two Values hold the same function value. They must
// have the same type and the same closure record, which the Values' own words compare.
//
// Takes made (reflect.Value) which is a wrapper as it was made.
// Takes passed (reflect.Value) which is the argument as the call received it.
//
// Returns bool which is true when they are one function value.
func sameFunctionValue(made reflect.Value, passed reflect.Value) bool {
	if passed.IsValid() && passed.Kind() == reflect.Interface {
		passed = passed.Elem()
	}
	if !made.IsValid() || !passed.IsValid() || made.Kind() != reflect.Func || passed.Kind() != reflect.Func {
		return false
	}
	return made.Type() == passed.Type() && made == passed
}

// finaliserTrampoline returns the function runtime.SetFinalizer registers in place of a
// guest finaliser. It re-records the object, whose finaliser the runtime has cleared, as
// a whole allocation that may take a new finaliser, and queues the call.
//
// Takes vm (*VM) which registered the finaliser.
// Takes function (reflect.Value) which is the finaliser to run.
//
// Returns reflect.Value which has function's type.
func finaliserTrampoline(vm *VM, function reflect.Value) reflect.Value {
	functionType := function.Type()
	owner := vm.Globals
	inExecution := detachedExecution(vm.ctx)
	return reflect.MakeFunc(functionType, func(arguments []reflect.Value) []reflect.Value {
		object := arguments[0]
		if object.Kind() == reflect.Interface {
			object = object.Elem()
		}
		finaliserAllocations.finaliserRan(object)
		guestFinalisers.enqueue(finaliserJob{inExecution: inExecution, owner: owner, function: function, arguments: slices.Clone(arguments)})
		return zeroResults(functionType)
	})
}

// cleanupTrampoline returns the function runtime.AddCleanup attaches in place of a guest
// cleanup. It queues the call.
//
// Takes vm (*VM) which attached the cleanup.
// Takes function (reflect.Value) which is the cleanup, of type func(any).
//
// Returns func(any) which the runtime calls.
func cleanupTrampoline(vm *VM, function reflect.Value) func(any) {
	owner := vm.Globals
	inExecution := detachedExecution(vm.ctx)
	return func(argument any) {
		held := reflect.New(cleanupArgumentType).Elem()
		if argument != nil {
			held.Set(reflect.ValueOf(argument))
		}
		guestFinalisers.enqueue(finaliserJob{inExecution: inExecution, owner: owner, function: function, arguments: []reflect.Value{held}})
	}
}

// detachedExecution returns the closure a queued finaliser or cleanup runs its logging
// under. It holds the registering execution's context without its cancellation, since the
// call may come after the execution ended.
//
// Returns func(func(context.Context)) which runs a body with that context.
func detachedExecution(ctx context.Context) func(body func(ctx context.Context)) {
	detached := context.WithoutCancel(ctx)
	return func(body func(ctx context.Context)) { body(detached) }
}

// zeroResults returns the zero value of each of functionType's results.
//
// Takes functionType (reflect.Type) which is a function type.
//
// Returns []reflect.Value which holds one zero value per result.
func zeroResults(functionType reflect.Type) []reflect.Value {
	results := make([]reflect.Value, functionType.NumOut())
	for index := range results {
		results[index] = reflect.Zero(functionType.Out(index))
	}
	return results
}
