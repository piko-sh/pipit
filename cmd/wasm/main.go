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

//go:build js && wasm

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	// browserSourceLimit is the maximum source size accepted per run.
	browserSourceLimit = 1 << 20

	// devExecTime caps wall-clock evaluation time in the default "dev" mode.
	devExecTime = 5 * time.Second

	// devMaxOutput caps captured stdout and stderr bytes in the default "dev" mode.
	devMaxOutput = 256 * 1024

	// devMaxGoroutines caps concurrent script goroutines in the default "dev" mode.
	devMaxGoroutines = 64

	// devMaxCallDepth caps the interpreter call-stack depth in the default "dev" mode.
	devMaxCallDepth = 2000

	// resultKeyOK names the result field reporting whether the operation succeeded.
	resultKeyOK = "ok"

	// resultKeyError names the result field carrying the failure message.
	resultKeyError = "error"

	// stderrFD is the file descriptor the WASM host writes script stderr through.
	stderrFD = 2

	// browserAssemblyLimit caps the bytes of pkasm returned by disassemble.
	browserAssemblyLimit = 1 << 20
)

// browserBusy prevents concurrent script evaluations.
var browserBusy atomic.Bool

// runCaps bundles the sandbox limits applied to a single run.
type runCaps struct {
	// execTime caps the wall-clock evaluation time.
	execTime time.Duration

	// maxOutput caps the captured stdout and stderr byte count.
	maxOutput int

	// maxGoroutines caps the concurrent script goroutines.
	maxGoroutines int32

	// maxCallDepth caps the interpreter call-stack depth.
	maxCallDepth int

	// costBudget caps the instruction-cost allowance; zero disables.
	costBudget int64
}

// options converts the caps to pipit interpreter options.
//
// Returns []pipit.Option which applies the configured limits.
func (caps runCaps) options() []pipit.Option {
	options := []pipit.Option{
		pipit.WithMaxExecutionTime(caps.execTime),
		pipit.WithMaxOutputSize(caps.maxOutput),
		pipit.WithMaxGoroutines(caps.maxGoroutines),
		pipit.WithMaxCallDepth(caps.maxCallDepth),
	}
	if caps.costBudget > 0 {
		options = append(options, pipit.WithCostBudget(caps.costBudget))
	}
	return options
}

// main registers the window.pipit JavaScript bindings and blocks forever so the Go
// runtime stays alive to service Promise callbacks.
func main() {
	fmt.Printf("[pipit WASM] ready (version %s) - call window.pipit.init()\n", pipit.Version)
	registerJSFunctions()
	select {}
}

// registerJSFunctions builds the window.pipit object and attaches every JS-callable entry
// point to it.
func registerJSFunctions() {
	api := js.Global().Get("Object").New()
	api.Set("init", js.FuncOf(jsInit))
	api.Set("run", js.FuncOf(jsRun))
	api.Set("format", js.FuncOf(jsFormat))
	api.Set("disassemble", js.FuncOf(jsDisassemble))
	api.Set("debugStart", js.FuncOf(jsDebugStart))
	api.Set("debugResume", js.FuncOf(jsDebugResume))
	api.Set("debugPause", js.FuncOf(jsDebugPause))
	api.Set("debugStop", js.FuncOf(jsDebugStop))
	api.Set("debugSetBreakpoints", js.FuncOf(jsDebugSetBreakpoints))
	api.Set("debugVariables", js.FuncOf(jsDebugVariables))
	api.Set("debugEvaluate", js.FuncOf(jsDebugEvaluate))
	js.Global().Set("pipit", api)
}

// jsInit reports that the module is loaded and returns the interpreter version.
//
// Takes no arguments.
//
// Returns any which is a Promise resolving to { ok: true, version: string }.
func jsInit(_ js.Value, _ []js.Value) any {
	return newPromise(func() any {
		return map[string]any{
			"ok":      true,
			"version": pipit.Version,
		}
	})
}

