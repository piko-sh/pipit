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
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"pipit.sh/pipit/internal/engine/program"
)

const (
	// callStackPCShift is the bit position of the function index in a synthesised program
	// counter: the low word is the instruction index, the high word the registry index plus
	// one, so zero never names a function.
	callStackPCShift = 32

	// callStackPCMask extracts the instruction index from a synthesised program counter.
	callStackPCMask = 1<<callStackPCShift - 1

	// rootGoroutineID is the id the main VM reports in stack dumps, as Go's main goroutine
	// does.
	rootGoroutineID = 1

	// pseudoFrameCallers is the runtime frame Go places at the top of a goroutine's stack
	// when returning from Callers.
	pseudoFrameCallers = "runtime.Callers"

	// pseudoFrameMain is the runtime frame Go places at the bottom of the main goroutine's
	// stack.
	pseudoFrameMain = "runtime.main"

	// pseudoFrameGoexit is the runtime frame Go places at the bottom of every spawned
	// goroutine's stack.
	pseudoFrameGoexit = "runtime.goexit"

	// pseudoFrameGopanic is the runtime frame Go shows between a deferred function and the
	// frame whose panic is running it.
	pseudoFrameGopanic = "runtime.gopanic"

	// unknownFile is what Go prints for a position whose //line directive named no file.
	unknownFile = "??"
)

var (
	// runtimeCallerPointer identifies runtime.Caller among native callees so the interpreter
	// can answer it from script frames.
	runtimeCallerPointer = reflect.ValueOf(runtime.Caller).Pointer()

	// runtimeCallersPointer identifies runtime.Callers.
	runtimeCallersPointer = reflect.ValueOf(runtime.Callers).Pointer()

	// runtimeCallersFramesPointer identifies runtime.CallersFrames.
	runtimeCallersFramesPointer = reflect.ValueOf(runtime.CallersFrames).Pointer()

	// runtimeFuncForPCPointer identifies runtime.FuncForPC.
	runtimeFuncForPCPointer = reflect.ValueOf(runtime.FuncForPC).Pointer()

	// runtimeStackPointer identifies runtime.Stack.
	runtimeStackPointer = reflect.ValueOf(runtime.Stack).Pointer()

	// debugStackPointer identifies debug.Stack.
	debugStackPointer = reflect.ValueOf(debug.Stack).Pointer()

	// debugPrintStackPointer identifies debug.PrintStack.
	debugPrintStackPointer = reflect.ValueOf(debug.PrintStack).Pointer()

	// runtimeFuncNamePointer identifies (*runtime.Func).Name as a method expression so the
	// registry can intercept it.
	runtimeFuncNamePointer = reflect.ValueOf((*runtime.Func).Name).Pointer()

	// runtimeFuncEntryPointer identifies (*runtime.Func).Entry.
	runtimeFuncEntryPointer = reflect.ValueOf((*runtime.Func).Entry).Pointer()

	// runtimeFuncFileLinePointer identifies (*runtime.Func).FileLine.
	runtimeFuncFileLinePointer = reflect.ValueOf((*runtime.Func).FileLine).Pointer()
)

// runtimeFuncPointerType is the static type of the handles runtime.FuncForPC returns.
var runtimeFuncPointerType = reflect.TypeFor[*runtime.Func]()

// pipitFunc stands in for *runtime.Func: the value runtime.FuncForPC hands back for a
// synthesised program counter. Method calls on a *runtime.Func static type reach it by
// name, and one value per function keeps == comparisons meaningful.
type pipitFunc struct {
	// function is the compiled function, nil for a runtime pseudo frame.
	function *program.CompiledFunction

	// name is the qualified function name (main.f).
	name string

	// entry is the synthesised program counter of the function's first instruction.
	entry uintptr
}

// Name returns the qualified function name, or "" for a nil receiver as runtime.Func
// does.
//
// Returns string which is the name.
func (f *pipitFunc) Name() string {
	if f == nil {
		return ""
	}
	return f.name
}

// Entry returns the synthesised entry program counter, or 0 for a nil receiver.
//
// Returns uintptr which is the entry.
func (f *pipitFunc) Entry() uintptr {
	if f == nil {
		return 0
	}
	return f.entry
}

// FileLine returns the source position of a synthesised program counter within the
// function.
//
// Takes pc (uintptr) which is a program counter runtime.Caller or Callers produced.
//
// Returns string which is the file, empty when unknown.
// Returns int which is the line, 0 when unknown.
func (f *pipitFunc) FileLine(pc uintptr) (string, int) {
	if f == nil || f.function == nil {
		return "", 0
	}
	return nearestSourcePosition(f.function, int(pc&callStackPCMask))
}

