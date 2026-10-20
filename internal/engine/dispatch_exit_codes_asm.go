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

//go:build !safe

package engine

const (
	// exitEndOfCode indicates that the program counter reached the end of the code.
	// Exit values must also be distinct from those in dispatch_exit_codes.go.
	exitEndOfCode int64 = 0

	// exitPathB indicates the dispatch loop exited because a tier 2 opcode requires Go-side
	// handling.
	exitPathB int64 = 1

	// exitDivByZero indicates the dispatch loop exited due to a division by zero error.
	exitDivByZero int64 = 2

	// exitCall indicates the dispatch loop exited to perform a function call via Go.
	exitCall int64 = 3

	// exitReturn indicates the dispatch loop exited to perform a function return via Go.
	exitReturn int64 = 4

	// exitReturnVoid indicates the dispatch loop exited to perform a void function return
	// via Go.
	exitReturnVoid int64 = 5

	// exitTailCall indicates the dispatch loop exited to perform a tail call via Go.
	exitTailCall int64 = 6

	// exitCallOverflow indicates the dispatch loop exited because the call depth limit was
	// exceeded.
	exitCallOverflow int64 = 7

	// exitGetStructFieldIntT0 is the direct-exit reason for the tier-0 int struct-field
	// reader. The matching processExit dispatcher calls the existing Go handler directly,
	// skipping the handlerTable[op] indirect.
	exitGetStructFieldIntT0 int64 = 13

	// exitGetStructFieldUintT0 is the direct-exit reason for the tier-0 uint struct-field
	// reader.
	exitGetStructFieldUintT0 int64 = 14

	// exitGetStructFieldFloatT0 is the direct-exit reason for the tier-0 float struct-field
	// reader.
	exitGetStructFieldFloatT0 int64 = 15

	// exitGetStructFieldBoolT0 is the direct-exit reason for the tier-0 bool struct-field
	// reader.
	exitGetStructFieldBoolT0 int64 = 16

	// exitSetStructFieldIntT0 is the direct-exit reason for the tier-0 int struct-field
	// writer. Primitive kind only; no GC barrier required (primitives carry no pointers).
	exitSetStructFieldIntT0 int64 = 17

	// exitSetStructFieldUintT0 is the direct-exit reason for the tier-0 uint struct-field
	// writer.
	exitSetStructFieldUintT0 int64 = 18

	// exitSetStructFieldFloatT0 is the direct-exit reason for the tier-0 float struct-field
	// writer.
	exitSetStructFieldFloatT0 int64 = 19

	// exitSetStructFieldBoolT0 is the direct-exit reason for the tier-0 bool struct-field
	// writer.
	exitSetStructFieldBoolT0 int64 = 20

	// exitFrameChanged is the tier-2 ASM-lift exit code for opFrameChanged.
	exitFrameChanged int64 = 21

	// exitPanicErrorPathB is the tier-2 ASM-lift exit code for opPanicError. See
	// exitFrameChanged for the OpResult-to-exitReason mapping table.
	exitPanicErrorPathB int64 = 22

	// exitStackOverflowPathB is the tier-2 ASM-lift exit code for OpStackOverflow. See
	// exitFrameChanged for the OpResult-to-exitReason mapping table.
	exitStackOverflowPathB int64 = 23

	// exitDonePathB is the tier-2 ASM-lift exit code for OpDone. See exitFrameChanged for
	// the OpResult-to-exitReason mapping table.
	exitDonePathB int64 = 24

	// exitGetStructFieldGeneralT0 requests the tier-0 general-bank struct-field reader.
	exitGetStructFieldGeneralT0 int64 = 25

	// exitSetStructFieldGeneralT0 requests the tier-0 general-bank struct-field writer.
	exitSetStructFieldGeneralT0 int64 = 26

	// exitTestNilJumpFalse requests the tier-2 nil test that branches when the pointer
	// is non-nil.
	exitTestNilJumpFalse int64 = 27

	// exitTestNilJumpTrue mirrors exitTestNilJumpFalse for the inverted-sense nil test.
	exitTestNilJumpTrue int64 = 28

	// exitPoll returns to Go to check cancellation and deadlines in assembly loops.
	exitPoll int64 = 29

	// dispatchPollBudget sets the assembly poll interval to match
	// cancellationCheckMask+1 in the Go loop.
	dispatchPollBudget int64 = 1024
)