// jsRun compiles and executes a Go source string through the pipit interpreter, capturing
// its stdout and stderr.
//
// Takes arguments ([]js.Value) where arguments[0] is the source string and the optional
// arguments[1] is an options object with a "mode" field ("dev" | "untrusted").
//
// Returns any which is a Promise resolving to a run-result object (see runSource).
func jsRun(_ js.Value, arguments []js.Value) any {
	if !browserBusy.CompareAndSwap(false, true) {
		return newPromise(func() any { return map[string]any{resultKeyOK: false, resultKeyError: "browser interpreter is busy"} })
	}
	return newPromise(func() any {
		defer browserBusy.Store(false)
		source, mode, err := parseRunArgs(arguments)
		if err != nil {
			return map[string]any{resultKeyOK: false, resultKeyError: err.Error()}
		}
		return runSource(source, mode)
	})
}

// jsFormat gofmt-formats a Go source string.
//
// Takes arguments ([]js.Value) where arguments[0] is the source string.
//
// Returns any which is a Promise resolving to { ok: bool, formatted: string, error:
// string }.
func jsFormat(_ js.Value, arguments []js.Value) any {
	if !browserBusy.CompareAndSwap(false, true) {
		return newPromise(func() any { return map[string]any{resultKeyOK: false, resultKeyError: "browser interpreter is busy"} })
	}
	return newPromise(func() any {
		defer browserBusy.Store(false)
		if len(arguments) != 1 || arguments[0].Type() != js.TypeString {
			return map[string]any{resultKeyOK: false, resultKeyError: "format requires a source string"}
		}
		return formatSource(arguments[0].String())
	})
}

// jsDisassemble compiles a Go source string and renders the bytecode as pkasm.
//
// Takes arguments ([]js.Value) where arguments[0] is the source string.
//
// Returns any which is a Promise resolving to { ok: bool, assembly: string, truncated:
// bool, error: string }.
func jsDisassemble(_ js.Value, arguments []js.Value) any {
	if !browserBusy.CompareAndSwap(false, true) {
		return newPromise(func() any { return map[string]any{resultKeyOK: false, resultKeyError: "browser interpreter is busy"} })
	}
	return newPromise(func() any {
		defer browserBusy.Store(false)
		if len(arguments) != 1 || arguments[0].Type() != js.TypeString {
			return map[string]any{resultKeyOK: false, resultKeyError: "disassemble requires a source string"}
		}
		return disassembleSource(arguments[0].String())
	})
}

// parseRunArgs extracts the source string and execution mode from the JS run() arguments.
//
// Takes arguments ([]js.Value) which holds the raw JS arguments.
//
// Returns source (string) which is the source text to run.
// Returns mode (string) which is the execution mode, defaulting to dev.
// Returns err (error) for malformed arguments or an unknown mode.
func parseRunArgs(arguments []js.Value) (source, mode string, err error) {
	if len(arguments) < 1 || len(arguments) > 2 || arguments[0].Type() != js.TypeString {
		return "", "", errors.New("run requires a source string and optional mode object")
	}
	mode = "dev"
	if len(arguments) == 2 {
		if arguments[1].Type() != js.TypeObject || arguments[1].IsNull() {
			return "", "", errors.New("run options must be an object")
		}
		if requested := arguments[1].Get("mode"); requested.Type() != js.TypeUndefined {
			if requested.Type() != js.TypeString {
				return "", "", errors.New("run mode must be dev or untrusted")
			}
			mode = requested.String()
		}
	}
	if mode != "dev" && mode != "untrusted" {
		return "", "", errors.New("unknown browser execution mode")
	}
	return arguments[0].String(), mode, nil
}

// developerCaps returns cooperative limits for explicitly trusted browser code.
//
// Returns runCaps which holds the limits for that mode.
func developerCaps() runCaps {
	return runCaps{
		execTime:      devExecTime,
		maxOutput:     devMaxOutput,
		maxGoroutines: devMaxGoroutines,
		maxCallDepth:  devMaxCallDepth,
		costBudget:    0,
	}
}