// pc synthesises the program counter of an instruction of the function.
//
// Takes offset (int) which is the instruction index.
//
// Returns uintptr which encodes the function and the instruction.
func (f *pipitFunc) pc(offset int) uintptr {
	return f.entry | uintptr(offset)&callStackPCMask
}

// pipitFrames stands in for *runtime.Frames: the iterator runtime.CallersFrames returns.
type pipitFrames struct {
	// frames is the decoded frame list.
	frames []runtime.Frame

	// index is the next frame Next hands out.
	index int
}

// Next returns the next frame and whether more follow, as runtime.Frames.Next does.
//
// Returns runtime.Frame which is the frame, zero when exhausted.
// Returns bool which is true when another frame follows.
func (f *pipitFrames) Next() (runtime.Frame, bool) {
	if f == nil || f.index >= len(f.frames) {
		return runtime.Frame{PC: 0, Func: nil, Function: "", File: "", Line: 0, Entry: 0}, false
	}
	frame := f.frames[f.index]
	f.index++
	return frame, f.index < len(f.frames)
}

// callStackRegistry hands out stable synthesised program counters: every compiled
// function seen on a stack gets one registry index for the life of the program, so a
// counter taken by runtime.Caller decodes again in runtime.FuncForPC or
// runtime.CallersFrames.
type callStackRegistry struct {
	// byFunction finds a compiled function's entry.
	byFunction map[*program.CompiledFunction]*pipitFunc

	// byName finds a runtime pseudo frame's entry.
	byName map[string]*pipitFunc

	// byHandle finds the entry behind a *runtime.Func handle a script holds.
	byHandle map[uintptr]*pipitFunc

	// functions is indexed by registry index.
	functions []*pipitFunc

	// mu guards the maps and slice: goroutine VMs share one registry.
	mu sync.Mutex
}

// newCallStackRegistry constructs an empty registry.
//
// Returns *callStackRegistry which is ready to use.
func newCallStackRegistry() *callStackRegistry {
	return &callStackRegistry{
		byFunction: make(map[*program.CompiledFunction]*pipitFunc),
		byName:     make(map[string]*pipitFunc),
		byHandle:   make(map[uintptr]*pipitFunc),
		functions:  nil,
		mu:         sync.Mutex{},
	}
}

// forFunction returns the entry for a compiled function, registering it on first sight.
//
// Takes function (*program.CompiledFunction) which is the function on the stack.
//
// Returns *pipitFunc which is the function's entry.
//
// Safe for concurrent use; acquires registry.mu.
func (registry *callStackRegistry) forFunction(function *program.CompiledFunction) *pipitFunc {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if entry, ok := registry.byFunction[function]; ok {
		return entry
	}
	name := function.RuntimeName
	if name == "" {
		name = function.Name
	}
	entry := registry.register(&pipitFunc{function: function, name: name, entry: 0})
	registry.byFunction[function] = entry
	return entry
}

// pseudo returns the entry for a runtime frame the interpreter reports around a
// goroutine's own frames, registering it on first sight.
//
// Takes name (string) which is the frame's qualified name.
//
// Returns *pipitFunc which is the frame's entry.
func (registry *callStackRegistry) pseudo(name string) *pipitFunc {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if entry, ok := registry.byName[name]; ok {
		return entry
	}
	entry := registry.register(&pipitFunc{function: nil, name: name, entry: 0})
	registry.byName[name] = entry
	return entry
}

// register appends an entry and assigns its entry program counter. Callers hold mu.
//
// Takes entry (*pipitFunc) which is the new entry.
//
// Returns *pipitFunc which is entry, for chaining.
func (registry *callStackRegistry) register(entry *pipitFunc) *pipitFunc {
	registry.functions = append(registry.functions, entry)
	entry.entry = uintptr(len(registry.functions)) << callStackPCShift
	if key := handleKey(entry); key != 0 {
		registry.byHandle[key] = entry
	}
	return entry
}

