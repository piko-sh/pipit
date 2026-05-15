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

package sandboxhost

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxworker"
)

// FilesystemRights selects independent operations on one host-approved root.
type FilesystemRights = sandboxbroker.Rights

const (
	// maximumFilesystemRoots is the upper bound on named roots per config.
	maximumFilesystemRoots = 64

	// maximumFilesystemHostPath is the upper bound on host path length in bytes.
	maximumFilesystemHostPath = 4096

	// FilesystemRead permits bounded regular-file reads.
	FilesystemRead FilesystemRights = sandboxbroker.Read

	// FilesystemWrite permits staged replacement, not arbitrary native file access.
	FilesystemWrite FilesystemRights = sandboxbroker.Write

	// FilesystemList permits bounded directory names independently of file reads.
	FilesystemList FilesystemRights = sandboxbroker.List
)

// FilesystemLimits selects finite cumulative broker quotas. Zero selects defaults;
// positive values may only reduce them.
type FilesystemLimits = sandboxbroker.FilesystemLimits

// FilesystemRoot binds an opaque script-visible name to a trusted absolute host path.
// Root preparation synchronously accesses trusted storage.
type FilesystemRoot struct {
	// Name is the opaque script-visible root name.
	Name string

	// HostPath is the trusted absolute host path for this root.
	HostPath string

	// Rights holds the approved operations for this root.
	Rights FilesystemRights
}

// IsolatedFilesystemConfig opts into experimental, separately confined filesystem I/O
// alongside a source worker.
type IsolatedFilesystemConfig struct {
	// BrokerPath is the absolute path to the approved broker executable.
	BrokerPath string

	// Roots holds the host-approved filesystem root grants.
	Roots []FilesystemRoot

	// Worker holds the source worker configuration.
	Worker IsolatedConfig

	// Limits selects the broker's cumulative quotas.
	Limits FilesystemLimits

	// BrokerSHA256 approves exact broker executable bytes.
	BrokerSHA256 [sha256.Size]byte
}

// IsolatedFilesystemWorker owns one source worker and one separate filesystem broker. It
// accepts one submission only.
type IsolatedFilesystemWorker struct {
	// process is the combined source and broker boundary.
	process isolatedFilesystemProcess

	// used tracks whether the single submission has been consumed.
	used atomic.Bool
}

// NewIsolatedFilesystemWorker launches independently approved source and broker images.
//
// Takes config (IsolatedFilesystemConfig) which contains explicit capabilities.
//
// Returns an owner, possibly alongside an error after partial startup.
// Returns ErrIsolatedUnavailable when the native boundary cannot be enforced.
func NewIsolatedFilesystemWorker(ctx context.Context, config IsolatedFilesystemConfig) (*IsolatedFilesystemWorker, error) {
	if err := validateIsolatedFilesystemConfig(ctx, &config); err != nil {
		return nil, err
	}
	ctx = isolatedLoggerContext(ctx, config.Worker.Logger)
	process, err := newIsolatedFilesystemProcess(ctx, &config)
	if process == nil {
		return nil, err
	}
	if err != nil {
		return nil, cleanupOrFail(err, process.Close)
	}
	return &IsolatedFilesystemWorker{process: process, used: atomic.Bool{}}, nil
}

// Eval runs one expression or statement submission with the fixed pipit/fs manifest.
//
// Takes source bytes, never a host path.
//
// Returns bounded output and a scalar only after both process owners are cleaned up.
func (worker *IsolatedFilesystemWorker) Eval(source string) (RestrictedResult, error) {
	return worker.evaluate(sandboxworker.Request{Kind: "expression", Source: source, Entrypoint: ""})
}

// EvalFile runs one complete source file and its named entrypoint.
//
// Takes source (string) which is the source bytes.
// Takes entrypoint (string) which is the named entrypoint, not a host filename.
//
// Returns bounded output and a scalar, or an execution or confinement error.
func (worker *IsolatedFilesystemWorker) EvalFile(source, entrypoint string) (RestrictedResult, error) {
	return worker.evaluate(sandboxworker.Request{Kind: "file", Source: source, Entrypoint: entrypoint})
}

// Close permanently prevents submissions and closes both process owners. Resource cleanup
// may be retried without making the worker reusable.
//
// Returns execution or cleanup errors.
func (worker *IsolatedFilesystemWorker) Close() error {
	if worker == nil || worker.process == nil {
		return nil
	}
	worker.used.Store(true)
	return worker.process.Close()
}

// Diagnostics returns bounded, untrusted source-worker stdout and stderr.
//
// Returns an empty string if source startup did not create a process.
func (worker *IsolatedFilesystemWorker) Diagnostics() string {
	if worker == nil || worker.process == nil {
		return ""
	}
	return worker.process.Diagnostics()
}

