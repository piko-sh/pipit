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
//	    use bounded JSON scalars. Run and format reject concurrent operations and
//	    source larger than 1 MiB. The parent must terminate its dedicated worker on
//	    timeout; these limits are not a hard browser memory boundary.
//
//	format(source: string) -> Promise<{ ok: boolean, formatted: string, error: string }>
//	    gofmt-formats the source via pipit.FormatGoSource.
//
// Output capture note: GOOS=js has no os.Pipe, so stdout and stderr are captured by
// overriding the global "fs" object's write and writeSync methods rather than by swapping
// os.Stdout/os.Stderr.
package main
