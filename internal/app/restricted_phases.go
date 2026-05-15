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
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"pipit.sh/pipit/internal/fault"
)

// restrictedCompilationTimeout is the compilation-phase deadline.
const restrictedCompilationTimeout = 5 * time.Second

// EvaluateRestrictedPhased separates compilation from execution for the native worker.
// Its caller must enforce independent hard process deadlines around both phases.
//
// Takes interpreter (*RestrictedInterpreter) which owns immutable reviewed policy.
// Takes source (string) which is bounded before parsing.
// Takes entrypoint (string) which selects the function for complete-file execution.
// Takes file (bool) which selects complete-file rather than expression compilation.
// Takes beginExecution (func() error) which must obtain host permission before execution.
//
// Returns RestrictedResult containing only bounded output and a JSON scalar.
// Returns error on compilation, phase refusal, execution or serialisation failure.
func EvaluateRestrictedPhased(
	ctx context.Context, interpreter *RestrictedInterpreter, source, entrypoint string,
	file bool, beginExecution func() error,
) (RestrictedResult, error) {
	if ctx == nil || interpreter == nil || interpreter.active == nil || beginExecution == nil {
		return RestrictedResult{}, ErrInvalidRestrictedConfig
	}
	if err := ctx.Err(); err != nil {
		return RestrictedResult{}, err
	}
	select {
	case interpreter.active <- struct{}{}:
		defer func() { <-interpreter.active }()
	default:
		return RestrictedResult{}, ErrRestrictedBusy
	}
	config := interpreter.config
	if len(source) > config.MaxSourceBytes {
		return RestrictedResult{}, fmt.Errorf("%w: source", ErrRestrictedLimit)
	}
	output := &restrictedOutput{buffer: bytes.Buffer{}, remaining: config.MaxOutputBytes, truncated: false}
	service := interpreter.newService(output)
	service.config.maxExecutionTime = restrictedCompilationTimeout
	compileContext, cancelCompile := context.WithTimeout(ctx, restrictedCompilationTimeout)
	execute, compileErr := compileRestrictedPhase(compileContext, service, source, entrypoint, file)
	compileErr = errors.Join(compileErr, compileContext.Err())
	cancelCompile()
	if compileErr != nil {
		return RestrictedResult{}, compileErr
	}
	if err := beginExecution(); err != nil {
		return RestrictedResult{}, errors.Join(err, ctx.Err())
	}
	service.config.maxExecutionTime = config.Timeout
	executionContext, cancelExecution := context.WithTimeout(ctx, config.Timeout)
	defer cancelExecution()
	value, err := execute(executionContext)
	if interpreter.filesystem != nil {
		err = errors.Join(err, interpreter.filesystem.Err())
	}
	result := RestrictedResult{Value: nil, Output: output.String(), OutputTruncated: output.truncated, CostUsed: service.LastCostUsed()}
	if err != nil || executionContext.Err() != nil {
		return result, errors.Join(err, executionContext.Err())
	}
	result.Value, err = restrictedScalar(value, config.MaxReturnBytes)
	return result, err
}

// compileRestrictedPhase returns a private execution closure without running script code.
//
// Takes service (*Service) which is private to this single restricted submission.
// Takes source (string) which has passed the source-size limit.
// Takes entrypoint (string) which names the complete-file entrypoint.
// Takes file (bool) which selects the complete-file compilation path.
//
// Returns func(context.Context) (any, error) which remains private to the worker.
// Returns error when source compilation fails.
func compileRestrictedPhase(
	ctx context.Context, service *Service, source, entrypoint string, file bool,
) (func(context.Context) (any, error), error) {
	if file {
		compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": source})
		if err != nil {
			return nil, err
		}
		return func(executionContext context.Context) (any, error) {
			return service.ExecuteEntrypoint(executionContext, compiled, entrypoint)
		}, nil
	}
	compiled, err := service.Compile(ctx, source)
	if errors.Is(err, fault.ErrTypeCheck) {
		compiled, err = service.compileMixed(ctx, source)
	}
	if err != nil {
		return nil, err
	}
	return func(executionContext context.Context) (any, error) {
		return service.Execute(executionContext, compiled)
	}, nil
}
