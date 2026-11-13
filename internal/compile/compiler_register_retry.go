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

package compile

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"reflect"
	"slices"

	"piko.sh/safeconv"

	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
)

const (
	// maxRegisterAttempts bounds how many times one body is compiled while it runs out of
	// registers.
	maxRegisterAttempts = 4

	// spillRetryMargin is how many registers beyond a statement's measured overshoot a retry
	// frees, covering a reload the lower spill limit adds.
	spillRetryMargin = 2
)

// registerRetryCheck enables PIPIT_REGISTER_RETRY_CHECK=1 verification that recompiling a
// discarded body produces identical bytecode.
var registerRetryCheck = os.Getenv("PIPIT_REGISTER_RETRY_CHECK") != ""

// registerSettings are the register-allocation choices one attempt at a body compiles
// with.
type registerSettings struct {
	// spillLimits lowers the spill threshold per bank where active.
	spillLimits [isa.NumRegisterKinds]scope.LoweredLimit

	// compactArguments releases temporaries beneath call arguments by moving the arguments
	// down.
	compactArguments bool
}

// registerPressure is what one attempt at a body learns about its registers, from which
// the next attempt's settings are derived.
type registerPressure struct {
	// overflowSites counts, per bank, the innermost statements that overflowed.
	overflowSites [isa.NumRegisterKinds]int

	// floor is, per bank, the first register after the parameters and named results; a lower
	// spill limit cannot free registers below it.
	floor [isa.NumRegisterKinds]uint32

	// limits is, per bank, the spill limit that would have let every overflowing statement
	// fit; inactive when none overflowed.
	limits [isa.NumRegisterKinds]scope.LoweredLimit

	// settings are the attempt's settings.
	settings registerSettings

	// infeasible marks the banks no lower spill limit can fix.
	infeasible [isa.NumRegisterKinds]bool

	// compactionWouldHelp records that some call argument left temporaries below its value
	// while compaction was off.
	compactionWouldHelp bool
}

// bodyAttempt compiles one attempt at a body with settings, up to but excluding
// finishBody.
type bodyAttempt func(settings registerSettings) (*Compiler, error)

// retryUnit is a body compiled with retries.
type retryUnit struct {
	// function receives the body.
	function *program.CompiledFunction

	// node identifies the function declaration, literal or range-over-func body being
	// compiled.
	node ast.Node

	// substitutions are the type arguments the body is compiled with; nil outside a generic
	// specialisation.
	substitutions map[*types.TypeParam]types.Type
}

// keptSettingsKey identifies a body by its syntax and type arguments across enclosing
// compilation attempts.
type keptSettingsKey struct {
	// node is the body's syntax.
	node ast.Node

	// substitutions identifies the type-argument map, zero outside a specialisation.
	substitutions uintptr
}

// applyRegisterSettings starts measuring an attempt and applies its settings. Call it
// once the parameters and named results are declared.
//
// Takes settings (registerSettings) which are the attempt's settings.
func (c *Compiler) applyRegisterSettings(settings registerSettings) {
	c.pressure = registerPressure{
		settings:            settings,
		floor:               c.Scopes.Alloc.Snapshot(),
		overflowSites:       [isa.NumRegisterKinds]int{},
		limits:              [isa.NumRegisterKinds]scope.LoweredLimit{},
		infeasible:          [isa.NumRegisterKinds]bool{},
		compactionWouldHelp: false,
	}
	c.Scopes.Alloc.SetSpillLimits(settings.spillLimits)
}

// notePressure records register demand for the innermost overflowing statement. Variables
// the statement reads cannot contribute freed registers because spilling them requires a
// reload.
//
// Takes usage (*scope.StatementUsage) which is how the statement used the registers.
// Takes sitesBefore ([isa.NumRegisterKinds]int) which is overflowSites when the statement
// began.
func (c *Compiler) notePressure(usage *scope.StatementUsage, sitesBefore [isa.NumRegisterKinds]int) {
	for kind := range isa.NumRegisterKinds {
		if usage.Peak[kind] <= scope.AddressableRegisters || c.pressure.overflowSites[kind] != sitesBefore[kind] {
			continue
		}
		c.pressure.overflowSites[kind]++
		bank := isa.RegisterKind(kind)
		if isa.IsTypedSliceKind(bank) {
			c.pressure.infeasible[kind] = true
			continue
		}
		limit, feasible := c.limitFreeing(usage, bank)
		if !feasible {
			c.pressure.infeasible[kind] = true
			continue
		}
		if current := c.pressure.limits[kind]; !current.Active || limit < current.Register {
			c.pressure.limits[kind] = scope.LoweredLimit{Register: limit, Active: true}
		}
	}
}