// fromHandle finds the entry behind a *runtime.Func value.
//
// Takes value (reflect.Value) which holds the handle.
//
// Returns *pipitFunc which is the entry, nil for a nil or foreign handle.
//
// Safe for concurrent use; acquires registry.mu.
func (registry *callStackRegistry) fromHandle(value reflect.Value) *pipitFunc {
	key := handleAddress(value)
	if key == 0 {
		return nil
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.byHandle[key]
}

// lookup decodes a synthesised program counter.
//
// A counter past the end of a function's body names no function, so a script stepping a
// counter forward leaves the function as it would with real code.
//
// Takes pc (uintptr) which is the counter to decode.
//
// Returns *pipitFunc which is the function's entry, nil when pc names none.
//
// Safe for concurrent use; acquires registry.mu.
func (registry *callStackRegistry) lookup(pc uintptr) *pipitFunc {
	index := int(pc>>callStackPCShift) - 1
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if index < 0 || index >= len(registry.functions) {
		return nil
	}
	entry := registry.functions[index]
	if entry.function != nil && int(pc&callStackPCMask) > len(entry.function.Body) {
		return nil
	}
	return entry
}

// stackFrame is one interpreted frame as the inspection intrinsics report it.
type stackFrame struct {
	// function is the frame's registry entry.
	function *pipitFunc

	// file is the source file of the executing instruction, empty when unknown.
	file string

	// pc is the index of the executing instruction.
	pc int

	// line is the source line of the executing instruction, 0 when unknown.
	line int
}

// panicStack renders the frames recorded when the current panic started, Go-style.
//
// Returns string which is empty when no panic is active.
func (vm *VM) panicStack() string {
	return formatFrames(vm.panicFrames)
}

// interpretedFrames lists the VM's live frames, innermost first.
//
// Deferred calls run on top of the frames they unwind, so a deferred function sees the
// panicking frames below it as in Go, with the runtime.gopanic frame between a deferred
// call and the panic that runs it.
//
// Returns []stackFrame which is the frame list.
func (vm *VM) interpretedFrames() []stackFrame {
	frames := make([]stackFrame, 0, vm.FramePointer+1)
	for index := vm.FramePointer; index >= 0; index-- {
		frame := &vm.CallStack[index]
		if frame.Function == nil {
			continue
		}
		frames = append(frames, vm.stackFrameFor(frame.Function, frameInstructionIndex(frame)))
		if record, ok := vm.panicDeferRecord(index); ok {
			frames = append(frames, stackFrame{function: vm.Globals.callStack.pseudo(pseudoFrameGopanic), file: "", pc: 0, line: 0})
			for _, unwound := range record.unwound {
				frames = append(frames, vm.stackFrameFor(unwound.function, unwound.pc))
			}
		}
	}
	return frames
}

// stackFrameFor describes one executing instruction of a function.
//
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index, len(Body) for the epilogue.
//
// Returns stackFrame which carries the registry entry and the source position.
func (vm *VM) stackFrameFor(function *program.CompiledFunction, pc int) stackFrame {
	file, line := nearestSourcePosition(function, pc)
	return stackFrame{function: vm.Globals.callStack.forFunction(function), file: file, pc: pc, line: line}
}

// panicDeferRecord finds the record of a deferred call running on behalf of a panic at
// the given frame index.
//
// Takes index (int) which is the frame's position in the call stack.
//
// Returns panicDeferRecord which is the record.
// Returns bool which is true when the frame has such a record.
func (vm *VM) panicDeferRecord(index int) (panicDeferRecord, bool) {
	for _, record := range vm.panicDeferFrames {
		if record.frameIndex == index {
			return record, true
		}
	}
	return panicDeferRecord{unwound: nil, frameIndex: 0}, false
}

// callStackIntrinsicResults evaluates the intrinsic a function pointer names.
//
// Takes pointer (uintptr) which identifies the native function.
// Takes arguments ([]reflect.Value) which are the coerced operands.
//
// Returns []reflect.Value which are the results.
// Returns bool which is true when pointer names an intrinsic and the operands have the
// expected shape.
func (vm *VM) callStackIntrinsicResults(pointer uintptr, arguments []reflect.Value) ([]reflect.Value, bool) {
	switch pointer {
	case runtimeCallerPointer:
		if len(arguments) != 1 {
			return nil, false
		}
		return vm.runtimeCaller(int(arguments[0].Int())), true
	case runtimeCallersPointer:
		if len(arguments) != 2 || arguments[1].Kind() != reflect.Slice {
			return nil, false
		}
		return []reflect.Value{reflect.ValueOf(vm.runtimeCallers(int(arguments[0].Int()), arguments[1]))}, true
	case runtimeCallersFramesPointer:
		if len(arguments) != 1 || arguments[0].Kind() != reflect.Slice {
			return nil, false
		}
		return []reflect.Value{reflect.ValueOf(vm.callersFrames(arguments[0]))}, true
	case runtimeFuncForPCPointer:
		if len(arguments) != 1 {
			return nil, false
		}
		return []reflect.Value{reflect.ValueOf(funcHandle(vm.Globals.callStack.lookup(uintptr(arguments[0].Uint()))))}, true
	case runtimeStackPointer:
		if len(arguments) != 2 || arguments[0].Kind() != reflect.Slice {
			return nil, false
		}
		return []reflect.Value{reflect.ValueOf(reflect.Copy(arguments[0], reflect.ValueOf(vm.formatStack())))}, true
	case debugStackPointer:
		return []reflect.Value{reflect.ValueOf(vm.formatStack())}, true
	case debugPrintStackPointer:
		vm.printStack()
		return nil, true
	case runtimeFuncNamePointer, runtimeFuncEntryPointer, runtimeFuncFileLinePointer:
		return vm.answerRuntimeFuncMethod(pointer, arguments)
	default:
		return nil, false
	}
}

// answerRuntimeFuncMethod evaluates a *runtime.Func method expression whose receiver is
// the first argument.
//
// Takes pointer (uintptr) which identifies the method expression.
// Takes arguments ([]reflect.Value) which hold the receiver and the method's operands.
//
// Returns []reflect.Value which are the results.
// Returns bool which is true when the call was answered.
func (vm *VM) answerRuntimeFuncMethod(pointer uintptr, arguments []reflect.Value) ([]reflect.Value, bool) {
	if len(arguments) == 0 {
		return nil, false
	}
	entry := vm.Globals.callStack.fromHandle(arguments[0])
	switch pointer {
	case runtimeFuncNamePointer:
		return []reflect.Value{reflect.ValueOf(entry.Name())}, true
	case runtimeFuncEntryPointer:
		return []reflect.Value{reflect.ValueOf(entry.Entry())}, true
	case runtimeFuncFileLinePointer:
		if len(arguments) != 2 {
			return nil, false
		}
		file, line := entry.FileLine(uintptr(arguments[1].Uint()))
		return []reflect.Value{reflect.ValueOf(file), reflect.ValueOf(line)}, true
	default:
		return nil, false
	}
}

// runtimeCaller answers runtime.Caller: skip 0 is the function that called Caller.
//
// Takes skip (int) which is the number of frames to skip.
//
// Returns []reflect.Value holding pc, file, line and ok.
func (vm *VM) runtimeCaller(skip int) []reflect.Value {
	frames := vm.interpretedFrames()
	if skip < 0 || skip >= len(frames) {
		return []reflect.Value{reflect.ValueOf(uintptr(0)), reflect.ValueOf(""), reflect.ValueOf(0), reflect.ValueOf(false)}
	}
	frame := frames[skip]
	return []reflect.Value{reflect.ValueOf(frame.function.pc(frame.pc)), reflect.ValueOf(frame.file), reflect.ValueOf(frame.line), reflect.ValueOf(true)}
}

// callerTokens lists the program counters runtime.Callers reports with skip 0: the
// Callers frame itself, the interpreted frames, and the runtime frames Go shows below a
// goroutine's own (runtime.main on the main goroutine, then runtime.goexit).
//
// Returns []uintptr which is the counter list, innermost first.
func (vm *VM) callerTokens() []uintptr {
	registry := vm.Globals.callStack
	frames := vm.interpretedFrames()
	tokens := make([]uintptr, 0, len(frames)+3)
	tokens = append(tokens, registry.pseudo(pseudoFrameCallers).entry)
	for _, frame := range frames {
		tokens = append(tokens, frame.function.pc(frame.pc))
	}
	if vm.goroutineID == rootGoroutineID {
		tokens = append(tokens, registry.pseudo(pseudoFrameMain).entry)
	}
	return append(tokens, registry.pseudo(pseudoFrameGoexit).entry)
}

// runtimeCallers answers runtime.Callers, filling the caller's slice.
//
// Takes skip (int) which is the number of frames to skip, 0 naming Callers itself.
// Takes pcs (reflect.Value) which is the []uintptr to fill.
//
// Returns int which is the number of entries written.
func (vm *VM) runtimeCallers(skip int, pcs reflect.Value) int {
	tokens := vm.callerTokens()
	skip = min(max(skip, 0), len(tokens))
	tokens = tokens[skip:]
	count := min(len(tokens), pcs.Len())
	for i := range count {
		pcs.Index(i).SetUint(uint64(tokens[i]))
	}
	return count
}

// callersFrames answers runtime.CallersFrames, decoding every counter up front.
//
// Takes pcs (reflect.Value) which is the []uintptr runtime.Callers filled.
//
// Returns *pipitFrames which iterates the decoded frames.
func (vm *VM) callersFrames(pcs reflect.Value) *pipitFrames {
	frames := &pipitFrames{frames: make([]runtime.Frame, 0, pcs.Len()), index: 0}
	for i := range pcs.Len() {
		token := uintptr(pcs.Index(i).Uint())
		frame := runtime.Frame{PC: token, Func: nil, Function: "", File: "", Line: 0, Entry: 0}
		if function := vm.Globals.callStack.lookup(token); function != nil {
			frame.Func = funcHandle(function)
			frame.Function = function.name
			frame.Entry = function.entry
			frame.File, frame.Line = function.FileLine(token)
		}
		frames.frames = append(frames.frames, frame)
	}
	return frames
}

// formatStack renders the goroutine's frames the way runtime/debug.Stack does: a
// goroutine header, the Stack frame itself, then each interpreted frame as a name line
// and an indented file:line line.
//
// Returns []byte which is the rendered stack.
func (vm *VM) formatStack() []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "goroutine %d [running]:\nruntime/debug.Stack()\n\truntime/debug/stack.go:0 +0x0\n", vm.goroutineID)
	builder.WriteString(formatFrames(vm.interpretedFrames()))
	return []byte(builder.String())
}