// runSource creates a fresh interpreter, runs the "main" entrypoint of source with output
// capture and a timeout, and returns a JS-marshalable result map.
//
// Takes source (string) which is the Go source to interpret.
// Takes mode (string) which selects the sandbox profile.
//
// Returns map[string]any carrying ok, stdout, stderr, result, error, durationMs, and
// truncated fields.
func runSource(source, mode string) (result map[string]any) {
	result = map[string]any{
		resultKeyOK:    false,
		"stdout":       "",
		"stderr":       "",
		"result":       "",
		resultKeyError: "",
		"durationMs":   float64(0),
		"truncated":    false,
	}

	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			result[resultKeyOK] = false
			result[resultKeyError] = fmt.Sprintf("panic in pipit.run: %v", recovered)
		}
		result["durationMs"] = float64(time.Since(start).Microseconds()) / 1000.0
	}()

	if len(source) > browserSourceLimit {
		result[resultKeyError] = "browser source exceeds 1 MiB"
		return result
	}
	if mode == "untrusted" {
		return runRestrictedSource(source, result)
	}
	if mode != "dev" {
		result[resultKeyError] = "unknown browser execution mode"
		return result
	}
	caps := developerCaps()
	interpreter := pipit.NewInterpreter(append(caps.options(), stdlib.WithStandardLibrary())...)

	ctx, cancel := context.WithTimeout(context.Background(), caps.execTime)
	defer cancel()

	var evalResult any
	var evalErr error
	stdoutBytes, stderrBytes, capturedTruncated := captureOutput(caps.maxOutput, func() {
		evalResult, evalErr = interpreter.EvalFile(ctx, source, "main")
	})

	stdout, stdoutTruncated := truncateOutput(stdoutBytes, caps.maxOutput)
	stderr, stderrTruncated := truncateOutput(stderrBytes, caps.maxOutput)
	result["stdout"] = stdout
	result["stderr"] = stderr
	result["truncated"] = capturedTruncated || stdoutTruncated || stderrTruncated

	if evalErr != nil {
		result[resultKeyError] = evalErr.Error()
	} else {
		result[resultKeyOK] = true
	}
	if evalResult != nil {
		result["result"] = fmt.Sprintf("%v", evalResult)
	}
	return result
}

// runRestrictedSource uses the same reviewed policy as restricted native source
// execution. It does not establish a hard browser memory boundary.
//
// Takes source (string) which contains a complete Go file.
// Takes result (map[string]any) which receives bounded output and scalar JSON.
//
// Returns map[string]any containing the evaluation outcome.
func runRestrictedSource(source string, result map[string]any) map[string]any {
	var config pipit.RestrictedConfig
	config.Imports = []string{"math"}
	interpreter, err := pipit.NewRestrictedInterpreter(config, stdlib.Providers()...)
	if err != nil {
		result[resultKeyError] = err.Error()
		return result
	}
	value, err := interpreter.EvalFile(context.Background(), source, "main")
	result["stdout"] = value.Output
	result["truncated"] = value.OutputTruncated
	if len(value.Value) != 0 && string(value.Value) != "null" {
		result["result"] = string(value.Value)
	}
	if err != nil {
		result[resultKeyError] = err.Error()
	} else {
		result[resultKeyOK] = true
	}
	return result
}

// formatSource gofmt-formats source and returns a JS-marshalable result map.
//
// Takes source (string) which is the Go source to format.
//
// Returns map[string]any with fields ok (bool), formatted (string), and error (string).
func formatSource(source string) (result map[string]any) {
	result = map[string]any{
		resultKeyOK:    false,
		"formatted":    "",
		resultKeyError: "",
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result[resultKeyOK] = false
			result[resultKeyError] = fmt.Sprintf("panic in pipit.format: %v", recovered)
		}
	}()

	if len(source) > browserSourceLimit {
		result[resultKeyError] = "browser source exceeds 1 MiB"
		return result
	}
	formatted, err := pipit.FormatGoSource([]byte(source))
	if err != nil {
		result[resultKeyError] = err.Error()
		return result
	}
	result[resultKeyOK] = true
	result["formatted"] = string(formatted)
	return result
}