// limitFreeing finds the highest spill limit that frees enough registers of bank below
// where a statement began, counting only registers whose variables it does not read,
// since a variable the statement reads needs a register again once spilled.
//
// Takes usage (*scope.StatementUsage) which is how the statement used the registers.
// Takes bank (isa.RegisterKind) which is the bank that overflowed.
//
// Returns uint32 which is the limit.
// Returns bool which is false when even the lowest limit above the floor frees too few.
func (c *Compiler) limitFreeing(usage *scope.StatementUsage, bank isa.RegisterKind) (uint32, bool) {
	need := usage.Peak[bank] - scope.AddressableRegisters + spillRetryMargin
	limit := min(c.Scopes.Alloc.SpillLimit(bank), usage.Start[bank])
	freed := uint32(0)
	for limit > c.pressure.floor[bank] && freed < need {
		limit--
		if !usage.Reads.Contains(bank, limit) {
			freed++
		}
	}
	return limit, freed >= need
}

// nextRegisterSettings chooses whether to retry with argument compaction or lower spill
// limits for overflowing banks.
//
// Takes attempt (int) which is the number of the attempt that just finished, from 1.
//
// Returns registerSettings which are the next attempt's settings.
// Returns bool which is false when the body fits or no further change can make it fit.
func (c *Compiler) nextRegisterSettings(attempt int) (registerSettings, bool) {
	if !c.mayRetry(attempt) {
		return registerSettings{}, false
	}
	next := c.pressure.settings
	if !next.compactArguments && c.pressure.compactionWouldHelp {
		next.compactArguments = true
		return next, true
	}
	next.compactArguments = true
	lowered := false
	for kind, limit := range c.pressure.limits {
		if c.pressure.infeasible[kind] {
			return registerSettings{}, false
		}
		if limit.Active && limit.Register < c.Scopes.Alloc.SpillLimit(isa.RegisterKind(kind)) {
			next.spillLimits[kind] = limit
			lowered = true
		}
	}
	return next, lowered
}

// mayRetry reports whether an attempt remains and spilling can relieve the body's own
// register overflow.
//
// Takes attempt (int) which is the number of the attempt that just finished, from 1.
//
// Returns bool which is true when the body may be compiled again.
func (c *Compiler) mayRetry(attempt int) bool {
	return attempt < maxRegisterAttempts && c.stickyError == nil &&
		c.Scopes.Alloc.OverflowErr != nil && !c.Scopes.Alloc.RefusedSpill()
}

// compileBodyWithRetry compiles a function or specialisation body with retries; see
// retryBody.
//
// Takes unit (retryUnit) which is the body.
// Takes attempt (bodyAttempt) which compiles one attempt.
// Takes finish (func(*Compiler) error) which finishes the kept attempt.
//
// Returns error from the kept attempt, its finish, or cancellation.
func (c *Compiler) compileBodyWithRetry(ctx context.Context, unit retryUnit, attempt bodyAttempt, finish func(*Compiler) error) error {
	return c.retryBody(ctx, unit, attempt, finish)
}

// compileNestedBodyWithRetry compiles a closure or range-over-func body with retries; see
// retryBody. The functions the kept attempt created also belong to the enclosing body's
// attempt, so they are discarded with it.
//
// Takes unit (retryUnit) which is the body.
// Takes attempt (bodyAttempt) which compiles one attempt.
// Takes finish (func(*Compiler) error) which finishes the kept attempt.
//
// Returns error from the kept attempt, its finish, or cancellation.
func (c *Compiler) compileNestedBodyWithRetry(ctx context.Context, unit retryUnit, attempt bodyAttempt, finish func(*Compiler) error) error {
	return c.retryBody(ctx, unit, attempt, func(sub *Compiler) error {
		c.createdFunctions = append(c.createdFunctions, sub.createdFunctions...)
		return finish(sub)
	})
}

