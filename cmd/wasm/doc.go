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

// Command wasm is the WebAssembly entry point that runs Go source through the pipit
// interpreter in the browser. It powers the pipit web playground.
//
// The binary is built with GOOS=js GOARCH=wasm and loaded via the standard Go WASM
// runtime (wasm_exec.js). On start it registers a global window.pipit object and then
// blocks forever so the runtime stays alive to service Promise callbacks. Every method
// returns a JavaScript Promise that resolves to a plain object (never a rejected Promise
// on ordinary evaluation errors: those are reported in the resolved object's error
// field).
//
// window.pipit API:
//
//	init() -> Promise<{ ok: true, version: string }>
//	    Reports that the module is loaded and returns the pipit interpreter version.
//
//	run(source: string, opts?: { mode?: "dev" | "untrusted" })
//	    -> Promise<{
//	         ok: boolean,          // true when evaluation returned no error
//	         stdout: string,       // captured fd 1 output (e.g. fmt.Println), truncated to cap
//	         stderr: string,       // captured fd 2 output, truncated to cap
//	         result: string,       // fmt "%v" of the main() return value, "" when nil
//	         error: string,        // evaluation or panic message, "" on success
//	         durationMs: number,   // wall-clock evaluation time in milliseconds
//	         truncated: boolean,   // true when stdout or stderr was clipped to the cap
//	       }>
//	    Creates a fresh interpreter per call and runs the "main" entrypoint. "dev" (the
//	    default) is for trusted code with cooperative limits. "untrusted" uses the
//	    shared RestrictedInterpreter policy, allowing only reviewed math helpers and
//	    builtin print/println. It denies goroutines, channels and other imports.
//	    Unknown or malformed modes fail instead of selecting dev. Restricted results
//	    use bounded JSON scalars. Run, format and disassemble reject concurrent
//	    operations and source larger than 1 MiB. The parent must terminate its dedicated
//	    worker on timeout; these limits are not a hard browser memory boundary.
//
//	format(source: string) -> Promise<{ ok: boolean, formatted: string, error: string }>
//	    gofmt-formats the source via pipit.FormatGoSource.
//
//	disassemble(source: string)
//	    -> Promise<{ ok: boolean, assembly: string, truncated: boolean, error: string }>
//	    Compiles the source as "main.go" with the dev-mode configuration, without running
//	    it, and renders the bytecode as pkasm with source line annotations. The listing
//	    is clipped at a line boundary to 1 MiB; truncated reports the clip. It shares the
//	    busy flag and source limit with run and format.
//
// Debugging (trusted mode only). One debug session may exist at a time; it holds the same
// busy flag as run, format and disassemble, so those are refused until the session ends.
// The source compiles as "main.go" and breakpoint lines are 1-based. There is no
// execution deadline: the page must time each running stretch and terminate the worker
// when one runs too long. Pause only takes effect when the program blocks (a sleep, a
// channel): Go WASM is single-threaded, so a tight loop never yields to the event loop.
//
//	debugStart(source: string, { breakpoints: number[], stopOnEntry?: boolean })
//	    -> Promise<Snapshot>
//	    Compiles, sets breakpoints (verified against the compiled code), pauses on
//	    panics, and runs main until the first pause or exit.
//
//	debugResume(action: "continue" | "stepOver" | "stepIn" | "stepOut", threadID: number)
//	    -> Promise<Snapshot>
//	    threadID 0 means the thread that paused.
//
//	debugPause() -> Promise<{ ok: boolean, error: string }>
//	    The pending debugStart or debugResume promise resolves with the pause.
//
//	debugStop() -> Promise<Snapshot>
//	    Ends the execution and the session.
//
//	debugSetBreakpoints(lines: number[])
//	    -> Promise<{ ok, error, breakpoints: {line, verified, message}[] }>
//
//	debugVariables(threadID: number, frame: number, scope: "locals" | "closure" |
//	    "globals", ref: number, start?: number)
//	    -> Promise<{ ok, error, variables: Variable[], total: number }>
//	    A non-zero ref expands a value returned earlier in this pause instead. Rows come
//	    in pages of up to 500 from start (default 0); total is how many the listing
//	    holds, so the page can ask for the next one.
//
//	debugEvaluate(threadID: number, frame: number, expression: string)
//	    -> Promise<{ ok, error, value, type, ref }>
//	    Function calls are refused; evaluation is limited to one second.
//
//	Snapshot = { ok, error, state: "paused" | "exited", reason, line, function,
//	    threadID, hitBreakpoint, panic, result, stdout, stderr, truncated, durationMs,
//	    breakpoints: {line, verified, message}[], stack: {function, line}[],
//	    locals: Variable[], threads: {id, name, paused}[] }
//	    stdout and stderr hold only the output written since the previous reply;
//	    durationMs counts running time, not time spent paused. On exit, ok is false and
//	    error is set when the program failed.
//
//	Variable = { name, value, type, ref, children }
//	    Text fields are clipped to 1 KiB; listings to pages of 500 rows. ref is 0 for
//	    values with nothing to expand. A session left paused for 15 minutes is stopped.
//
// Output capture note: GOOS=js has no os.Pipe, so stdout and stderr are captured by
// overriding the global "fs" object's write and writeSync methods rather than by swapping
// os.Stdout/os.Stderr.
package main
