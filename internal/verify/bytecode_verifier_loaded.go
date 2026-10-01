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

package verify

import (
	"context"
	"fmt"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

// VerifyLoadedFileSet runs the bytecode verifier and the operand bounds check over a file
// set decoded from packed bytes.
//
// Loaded bytecode is untrusted input, so this runs whenever bytecode is loaded, whatever
// the post-compile verification setting.
//
// Takes cfs (*program.CompiledFileSet) which was just decoded; nil passes.
//
// Returns an error wrapping fault.ErrBytecodeVerification, ErrRegisterOperandOutOfRange
// or ErrCallSiteOutOfRange when any reachable function fails, or nil.
func VerifyLoadedFileSet(ctx context.Context, cfs *program.CompiledFileSet) error {
	if cfs == nil {
		return nil
	}
	for _, root := range [...]*program.CompiledFunction{cfs.Root(), cfs.VariableInitFunction()} {
		if root == nil {
			continue
		}
		report, err := VerifyBytecode(ctx, root)
		if err != nil {
			return fmt.Errorf("verifying loaded bytecode: %w", err)
		}
		if report.HasErrors() {
			return fmt.Errorf("loaded %w:\n%w", fault.ErrBytecodeVerification, report.Err())
		}
		if err := VerifyOperandBounds(root); err != nil {
			return fmt.Errorf("loaded bytecode failed verification: %w", err)
		}
	}
	return nil
}
