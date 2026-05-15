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

// dispatchAction indicates what the dispatch loop should do after handling an exit from
// the dispatch loop: rebuild the context, continue, or return to the caller.
type dispatchAction int

const (
	// loopRebuild instructs the dispatch loop to rebuild context and re-enter dispatch.
	loopRebuild dispatchAction = iota

	// loopContinue instructs the dispatch loop to skip rebuild and re-enter dispatch.
	loopContinue

	// loopReturn instructs the dispatch loop to return result and error to caller.
	loopReturn
)

// dispatchOutcome is what a terminal OpResult resolves to: the value to hand back, the
// error to report and what the dispatch loop does next.
type dispatchOutcome struct {
	// result is the evaluation result when the program finished.
	result any

	// err is the error to return, nil when the program finished or must keep running.
	err error

	// action is loopReturn when execution ends here and loopRebuild when an interpreted
	// panic changed the frame and dispatch must resume there.
	action dispatchAction
}

// raiseDivByZeroForDispatch raises a divide-by-zero runtime panic as an interpreted panic
// so defer/recover can observe it.
//
// Returns the result value, the dispatch action, and any propagated error.
func (vm *VM) raiseDivByZeroForDispatch() (any, dispatchAction, error) {
	result := raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: integer divide by zero"))
	switch result {
	case opDone:
		r := vm.evalResult
		vm.evalResult = nil
		return r, loopReturn, nil
	case opPanicError:
		err := vm.evalError
		vm.evalError = nil
		return nil, loopReturn, err
	case opFrameChanged:
		return nil, loopRebuild, nil
	default:
	}
	return nil, loopRebuild, nil
}

// resolveTerminalResult maps a terminal OpResult to its outcome. It is the one place that
// turns OpDone, OpDivByZero, OpStackOverflow and opPanicError into what the caller sees;
// every dispatch loop and HandleOpResult route through it, so a divide by zero raises the
// same interpreted, recoverable panic on every path.
//
// Takes rc (OpResult) which a handler returned.
//
// Returns dispatchOutcome which is the resolved outcome.
// Returns bool which is false for opContinue and frame-changing results.
func (vm *VM) resolveTerminalResult(rc OpResult) (dispatchOutcome, bool) {
	switch rc {
	case opDone:
		result := vm.evalResult
		vm.evalResult = nil
		return dispatchOutcome{result: result, err: nil, action: loopReturn}, true
	case opDivByZero:
		result, action, err := vm.raiseDivByZeroForDispatch()
		return dispatchOutcome{result: result, err: err, action: action}, true
	case opStackOverflow:
		return dispatchOutcome{result: nil, err: vm.stackOverflowError(), action: loopReturn}, true
	case opPanicError:
		err := vm.evalError
		vm.evalError = nil
		return dispatchOutcome{result: nil, err: err, action: loopReturn}, true
	default:
		return dispatchOutcome{result: nil, err: nil, action: loopContinue}, false
	}
}