// beginPanicFrames records the stack a panic starts from, for the report an uncaught
// panic prints; a panic raised while another unwinds keeps the first stack.
func (vm *VM) beginPanicFrames() {
	if !vm.panicking {
		vm.panicFrames = vm.interpretedFrames()
	}
}

// printStack answers runtime/debug.PrintStack, writing to the VM's stderr stream under
// the output budget print statements share.
func (vm *VM) printStack() {
	_, _ = vm.limitedStderr().Write(vm.formatStack())
}

// frameInstructionIndex returns the index of the instruction a frame is executing.
//
// When the frame ran off the end of its body the dispatcher moves the counter one past
// the body, so the closing brace's position is reported as Go does.
//
// Takes frame (*CallFrame) which is the frame.
//
// Returns int which is the instruction index.
func frameInstructionIndex(frame *CallFrame) int {
	body := frame.Function.Body
	if frame.ProgramCounter > len(body) {
		return len(body)
	}
	return max(frame.ProgramCounter-1, 0)
}

// nearestSourcePosition returns the source position of an instruction, falling back to
// the closest earlier instruction that has one, since prologue and glue instructions
// carry none.
//
// Takes function (*program.CompiledFunction) which owns the source map.
// Takes pc (int) which is the instruction index.
//
// Returns string which is the file, empty when the function has no source map.
// Returns int which is the line, 0 when unknown.
func nearestSourcePosition(function *program.CompiledFunction, pc int) (string, int) {
	if function.DebugSourceMap == nil {
		return "", 0
	}
	if pc >= len(function.Body) {
		if file, line := function.DebugSourceMap.EpiloguePosition(); line > 0 {
			return file, line
		}
		pc = len(function.Body) - 1
	}
	for candidate := pc; candidate >= 0; candidate-- {
		file, line, _ := function.DebugSourceMap.SourcePosition(candidate)
		if line > 0 {
			if file == "" {
				file = unknownFile
			}
			return file, line
		}
	}
	return "", 0
}