// evaluate admits exactly one source submission without retaining queued work.
//
// Takes request (sandboxworker.Request) which is the fixed source request.
//
// Returns no result after a protocol, confinement or cleanup failure.
func (worker *IsolatedFilesystemWorker) evaluate(request sandboxworker.Request) (RestrictedResult, error) {
	if worker == nil || worker.process == nil || !worker.used.CompareAndSwap(false, true) {
		return RestrictedResult{}, ErrIsolatedUsed
	}
	response, err := worker.process.evaluate(request)
	if err != nil {
		return RestrictedResult{}, errors.Join(err, worker.process.Close())
	}
	result := RestrictedResult{Value: response.Value, Output: response.Output, CostUsed: response.CostUsed, OutputTruncated: response.OutputTruncated}
	if response.Code == "evaluation_failed" {
		return result, &fault.EvaluationError{Tier: "isolated-filesystem", Message: response.Error}
	}
	return result, nil
}

// isolatedFilesystemProcess is the combined source and broker boundary.
type isolatedFilesystemProcess interface {
	// evaluate runs a single source submission through the boundary.
	//
	// Takes request (sandboxworker.Request) which is the source submission.
	//
	// Returns sandboxworker.Response and any execution error.
	evaluate(sandboxworker.Request) (sandboxworker.Response, error)

	// Close terminates both processes and releases native resources.
	//
	// Returns error when cleanup fails.
	Close() error

	// Diagnostics returns bounded, untrusted worker diagnostic text.
	//
	// Returns string containing diagnostic output.
	Diagnostics() string
}

// validateIsolatedFilesystemConfig copies capability policy before native startup.
//
// Takes config (*IsolatedFilesystemConfig) which is the private configuration copy.
//
// Returns an invalid-configuration error before accessing any grant root.
func validateIsolatedFilesystemConfig(ctx context.Context, config *IsolatedFilesystemConfig) error {
	if err := validateIsolatedConfig(ctx, &config.Worker); err != nil {
		return err
	}
	if config.BrokerSHA256 == ([sha256.Size]byte{}) || len(config.Roots) > maximumFilesystemRoots {
		return ErrInvalidIsolatedConfig
	}
	storage := []string{config.Worker.StateDirectory}
	if err := validateFilesystemStorage(storage); err != nil {
		return err
	}
	for _, executable := range []string{config.BrokerPath, config.Worker.WorkerPath, config.Worker.WatchdogPath} {
		if !validFilesystemHostPath(executable) {
			return ErrInvalidIsolatedConfig
		}
	}
	if config.Worker.LinuxCgroupParent != "" && !validFilesystemHostPath(config.Worker.LinuxCgroupParent) {
		return ErrInvalidIsolatedConfig
	}
	if err := validateFilesystemGrantPaths(config.Roots, storage); err != nil {
		return err
	}
	config.Roots = slices.Clone(config.Roots)
	grants := make([]sandboxbroker.RootGrant, len(config.Roots))
	for index, root := range config.Roots {
		grants[index] = sandboxbroker.RootGrant{Name: root.Name, Rights: root.Rights}
	}
	budget, err := sandboxbroker.NewFilesystemBudget(grants, config.Limits)
	if err != nil {
		return errors.Join(ErrInvalidIsolatedConfig, err)
	}
	budget.Close()
	return nil
}

// validFilesystemHostPath bounds unambiguous host paths before policy encoding.
//
// Takes path (string) which is the host-selected executable, storage or grant path.
//
// Returns false for invalid UTF-8, embedded NUL, relative or oversized paths.
func validFilesystemHostPath(path string) bool {
	return len(path) <= maximumFilesystemHostPath && utf8.ValidString(path) &&
		strings.IndexByte(path, 0) < 0 && filepath.IsAbs(path)
}

// validateFilesystemStorage rejects missing or lexically overlapping recovery stores.
//
// Takes storage ([]string) which holds all host-selected metadata directories before
// native descriptor validation.
//
// Returns an invalid-configuration error without accessing storage.
func validateFilesystemStorage(storage []string) error {
	for index, directory := range storage {
		if !validFilesystemHostPath(directory) {
			return ErrInvalidIsolatedConfig
		}
		for _, other := range storage[:index] {
			if filesystemPathsOverlap(directory, other) {
				return ErrInvalidIsolatedConfig
			}
		}
	}
	return nil
}

// validateFilesystemGrantPaths separates host grants from every recovery store.
//
// Takes roots ([]FilesystemRoot) which holds the bounded host-selected roots.
// Takes storage ([]string) which holds the validated metadata directory paths.
//
// Returns an invalid-configuration error before root opening or policy encoding.
func validateFilesystemGrantPaths(roots []FilesystemRoot, storage []string) error {
	for _, root := range roots {
		if !validFilesystemHostPath(root.HostPath) {
			return ErrInvalidIsolatedConfig
		}
		for _, directory := range storage {
			if filesystemPathsOverlap(root.HostPath, directory) {
				return ErrInvalidIsolatedConfig
			}
		}
	}
	return nil
}

// filesystemPathsOverlap reports whether two host paths lexically overlap.
//
// Takes first (string) which is the first absolute host path, never a script-selected
// filename.
// Takes second (string) which is the second absolute host path, never a script-selected
// filename.
//
// Returns true when either path contains the other after lexical normalisation.
func filesystemPathsOverlap(first, second string) bool {
	for _, pair := range [][2]string{{first, second}, {second, first}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
