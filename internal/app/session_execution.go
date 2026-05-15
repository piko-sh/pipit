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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

// sessionExecution holds the compiled artefacts for one session submission.
type sessionExecution struct {
	// session is the owning session.
	session *Session

	// variableInit is the compiled variable initialiser, if any.
	variableInit *program.CompiledFunction

	// body is the compiled submission body.
	body *program.CompiledFunction

	// initIndices holds the package init function indices.
	initIndices []uint16

	// costUsed accumulates metered execution cost.
	costUsed int64
}

// run executes a compiled submission using one cost budget across every VM. Compilation
// and execution contexts may differ, but no script runs during compilation.
//
// Returns the final expression value and any execution or budget error.
func (execution *sessionExecution) run(ctx context.Context) (any, error) {
	session := execution.session
	service := session.service
	budget := service.limits.CostBudget
	defer func() {
		service.limits.CostBudget = budget
		service.lastCostUsed.Store(execution.costUsed)
	}()
	if execution.variableInit != nil {
		if _, err := execution.stage(ctx, budget, func() (any, error) {
			return nil, session.executeSessionVarInits(ctx, execution.variableInit)
		}); err != nil {
			return nil, err
		}
	}
	for _, index := range execution.initIndices {
		if session.executedInits[index] {
			continue
		}
		if _, err := execution.stage(ctx, budget, func() (any, error) {
			return nil, service.executeInitFunction(ctx, session.rootFunction, program.ExportFunctions(session.rootFunction)[index])
		}); err != nil {
			return nil, err
		}
		session.executedInits[index] = true
	}
	return execution.stage(ctx, budget, func() (any, error) {
		return service.executeCompiledEvalBody(ctx, execution.body)
	})
}

// stage deducts prior execution costs before entering another VM.
//
// Takes budget (int64) which is the remaining cost budget.
// Takes run (func() (any, error)) which executes the VM stage.
//
// Returns the stage result or an error without starting an exhausted stage.
func (execution *sessionExecution) stage(ctx context.Context, budget int64, run func() (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service := execution.session.service
	if budget > 0 {
		if execution.costUsed >= budget {
			return nil, fault.ErrCostBudgetExceeded
		}
		service.limits.CostBudget = budget - execution.costUsed
	}
	service.lastCostUsed.Store(0)
	result, err := run()
	execution.costUsed += service.LastCostUsed()
	return result, err
}