// dispatchCallStackIntrinsic answers runtime.Caller, runtime.Callers,
// runtime.CallersFrames, runtime.FuncForPC and runtime.Stack, debug.Stack and
// debug.PrintStack, and the *runtime.Func methods, from the interpreter's frames.
//
// Takes vm (*VM) which owns the frames.
// Takes registers (*Registers) which receives the results.
// Takes site (*CallSite) which describes the result locations.
// Takes reflectedFunction (reflect.Value) which is the resolved native function.
// Takes arguments ([]reflect.Value) which are the coerced operands.
//
// Returns opContinue and true when the call was consumed.
func dispatchCallStackIntrinsic(vm *VM, registers *Registers, site *program.CallSite, reflectedFunction reflect.Value, arguments []reflect.Value) (OpResult, bool) {
	pointer := reflectedFunction.Pointer()
	if pointer == 0 {
		return opContinue, false
	}
	results, ok := vm.callStackIntrinsicResults(pointer, arguments)
	if !ok {
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, results)
	return opContinue, true
}

// formatFrames renders frames as Go's traceback does: a name line and an indented
// file:line line per frame.
//
// Takes frames ([]stackFrame) which are the frames, innermost first.
//
// Returns string which is the rendered frames.
func formatFrames(frames []stackFrame) string {
	var builder strings.Builder
	for _, frame := range frames {
		arguments := "(...)"
		if frame.function.function != nil && len(frame.function.function.ParameterKinds) == 0 {
			arguments = "()"
		}
		file := frame.file
		if file == "" {
			file = "?"
		}
		fmt.Fprintf(&builder, "%s%s\n\t%s:%d +0x%x\n", frame.function.name, arguments, file, frame.line, frame.pc)
	}
	return builder.String()
}

