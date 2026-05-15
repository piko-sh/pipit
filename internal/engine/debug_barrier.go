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
	"pipit.sh/pipit/internal/engine/program"
)

// stopWorldLocked begins a stop: every other thread parks at its next safe point and the
// active step is cancelled. The mutex must be held.
//
// Takes focus (*DebugThread) which is the thread whose pause stopped the world.
func (s *DebugSession) stopWorldLocked(focus *DebugThread) {
	if s.stopped {
		return
	}
	s.stopped = true
	s.stopGen++
	s.focus = focus
	s.step = debugStepState{vm: nil, file: "", goroutineID: 0, framePointer: 0, line: 0, mode: debugStepNone}
	s.quiescent = make(chan struct{})
	for tracker := range s.trackers {
		tracker.blockEpoch.Add(1)
	}
	s.recomputeActiveLocked()
	s.checkQuiescentLocked()
}

// releaseWorldLocked ends a stop and wakes every parked thread; the mutex must be held.
func (s *DebugSession) releaseWorldLocked() {
	s.stopped = false
	s.focus = nil
	for _, thread := range s.threads {
		if thread.state == DebugThreadParked {
			select {
			case thread.wake <- struct{}{}:
			default:
			}
		}
	}
	s.recomputeActiveLocked()
}

// checkQuiescentLocked closes the quiescent channel once no thread is running during a
// stop; the mutex must be held.
func (s *DebugSession) checkQuiescentLocked() {
	if !s.stopped || s.running != 0 || s.quiescent == nil {
		return
	}
	select {
	case <-s.quiescent:
	default:
		close(s.quiescent)
	}
}

// pauseHereLocked records a pause for t at the given position, stops the world if it is
// running, and parks the calling goroutine until the client resumes. The mutex must be
// held on entry and is released while parked.
//
// Takes pause (DebugPause) which describes the stop; Thread is filled in.
// Takes exactPC (bool) which is true when pause.PC is the instruction about to execute.
//
// Returns program.DebugAction which is Stop when the client ended the execution,
// otherwise Continue (the step state, if any, was installed by Resume).
func (t *DebugThread) pauseHereLocked(pause DebugPause, exactPC bool) program.DebugAction {
	s := t.session
	pause.thread = t
	t.pausedPC = pause.pc
	t.pausedPCExact = exactPC
	s.stopWorldLocked(t)
	t.parkedAtGen = s.stopGen
	if t.state == debugThreadRunning {
		s.running--
	}
	t.state = DebugThreadParked
	stored := new(DebugPause)
	*stored = pause
	s.appendEventLocked(DebugSessionEvent{Pause: stored, Thread: t.infoLocked(), Kind: DebugSessionEventPaused})
	s.checkQuiescentLocked()
	return t.parkLocked()
}

// parkLocked waits until the world is released, the execution is cancelled or the
// debugger stops it. The mutex must be held on entry and is held again on return only
// long enough to restore the running state.
//
// Returns program.DebugAction which is Stop when the debugger ended the execution or the
// execution was cancelled (debugWokenByCancel then tells debugStopError which).
func (t *DebugThread) parkLocked() program.DebugAction {
	s := t.session
	vm := t.vm
	s.mu.Unlock()
	released := vm.releaseInterpreterLockForBlock()
	action := t.waitReleased()
	vm.reacquireInterpreterLockAfterBlock(released)
	s.mu.Lock()
	t.state = debugThreadRunning
	s.running++
	s.mu.Unlock()
	return action
}

// waitReleased is the wait loop of parkLocked, run without the mutex.
//
// Returns program.DebugAction which is Stop when the debugger ended the execution.
func (t *DebugThread) waitReleased() program.DebugAction {
	s := t.session
	for {
		s.mu.Lock()
		terminated := s.terminated
		released := !s.stopped || t.parkedAtGen != s.stopGen
		s.mu.Unlock()
		if terminated {
			return program.DebugActionStop
		}
		if released {
			return program.DebugActionContinue
		}
		select {
		case <-t.wake:
		case <-t.vm.debugParkDone:
			t.vm.debugWokenByCancel = true
			return program.DebugActionStop
		case <-s.terminate:
			return program.DebugActionStop
		}
	}
}

// parkForWorldLocked parks a thread that reached a safe point while another thread has
// the world stopped; the mutex must be held.
//
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
// Takes exactPC (bool) which is true when pc is the instruction about to execute.
//
// Returns program.DebugAction which is Stop when the debugger ended the execution.
func (t *DebugThread) parkForWorldLocked(function *program.CompiledFunction, pc int, exactPC bool) program.DebugAction {
	pause := t.vm.debugPauseAt(function, pc, program.DebugEventThreadPaused)
	return t.pauseHereLocked(pause, exactPC)
}

// debugBlockEnter marks the VM's thread blocked before a blocking operation another VM
// must complete, so the stop barrier does not wait for it.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (vm *VM) debugBlockEnter() {
	t := vm.debugThread
	if t == nil {
		return
	}
	s := t.session
	s.mu.Lock()
	if t.state == debugThreadRunning {
		t.state = DebugThreadBlocked
		s.running--
		s.checkQuiescentLocked()
	}
	s.mu.Unlock()
}

// debugBlockLeave marks the VM's thread running again after a blocking operation and,
// when the world was stopped meanwhile, parks it before the operation's result is written
// to a register.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (vm *VM) debugBlockLeave() {
	t := vm.debugThread
	if t == nil {
		return
	}
	s := t.session
	s.mu.Lock()
	if t.state == DebugThreadBlocked {
		t.state = debugThreadRunning
		s.running++
	}
	if s.stopped && t.parkedAtGen != s.stopGen && vm.FramePointer >= 0 {
		frame := &vm.CallStack[vm.FramePointer]
		_ = t.parkForWorldLocked(frame.Function, frameInstructionIndex(frame), false)
		return
	}
	s.mu.Unlock()
}
