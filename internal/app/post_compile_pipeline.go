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
	"fmt"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/verify"
)

// runPostCompilationChecks runs recursion detection, the compiler's program pipeline, and
// then bytecode verification.
//
// Takes root (*CompiledFunction) which is the top-level compiled function to validate.
// Takes wrapFmt (string) which is the format string used to wrap any error.
//
// Returns nil on success, otherwise the first failing check's error.
func (s *Service) runPostCompilationChecks(ctx context.Context, root *program.CompiledFunction, wrapFmt string) error {
	if !s.features.Has(policy.InterpFeatureRecursion) {
		if err := program.DetectRecursion(root); err != nil {
			return fmt.Errorf(wrapFmt, err)
		}
	}
	for _, step := range compile.ProgramPipeline(s.optimisations()) {
		if err := step.Run(ctx, root); err != nil {
			return fmt.Errorf(wrapFmt, err)
		}
	}
	return runBytecodeVerifier(ctx, s, root, wrapFmt)
}

// bytecodeVerificationEnabled reports whether the verifier should run for the given
// service. Verification is on by default and may be toggled via WithBytecodeVerification.
//
// Takes s (*Service) which carries the configured opt-out flag.
//
// Returns true when the verifier should be invoked after compilation.
func bytecodeVerificationEnabled(s *Service) bool {
	if s == nil || s.config == nil {
		return true
	}
	if s.config.bytecodeVerificationDisabled {
		return false
	}
	return true
}

// runBytecodeVerifier invokes the verifier when enabled, wrapping any reported violations
// in an error with the caller-supplied compilation stage prefix. Service-layer compile
// entry points call this after DetectRecursion to gate execution on a clean verifier
// report.
//
// Takes service (*Service) which carries the verification-enabled flag.
// Takes root (*CompiledFunction) which is the top-level compiled function to verify.
// Takes wrapFmt (string) which is the format string used to wrap any error with the
// calling compilation stage's prefix (e.g. the CompileFileSet or CompileProgram error
// format).
//
// Returns nil when verification is disabled or successful, and a formatted error
// otherwise.
func runBytecodeVerifier(ctx context.Context, service *Service, root *program.CompiledFunction, wrapFmt string) error {
	if !bytecodeVerificationEnabled(service) {
		return nil
	}
	report, err := verify.VerifyBytecode(ctx, root)
	if err != nil {
		return fmt.Errorf(wrapFmt, err)
	}
	if !report.HasErrors() {
		return nil
	}
	return fmt.Errorf(wrapFmt, fmt.Errorf("%w:\n%w", fault.ErrBytecodeVerification, report.Err()))
}
