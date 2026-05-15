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
	fs := js.Global().Get("fs")
	if fs.Type() != js.TypeObject {
		run()
		return nil, nil, false
	}

	originalWrite := fs.Get("write")
	originalWriteSync := fs.Get("writeSync")

	var stdoutBuf, stderrBuf bytes.Buffer
	collect := func(fd int, buffer js.Value) int {
		length := buffer.Get("length").Int()
		if length <= 0 {
			return 0
		}
		retained := min(length, max(0, limit-stdoutBuf.Len()-stderrBuf.Len()))
		truncated = truncated || retained != length
		chunk := make([]byte, retained)
		js.CopyBytesToGo(chunk, buffer.Call("subarray", 0, retained))
		if fd == stderrFD {
			_, _ = stderrBuf.Write(chunk)
		} else {
			_, _ = stdoutBuf.Write(chunk)
		}
		return length
	}

	writeSyncShim := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
		if len(arguments) < 2 {
			return 0
		}
		return collect(arguments[0].Int(), arguments[1])
	})

	writeShim := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
		if len(arguments) < 6 {
			return nil
		}
		buffer := arguments[1].Call("subarray", arguments[2].Int(), arguments[2].Int()+arguments[3].Int())
		written := collect(arguments[0].Int(), buffer)
		arguments[5].Invoke(js.Null(), written)
		return nil
	})

	fs.Set("writeSync", writeSyncShim)
	fs.Set("write", writeShim)
	defer func() {
		fs.Set("write", originalWrite)
		fs.Set("writeSync", originalWriteSync)
		writeShim.Release()
		writeSyncShim.Release()
	}()

	run()
	return stdoutBuf.Bytes(), stderrBuf.Bytes(), truncated
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