// disassembleSource compiles source as "main.go" with the dev-mode configuration and
// returns its pkasm listing as a JS-marshalable result map. Debug info adds source line
// annotations without changing the generated bytecode.
//
// Takes source (string) which is the Go source to compile.
//
// Returns map[string]any with fields ok (bool), assembly (string), truncated (bool), and
// error (string).
func disassembleSource(source string) (result map[string]any) {
	result = map[string]any{
		resultKeyOK:    false,
		"assembly":     "",
		"truncated":    false,
		resultKeyError: "",
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result[resultKeyOK] = false
			result["assembly"] = ""
			result["truncated"] = false
			result[resultKeyError] = fmt.Sprintf("panic in pipit.disassemble: %v", recovered)
		}
	}()

	if len(source) > browserSourceLimit {
		result[resultKeyError] = "browser source exceeds 1 MiB"
		return result
	}
	caps := developerCaps()
	interpreter := pipit.NewInterpreter(append(caps.options(), stdlib.WithStandardLibrary(), pipit.WithDebugInfo())...)
	ctx, cancel := context.WithTimeout(context.Background(), caps.execTime)
	defer cancel()
	cfs, err := interpreter.CompileFileSet(ctx, map[string]string{"main.go": source})
	if err != nil {
		result[resultKeyError] = err.Error()
		return result
	}
	assembly, truncated := clipListing(pipit.DisassembleAssembly(cfs), browserAssemblyLimit)
	result[resultKeyOK] = true
	result["assembly"] = assembly
	result["truncated"] = truncated
	return result
}

// clipListing clamps a listing to the byte cap at a line boundary.
//
// Takes listing (string) which is the text to clamp.
// Takes limit (int) which is the maximum bytes to keep.
//
// Returns string which is the (possibly clipped) listing.
// Returns bool which is true when lines were dropped.
func clipListing(listing string, limit int) (string, bool) {
	if len(listing) <= limit {
		return listing, false
	}
	return listing[:strings.LastIndexByte(listing[:limit], '\n')+1], true
}

// truncateOutput clamps captured output to the byte cap.
//
// Takes data ([]byte) which holds the captured output.
// Takes limit (int) which is the maximum bytes to keep (<= 0 means no limit).
//
// Returns string which is the (possibly clipped) output.
// Returns bool which is true when the output was clipped.
func truncateOutput(data []byte, limit int) (string, bool) {
	if limit > 0 && len(data) > limit {
		return string(data[:limit]), true
	}
	return string(data), false
}

// captureOutput runs the given function with the WASM host's stdout (fd 1) and stderr (fd
// 2) redirected into in-process buffers, then restores the originals.
//
// Takes limit (int) which bounds combined retained output before copying from JavaScript.
// Takes run (func()) which performs the work whose output should be captured.
//
// Returns []byte which is everything written to fd 1 (stdout).
// Returns []byte which is everything written to fd 2 (stderr).
// Returns bool which reports discarded output bytes.
func captureOutput(limit int, run func()) (stdout, stderr []byte, truncated bool) {
	capture := installCapture(limit)
	run()
	capture.restore()
	return capture.stdout.Bytes(), capture.stderr.Bytes(), capture.truncated
}

// outputCapture redirects the WASM host's stdout and stderr into bounded buffers until
// restored. A debug session keeps one installed across pauses and drains it
// incrementally.
type outputCapture struct {
	// release restores the host writers and frees the shims; nil once restored or when the
	// host has no fs object.
	release func()

	// stdout holds everything retained from fd 1.
	stdout bytes.Buffer

	// stderr holds everything retained from fd 2.
	stderr bytes.Buffer

	// limit bounds the combined retained bytes of stdout and stderr.
	limit int

	// stdoutTaken is how many stdout bytes take has already returned.
	stdoutTaken int

	// stderrTaken is how many stderr bytes take has already returned.
	stderrTaken int

	// mu guards the buffers against the shims and take running on different goroutines.
	mu sync.Mutex

	// truncated reports that output beyond limit was discarded.
	truncated bool
}

