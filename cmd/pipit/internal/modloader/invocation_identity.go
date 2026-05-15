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

package modloader

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

const (
	// maxInvocationArguments is the ceiling on axes or script arguments in an invocation
	// identity.
	maxInvocationArguments = 4096

	// maxInvocationBytes is the total byte budget for all invocation metadata fields
	// combined.
	maxInvocationBytes = 1 << 20

	// invocationContextFields is the number of fixed host-context fields prepended to the
	// identity.
	invocationContextFields = 4
)

// InvocationIdentity binds source, gates, invocation and captured host context. It does
// not authenticate external dependencies, environment values or native helpers.
//
// Takes target (string) which selects the local source file or directory.
// Takes axes ([]string) which holds the parsed capability gate axes.
// Takes entrypoint (string) which selects the function to execute.
// Takes arguments ([]string) which holds the script arguments in their original order.
//
// Returns string which is the canonical script path.
// Returns string which is the domain-separated SHA-256 approval identity.
// Returns error when invocation inputs or local source exceed bounded scan limits.
func InvocationIdentity(target string, axes []string, entrypoint string, arguments []string) (scriptPath, approvalHash string, err error) {
	if err := validateInvocation(axes, entrypoint, arguments); err != nil {
		return "", "", err
	}
	scriptPath, sourceHash, err := ScriptIdentity(target)
	if err != nil {
		return "", "", err
	}
	identity, err := invocationIdentityHash(sourceHash, axes, entrypoint, arguments)
	return scriptPath, identity, err
}

// invocationIdentityHash captures host context before framing an approval identity.
//
// Takes sourceHash (string) which identifies the captured local source bytes.
// Takes axes ([]string) which holds the selected capability gate axes.
// Takes entrypoint (string) which selects the function to execute.
// Takes arguments ([]string) which holds the ordered script arguments.
//
// Returns string which is the domain-separated digest.
// Returns error when host context cannot be established.
func invocationIdentityHash(sourceHash string, axes []string, entrypoint string, arguments []string) (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	current, err := os.Stat(".")
	if err != nil {
		return "", err
	}
	named, err := os.Stat(directory)
	if err != nil {
		return "", err
	}
	if !os.SameFile(current, named) {
		return "", errors.New("modloader: working directory changed during approval capture")
	}
	principal, err := invocationPrincipal(current)
	if err != nil {
		return "", err
	}
	executable, err := invocationExecutable()
	if err != nil {
		return "", fmt.Errorf("modloader: measuring approval executable: %w", err)
	}
	hostContext := append([]string{runtime.GOOS, runtime.GOARCH, directory, executable}, principal...)
	if err := validateInvocation(hostContext, "", nil); err != nil {
		return "", err
	}
	return invocationHash(sourceHash, axes, entrypoint, arguments, hostContext), nil
}

// validateInvocation bounds invocation metadata before scanning source.
//
// Takes axes ([]string) which contains the selected capability axes.
// Takes entrypoint (string) which selects execution.
// Takes arguments ([]string) which holds ordered script arguments.
//
// Returns error when metadata exceeds its count or byte budget.
func validateInvocation(axes []string, entrypoint string, arguments []string) error {
	if len(axes) > maxInvocationArguments || len(arguments) > maxInvocationArguments {
		return errors.New("modloader: approval invocation exceeds argument count limit")
	}
	remaining := maxInvocationBytes
	for _, values := range [][]string{{entrypoint}, axes, arguments} {
		for _, value := range values {
			if len(value) > remaining {
				return errors.New("modloader: approval invocation exceeds byte limit")
			}
			remaining -= len(value)
		}
	}
	return nil
}

// invocationHash frames source and invocation inputs without ambiguous concatenation.
//
// Takes sourceHash (string) which identifies captured local source bytes.
// Takes axes ([]string) which holds the selected capability axes.
// Takes entrypoint (string) which selects execution.
// Takes arguments ([]string) which holds ordered script arguments.
// Takes hostContext ([]string) which identifies the captured host execution context.
//
// Returns string which is the domain-separated SHA-256 identity.
func invocationHash(sourceHash string, axes []string, entrypoint string, arguments, hostContext []string) string {
	canonicalAxes := slices.Clone(axes)
	slices.Sort(canonicalAxes)
	canonicalAxes = slices.Compact(canonicalAxes)
	digest := sha256.New()
	fmt.Fprintf(digest, "pipit-gated-invocation-v3\x00%d:%s%d:%s", len(sourceHash), sourceHash, len(entrypoint), entrypoint)
	for _, values := range [][]string{canonicalAxes, arguments, hostContext} {
		fmt.Fprintf(digest, "%d:", len(values))
		for _, value := range values {
			fmt.Fprintf(digest, "%d:%s", len(value), value)
		}
	}
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}
