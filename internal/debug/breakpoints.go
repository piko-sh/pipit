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

package debug

import (
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
)

// Breakpoint describes one source breakpoint a client wants set.
type Breakpoint struct {
	// Condition is a boolean expression that must hold for the breakpoint to pause; empty
	// means unconditional.
	Condition string

	// HitCondition restricts which hits pause, in the forms "N", "> N", ">= N", "< N", "<=
	// N", "== N" and "% N"; empty means every hit.
	HitCondition string

	// Line is the 1-based source line.
	Line int
}

// FunctionBreakpoint describes a breakpoint on entry to a named function.
type FunctionBreakpoint struct {
	// Name is the function's runtime name (main.(*T).M, main.run.func1) or table name.
	Name string

	// Condition and HitCondition are as for Breakpoint.
	Condition string

	// HitCondition restricts which hits pause, as for Breakpoint.
	HitCondition string
}

// BreakpointResult reports how a requested breakpoint was installed.
type BreakpointResult struct {
	// Message explains why the breakpoint is not verified, or how it was adjusted.
	Message string

	// ID identifies the breakpoint in pause events until it is replaced or cleared.
	ID int

	// Line is the line the breakpoint was placed on.
	Line int

	// Verified is false when the breakpoint could not be installed as requested.
	Verified bool
}

// ExceptionFilter names a class of runtime events to pause on.
type ExceptionFilter string

const (
	// ExceptionFilterPanic pauses where an interpreted panic starts, before deferred
	// functions run and before recover can observe the value.
	ExceptionFilterPanic ExceptionFilter = "panic"
)

// SetBreakpoints replaces every breakpoint in file with the given set, as a DAP
// setBreakpoints request does. Safe to call at any time, before or during an execution.
//
// Takes file (string) which is the source file as compiled.
// Takes breakpoints ([]Breakpoint) which is the new set; empty clears the file.
//
// Returns []BreakpointResult with one entry per requested breakpoint, in order.
func (d *Debugger) SetBreakpoints(file string, breakpoints []Breakpoint) []BreakpointResult {
	d.session.ClearBreakpoints(file)
	results := make([]BreakpointResult, 0, len(breakpoints))
	for _, breakpoint := range breakpoints {
		results = append(results, d.installBreakpoint(file, breakpoint))
	}
	return results
}

// SetBreakpoint adds an unconditional breakpoint at file:line, keeping the file's other
// breakpoints.
//
// Takes file (string) which is the source file as compiled.
// Takes line (int) which is the 1-based line.
//
// Returns BreakpointResult which carries the breakpoint id.
func (d *Debugger) SetBreakpoint(file string, line int) BreakpointResult {
	return d.installBreakpoint(file, Breakpoint{Condition: "", HitCondition: "", Line: line})
}

// ClearBreakpoint removes the breakpoint at file:line, if any.
//
// Takes file (string) which is the source file.
// Takes line (int) which is the 1-based line.
func (d *Debugger) ClearBreakpoint(file string, line int) {
	d.session.ClearBreakpoint(file, line)
}

// SetFunctionBreakpoints replaces every function breakpoint with the given set.
//
// Takes breakpoints ([]FunctionBreakpoint) which is the new set; empty clears them.
//
// Returns []BreakpointResult with one entry per requested breakpoint, in order.
func (d *Debugger) SetFunctionBreakpoints(breakpoints []FunctionBreakpoint) []BreakpointResult {
	d.session.ClearFunctionBreakpoints()
	results := make([]BreakpointResult, 0, len(breakpoints))
	for _, breakpoint := range breakpoints {
		hit, err := program.ParseHitCondition(breakpoint.HitCondition)
		if err != nil {
			results = append(results, BreakpointResult{Message: err.Error(), ID: 0, Line: 0, Verified: false})
			continue
		}
		id := d.session.SetFunctionBreakpoint(breakpoint.Name, engine.DebugBreakpoint{Condition: breakpoint.Condition, HitCondition: hit, ID: 0, HitCount: 0})
		results = append(results, BreakpointResult{Message: "", ID: id, Line: 0, Verified: true})
	}
	return results
}

// SetExceptionBreakpoints selects which runtime events pause the program. Unknown filters
// are ignored.
//
// Takes filters ([]ExceptionFilter) which is the wanted set; empty disables them all.
func (d *Debugger) SetExceptionBreakpoints(filters []ExceptionFilter) {
	pauseOnPanic := false
	for _, filter := range filters {
		if filter == ExceptionFilterPanic {
			pauseOnPanic = true
		}
	}
	d.session.SetPauseOnPanic(pauseOnPanic)
}

// SetPauseOnEntry makes the next entrypoint pause at its first instruction.
//
// Takes enabled (bool) which arms or disarms the entry pause.
func (d *Debugger) SetPauseOnEntry(enabled bool) {
	d.session.SetStopOnEntry(enabled)
}

// installBreakpoint parses and installs one source breakpoint.
//
// Takes file (string) which is the source file as compiled.
// Takes breakpoint (Breakpoint) which describes the breakpoint.
//
// Returns BreakpointResult which reports the outcome.
func (d *Debugger) installBreakpoint(file string, breakpoint Breakpoint) BreakpointResult {
	hit, err := program.ParseHitCondition(breakpoint.HitCondition)
	if err != nil {
		return BreakpointResult{Message: err.Error(), ID: 0, Line: breakpoint.Line, Verified: false}
	}
	line, message, verified := d.resolveLine(file, breakpoint.Line)
	if !verified {
		return BreakpointResult{Message: message, ID: 0, Line: breakpoint.Line, Verified: false}
	}
	id := d.session.SetBreakpoint(file, line, engine.DebugBreakpoint{Condition: breakpoint.Condition, HitCondition: hit, ID: 0, HitCount: 0})
	return BreakpointResult{Message: message, ID: id, Line: line, Verified: true}
}

// resolveLine checks a breakpoint line against the bound program's source maps when a
// binding is available, moving it to the next line that carries an instruction.
//
// Takes file (string) which is the source file.
// Takes line (int) which is the requested 1-based line.
//
// Returns int which is the line to install on.
// Returns string which is a message for the client.
// Returns bool which is false when no instruction exists at or after the line.
func (d *Debugger) resolveLine(file string, line int) (int, string, bool) {
	binding := d.currentBinding()
	if binding == nil {
		return line, "", true
	}
	resolved, ok := binding.resolveLine(file, line)
	switch {
	case !ok:
		return line, "no code at this line", false
	case resolved != line:
		return resolved, "moved to the next line with code", true
	default:
		return line, "", true
	}
}
