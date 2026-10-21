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

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/verify"
)

// errLicenceGrantsRejected reports a program whose arena grants the licence checker could
// not confirm (a granted store into an object that is not a fresh allocation the function
// still holds alone); CompileFileSet and CompileProgram compile it again without arena
// promotion (compileWithoutArenaPromotion).
var errLicenceGrantsRejected = errors.New("the licence checker rejected the program's grants")

// errLicenceCheckerDefect reports a defect of the engine or ISA spec on a service whose
// licence checker is strict (withLicenceCheckerStrict).
var errLicenceCheckerDefect = errors.New("the licence checker found an unclassified or ill-formed instruction")

// noArenaPromotionKey marks a compile context whose escape pass makes no grant.
type noArenaPromotionKey struct{}

// licenceCheckWalk is one run of the licence checker over a program's functions.
type licenceCheckWalk struct {
	// stopped is the context's cause or the checker's error that ended the walk, or nil.
	stopped error

	// seen holds the functions already visited.
	seen map[*program.CompiledFunction]bool

	// grantRejected is the first function whose grants the checker rejected, or nil.
	grantRejected *program.CompiledFunction

	// defect is the first function in which the checker found a defect, or nil.
	defect *program.CompiledFunction

	// functions holds every function visited, in visiting order.
	functions []*program.CompiledFunction

	// defectVerdict is the checker's verdict on defect.
	defectVerdict verify.CheckerVerdict
}

// licenceCheckerStrictDefault is the strictness a new service's licence checker starts
// with; SetLicenceCheckerStrictForTest sets it.
var licenceCheckerStrictDefault atomic.Bool

// licenceCheckerVerdictHook, when set (tests only), replaces the checker's verdict for a
// function, so the fallbacks can be exercised without a broken compiler or ISA spec.
var licenceCheckerVerdictHook func(ctx context.Context, f *program.CompiledFunction, verdict verify.CheckerVerdict) verify.CheckerVerdict

// compileWithoutArenaPromotion returns ctx marked so the compile it governs runs the
// escape pass without arena promotion, so the program has no grants.
//
// Returns context.Context which is the marked context.
func compileWithoutArenaPromotion(ctx context.Context) context.Context {
	return context.WithValue(ctx, noArenaPromotionKey{}, true)
}

// arenaPromotionOff reports whether ctx was marked by compileWithoutArenaPromotion.
//
// Returns bool which is true when the compile makes no grant.
func arenaPromotionOff(ctx context.Context) bool {
	return ctx != nil && ctx.Value(noArenaPromotionKey{}) != nil
}

// runLicenceChecker checks every function of a program with the per-program licence
// checker, which confirms that the escape pass's grants are correct and that every
// instruction declares its hand-offs.
//
// Takes root (*program.CompiledFunction) which is the program's root.
// Takes strict (bool) which is true when a defect fails the compile.
//
// Returns error which is errLicenceGrantsRejected, errLicenceCheckerDefect, the context's
// cause, or nil.
func runLicenceChecker(ctx context.Context, root *program.CompiledFunction, strict bool) error {
	if root == nil {
		return nil
	}
	walk := licenceCheckWalk{
		stopped:       nil,
		seen:          map[*program.CompiledFunction]bool{},
		grantRejected: nil,
		defect:        nil,
		functions:     nil,
		defectVerdict: verify.CheckerAccepted,
	}
	walk.visit(ctx, root)
	if walk.stopped != nil {
		return fmt.Errorf("checking licence grants: %w", walk.stopped)
	}
	switch {
	case walk.defect != nil && strict:
		engine.NoteLicenceCheck(walk.functions, false)
		return fmt.Errorf("%w: %s (%s)", errLicenceCheckerDefect, walk.defect.Name, walk.defectVerdict)
	case walk.defect != nil:
		_, logger := logging.From(ctx)
		logger.Warn("The licence checker found a defect; the program runs abandoned, without the minor collection",
			slog.String("function", walk.defect.Name), slog.String("verdict", walk.defectVerdict.String()))
		engine.NoteLicenceCheck(walk.functions, true)
		return nil
	case walk.grantRejected != nil:
		return fmt.Errorf("%w: %s", errLicenceGrantsRejected, walk.grantRejected.Name)
	}
	engine.NoteLicenceCheck(walk.functions, false)
	return nil
}

// visit checks function, its variable initialiser and its nested functions, once each,
// until the context ends or a check fails.
//
// Takes function (*program.CompiledFunction) which is the function, or nil.
func (w *licenceCheckWalk) visit(ctx context.Context, function *program.CompiledFunction) {
	if function == nil || w.seen[function] || w.stopped != nil {
		return
	}
	if w.stopped = context.Cause(ctx); w.stopped != nil {
		return
	}
	w.seen[function] = true
	w.functions = append(w.functions, function)
	if len(function.Body) > 0 {
		w.check(ctx, function)
		if w.stopped != nil {
			return
		}
	}
	w.visit(ctx, function.VariableInitFunction)
	for _, child := range function.Functions {
		w.visit(ctx, child)
	}
}

// check runs the checker over one function with a body and records its verdict.
//
// Takes function (*program.CompiledFunction) which is the function.
func (w *licenceCheckWalk) check(ctx context.Context, function *program.CompiledFunction) {
	verdict, err := verify.CheckLicence(ctx, function, !arenaPromotionOff(ctx))
	if err != nil {
		w.stopped = fmt.Errorf("licence check of %s: %w", function.Name, err)
		return
	}
	if licenceCheckerVerdictHook != nil {
		verdict = licenceCheckerVerdictHook(ctx, function, verdict)
	}
	if verdict != verify.CheckerAccepted {
		function.LocalContainerAllocPCs, function.LocalContainerStorePCs = nil, nil
	}
	switch {
	case verdict == verify.CheckerAccepted:
	case verdict == verify.CheckerRejectedInstruction || arenaPromotionOff(ctx):
		if w.defect == nil {
			w.defect, w.defectVerdict = function, verdict
		}
	default:
		if w.grantRejected == nil {
			w.grantRejected = function
		}
	}
}

// SetLicenceCheckerStrictForTest sets whether services created from now on fail a compile
// in which the licence checker finds an unclassified or ill-formed instruction, as
// withLicenceCheckerStrict does for one service. Pipit's own test binaries enable it in
// their TestMain.
//
// Takes enabled (bool) which is the new default.
func SetLicenceCheckerStrictForTest(enabled bool) {
	licenceCheckerStrictDefault.Store(enabled)
}