// retryBody recompiles a body with tighter register settings until it fits or no retry
// can help, then finishes the kept attempt.
//
// Retries restore body state and discard created closures. Successful nested settings are
// reused on later parent attempts to avoid multiplying retries with nesting depth.
//
// Takes unit (retryUnit) which is the body.
// Takes attempt (bodyAttempt) which compiles one attempt.
// Takes finish (func(*Compiler) error) which finishes the kept attempt.
//
// Returns error from the kept attempt, its finish, or cancellation.
func (c *Compiler) retryBody(ctx context.Context, unit retryUnit, attempt bodyAttempt, finish func(*Compiler) error) error {
	ctx, logger := logging.From(ctx)
	checkpoint := program.CaptureBodyCheckpoint(unit.function)
	key := keptSettingsKey{node: unit.node, substitutions: substitutionsIdentity(unit.substitutions)}
	settings, remembered := c.keptRegisterSettings[key]
	for attemptNumber := 1; ; attemptNumber++ {
		start := len(c.RootFunction.Functions)
		sub, err := attempt(settings)
		if err != nil {
			if sub == nil {
				return err
			}
			c.discardCreatedFunctions(sub.createdFunctions, start)
			return sub.withStickyCause(err)
		}
		next, retry := sub.retrySettings(attemptNumber, settings, remembered)
		if !retry {
			c.keptRegisterSettings[key] = settings
			return finish(sub)
		}
		c.discardCreatedFunctions(sub.createdFunctions, start)
		program.RestoreBodyCheckpoint(unit.function, &checkpoint)
		if err := ctx.Err(); err != nil {
			return err
		}
		logger.Debug("recompiling body under register pressure",
			"function", unit.function.Name, "attempt", attemptNumber+1,
			"compactArguments", next.compactArguments, "spillLimits", next.spillLimits)
		settings = next
	}
}

// retrySettings selects register settings for recompilation. PIPIT_REGISTER_RETRY_CHECK
// repeats successful initial compilations with unchanged settings, except bodies using
// remembered settings.
//
// Takes attemptNumber (int) which is the number of the attempt that just finished.
// Takes settings (registerSettings) which are that attempt's settings.
// Takes remembered (bool) which is true when the body started from remembered settings.
//
// Returns registerSettings which are the next attempt's settings.
// Returns bool which is false when the attempt is kept.
func (c *Compiler) retrySettings(attemptNumber int, settings registerSettings, remembered bool) (registerSettings, bool) {
	next, retry := c.nextRegisterSettings(attemptNumber)
	if !retry && registerRetryCheck && attemptNumber == 1 && !remembered {
		return settings, true
	}
	return next, retry
}

// registerCreatedFunction adds a closure or range-over-func body to the function table,
// reusing a slot a discarded attempt freed, and records it as created by this attempt.
//
// Takes function (*program.CompiledFunction) which is the new function.
//
// Returns uint16 which is its index in the function table.
// Returns error which wraps fault.ErrCompileFunctionTableFull when the table is full.
func (c *Compiler) registerCreatedFunction(function *program.CompiledFunction) (uint16, error) {
	root := c.RootFunction
	var index uint16
	if free := len(c.freeFunctionSlots); free > 0 {
		index = c.freeFunctionSlots[free-1]
		c.freeFunctionSlots = c.freeFunctionSlots[:free-1]
		root.Functions[index] = function
	} else {
		if len(root.Functions) >= maxFunctionIndex {
			return 0, fmt.Errorf("%w: %s needs more than %d", fault.ErrCompileFunctionTableFull, function.Name, maxFunctionIndex)
		}
		index = safeconv.MustIntToUint16(len(root.Functions))
		root.Functions = append(root.Functions, function)
	}
	c.createdFunctions = append(c.createdFunctions, index)
	return index, nil
}

// discardCreatedFunctions removes abandoned closures, preserving complete specialisations
// shared through the cache. Trailing slots are truncated no further than start, and
// remaining slots become reusable stand-ins.
//
// Takes created ([]uint16) which are the indices the attempt created.
// Takes start (int) which is the table's length when the attempt began.
func (c *Compiler) discardCreatedFunctions(created []uint16, start int) {
	if len(created) == 0 {
		return
	}
	root := c.RootFunction
	discarded := make(map[uint16]bool, len(created))
	for _, index := range created {
		discarded[index] = true
	}
	for len(root.Functions) > start && discarded[safeconv.MustIntToUint16(len(root.Functions)-1)] {
		root.Functions = root.Functions[:len(root.Functions)-1]
	}
	remaining := make([]uint16, 0, len(created))
	for _, index := range created {
		if int(index) < len(root.Functions) {
			root.Functions[index] = program.NewDiscardedFunction()
			remaining = append(remaining, index)
		}
	}
	slices.Sort(remaining)
	slices.Reverse(remaining)
	c.freeFunctionSlots = slices.DeleteFunc(append(c.freeFunctionSlots, remaining...), func(index uint16) bool {
		return int(index) >= len(root.Functions)
	})
}

// substitutionsIdentity returns a value that tells type-argument maps apart, so two
// specialisations of one body keep their settings apart.
//
// Takes substitutions (map[*types.TypeParam]types.Type) which may be nil.
//
// Returns uintptr which is zero for nil and the map's identity otherwise.
func substitutionsIdentity(substitutions map[*types.TypeParam]types.Type) uintptr {
	if substitutions == nil {
		return 0
	}
	return reflect.ValueOf(substitutions).Pointer()
}