// installCapture swaps the host's fs.write and fs.writeSync for collecting shims.
//
// Takes limit (int) which bounds combined retained output.
//
// Returns *outputCapture which collects output until restore is called.
func installCapture(limit int) *outputCapture {
	capture := &outputCapture{
		release:     nil,
		stdout:      bytes.Buffer{},
		stderr:      bytes.Buffer{},
		limit:       limit,
		stdoutTaken: 0,
		stderrTaken: 0,
		mu:          sync.Mutex{},
		truncated:   false,
	}
	fs := js.Global().Get("fs")
	if fs.Type() != js.TypeObject {
		return capture
	}

	originalWrite := fs.Get("write")
	originalWriteSync := fs.Get("writeSync")

	writeSyncShim := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
		if len(arguments) < 2 {
			return 0
		}
		return capture.collect(arguments[0].Int(), arguments[1])
	})

	writeShim := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
		if len(arguments) < 6 {
			return nil
		}
		buffer := arguments[1].Call("subarray", arguments[2].Int(), arguments[2].Int()+arguments[3].Int())
		written := capture.collect(arguments[0].Int(), buffer)
		arguments[5].Invoke(js.Null(), written)
		return nil
	})

	fs.Set("writeSync", writeSyncShim)
	fs.Set("write", writeShim)
	capture.release = func() {
		fs.Set("write", originalWrite)
		fs.Set("writeSync", originalWriteSync)
		writeShim.Release()
		writeSyncShim.Release()
	}
	return capture
}

// collect retains as much of one write as the combined limit allows.
//
// Takes fd (int) which is the destination file descriptor.
// Takes buffer (js.Value) which is the Uint8Array being written.
//
// Returns int which is the full length, so the writer never retries discarded bytes.
//
// Concurrency: safe for concurrent use; acquires c.mu.
func (c *outputCapture) collect(fd int, buffer js.Value) int {
	length := buffer.Get("length").Int()
	if length <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	retained := min(length, max(0, c.limit-c.stdout.Len()-c.stderr.Len()))
	c.truncated = c.truncated || retained != length
	chunk := make([]byte, retained)
	js.CopyBytesToGo(chunk, buffer.Call("subarray", 0, retained))
	if fd == stderrFD {
		_, _ = c.stderr.Write(chunk)
	} else {
		_, _ = c.stdout.Write(chunk)
	}
	return length
}

// take returns the output retained since the previous take.
//
// Returns stdout (string) which is the new fd 1 output.
// Returns stderr (string) which is the new fd 2 output.
// Returns truncated (bool) which reports that any output so far was discarded.
//
// Concurrency: safe for concurrent use; acquires c.mu.
func (c *outputCapture) take() (stdout, stderr string, truncated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	stdout = string(c.stdout.Bytes()[c.stdoutTaken:])
	stderr = string(c.stderr.Bytes()[c.stderrTaken:])
	c.stdoutTaken = c.stdout.Len()
	c.stderrTaken = c.stderr.Len()
	return stdout, stderr, c.truncated
}

// restore puts the host writers back. Safe to call more than once.
func (c *outputCapture) restore() {
	if c.release != nil {
		c.release()
		c.release = nil
	}
}

// newPromise wraps operation in a JavaScript Promise run on its own goroutine.
//
// Takes operation (func() any) which produces the value to resolve with.
//
// Returns js.Value which is the pending Promise.
func newPromise(operation func() any) js.Value {
	var handler js.Func
	handler = js.FuncOf(func(_ js.Value, arguments []js.Value) any {
		resolve := arguments[0]
		reject := arguments[1]

		go func() {
			defer handler.Release()
			defer func() {
				if recovered := recover(); recovered != nil {
					reject.Invoke(fmt.Sprintf("panic in pipit WASM handler: %v", recovered))
				}
			}()
			resolve.Invoke(js.ValueOf(operation()))
		}()

		return nil
	})

	return js.Global().Get("Promise").New(handler)
}