// tryInterceptRuntimeFuncMethod answers Name, Entry and FileLine on a *runtime.Func the
// interpreter handed out, which must never reach the real methods: the handle points at a
// registry entry, not at runtime function metadata. A nil handle answers as Go's methods
// do for a nil *runtime.Func.
//
// Takes vm (*VM) which owns the registry.
// Takes registers (*Registers) which receives the results.
// Takes site (*CallSite) which describes the argument and result locations.
// Takes receiver (reflect.Value) which is the method receiver.
// Takes methodName (string) which is the method being called.
//
// Returns opContinue and true when the call was answered.
func tryInterceptRuntimeFuncMethod(vm *VM, registers *Registers, site *program.CallSite, receiver reflect.Value, methodName string) (OpResult, bool) {
	if !receiver.IsValid() || receiver.Type() != runtimeFuncPointerType {
		return opContinue, false
	}
	entry := vm.Globals.callStack.fromHandle(receiver)
	var results []reflect.Value
	switch methodName {
	case "Name":
		results = []reflect.Value{reflect.ValueOf(entry.Name())}
	case "Entry":
		results = []reflect.Value{reflect.ValueOf(entry.Entry())}
	case "FileLine":
		if len(site.Arguments) < 2 {
			return opContinue, false
		}
		argument := site.Arguments[1]
		pc := registerToReflectValue(vm.Arena, registers, argument.Kind, argument.Register)
		file, line := entry.FileLine(uintptr(pc.Uint()))
		results = []reflect.Value{reflect.ValueOf(file), reflect.ValueOf(line)}
	default:
		return opContinue, false
	}
	storeReflectResults(registers, site.Returns, results)
	return opContinue, true
}

// isCallStackIntrinsic reports whether a native function value is one the interpreter
// answers itself, so call sites never cache a typed fast path for it.
//
// Takes function (any) which is the Go function value a call site resolved.
//
// Returns bool which is true for the runtime and runtime/debug stack inspection functions
// and the *runtime.Func methods.
func isCallStackIntrinsic(function any) bool {
	value := reflect.ValueOf(function)
	if !value.IsValid() || value.Kind() != reflect.Func {
		return false
	}
	switch value.Pointer() {
	case runtimeCallerPointer, runtimeCallersPointer, runtimeCallersFramesPointer, runtimeFuncForPCPointer,
		runtimeStackPointer, debugStackPointer, debugPrintStackPointer,
		runtimeFuncNamePointer, runtimeFuncEntryPointer, runtimeFuncFileLinePointer:
		return true
	default:
		return false
	}
}

// boundRuntimeFuncMethod binds Name, Entry or FileLine of a *runtime.Func handle to the
// registry entry behind it, so a method value or a by-name call never reaches the real
// methods. A nil handle binds the methods of a nil entry, which answer as Go's do.
//
// Takes vm (*VM) which owns the registry.
// Takes receiver (reflect.Value) which is the method receiver.
// Takes methodName (string) which is the method being bound.
//
// Returns reflect.Value which is the bound method.
// Returns bool which is true when the receiver is a handle and the method is one of the
// three.
func boundRuntimeFuncMethod(vm *VM, receiver reflect.Value, methodName string) (reflect.Value, bool) {
	if !receiver.IsValid() || receiver.Type() != runtimeFuncPointerType {
		return reflect.Value{}, false
	}
	entry := vm.Globals.callStack.fromHandle(receiver)
	switch methodName {
	case "Name":
		return reflect.ValueOf(entry.Name), true
	case "Entry":
		return reflect.ValueOf(entry.Entry), true
	case "FileLine":
		return reflect.ValueOf(entry.FileLine), true
	default:
		return reflect.Value{}, false
	}
}
