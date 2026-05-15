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
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"pipit.sh/pipit"
)

// HookMode controls the pipit capability hook's behaviour when it encounters an
// unapproved capability claim.
type HookMode int

const (
	// HookModePermissive logs every check but allows everything. Default for development;
	// refused in production.
	HookModePermissive HookMode = iota

	// HookModeInteractive prompts the operator on the TTY for every unapproved capability
	// claim and persists the answer in the lockfile. Used by pipit run when stdin and stdout
	// are TTYs.
	HookModeInteractive

	// HookModeFrozen treats the lockfile as the source of truth; any check against an
	// unapproved capability returns an error. Used by `pipit run --gate` with `--autodeny`
	// and by non-TTY runs.
	HookModeFrozen

	// HookModeNoPrompt is like Frozen but emits a friendlier error referring the operator to
	// an interactive run. Reserved for `pipit module` freeze-style flows.
	HookModeNoPrompt
)

// Hook implements the pipit CapabilityHook interface with interactive-approval and
// lockfile-persistence semantics.
type Hook struct {
	// prompter asks the operator for approval in interactive mode.
	prompter Prompter

	// logSink receives diagnostic messages about capability checks.
	logSink io.Writer

	// store persists approved capabilities to the lockfile.
	store *Store

	// gated tracks which capability axes are subject to gating.
	gated map[string]bool

	// scriptLabel identifies the script in prompt and log messages.
	scriptLabel string

	// mode selects the approval strategy (permissive, interactive, frozen).
	mode HookMode

	// mu guards concurrent access to the hook's mutable state.
	mu sync.Mutex
}

var _ pipit.CapabilityHook = (*Hook)(nil)

// NewHook constructs a Hook in the requested mode, wired to a lockfile Store and a
// prompter (used only in HookModeInteractive).
//
// Takes mode (HookMode) which selects behaviour.
// Takes store (*Store) which persists approval decisions; may be nil in
// HookModePermissive.
// Takes prompter (Prompter) which is used in HookModeInteractive; may be nil in other
// modes.
//
// Returns a *Hook implementing pipit.CapabilityHook.
func NewHook(mode HookMode, store *Store, prompter Prompter) *Hook {
	return &Hook{
		mode:     mode,
		store:    store,
		prompter: prompter,
		logSink:  os.Stderr,
		gated:    nil, scriptLabel: "", mu: sync.Mutex{}}
}

// Gate marks axes that CheckFunctionCall enforces. Calls on unlisted axes are allowed
// without consultation, keeping prompts to the operator's chosen capabilities.
//
// Takes axes (...string) which are the capability axes to enforce.
func (h *Hook) Gate(axes ...string) {
	if h.gated == nil {
		h.gated = make(map[string]bool, len(axes))
	}
	for _, axis := range axes {
		h.gated[axis] = true
	}
}

// SetScriptLabel sets the friendly name shown for the main script in prompts and errors.
//
// Takes label (string) which is the display name, typically the script's file name.
func (h *Hook) SetScriptLabel(label string) {
	h.scriptLabel = label
}

// CheckFunctionCall enforces the gated axes against a native call.
//
// Takes modulePath (string) which identifies the calling module.
// Takes fnPath (string) which is the dotted native symbol path.
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns error when the call exercises a gated capability that is refused.
func (h *Hook) CheckFunctionCall(ctx context.Context, modulePath, fnPath string, args []reflect.Value) error {
	if len(h.gated) == 0 {
		return nil
	}
	if h.mode != HookModePermissive {
		switch fnPath {
		case "os.DirFS", "os.OpenRoot", "os.OpenInRoot":
			if h.gated[AxisFilesystemRead] || h.gated[AxisFilesystemWrite] {
				return fmt.Errorf("modloader: %s returns filesystem authority that capability gates cannot confine", fnPath)
			}
		case "os.Exit":
			if h.gated[AxisExec] || h.gated[AxisSubprocess] {
				return fmt.Errorf("modloader: %s would terminate the host process", fnPath)
			}
		}
	}
	axis, scope, gated := classifyNativeCall(fnPath, args)
	if !gated || !h.gated[axis] {
		return nil
	}
	return h.consult(ctx, modulePath, axis, scope)
}

// CheckFileOpen consults the "filesystem.read" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes path (string) which is the filesystem path being opened.
//
// Returns error when the read capability is not approved.
func (h *Hook) CheckFileOpen(ctx context.Context, modulePath, path string, _ int, _ os.FileMode) error {
	return h.consult(ctx, modulePath, "filesystem.read", path)
}

// CheckFileWrite consults the "filesystem.write" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes path (string) which is the filesystem path being written.
//
// Returns error when the write capability is not approved.
func (h *Hook) CheckFileWrite(ctx context.Context, modulePath, path string) error {
	return h.consult(ctx, modulePath, "filesystem.write", path)
}

// CheckExec consults the "exec" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes name (string) which is the executable being run.
//
// Returns error when the exec capability is not approved.
func (h *Hook) CheckExec(ctx context.Context, modulePath, name string, _ []string) error {
	return h.consult(ctx, modulePath, "exec", name)
}

// CheckNetDial consults the "network.dial" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes network (string) which is the transport, such as "tcp".
// Takes address (string) which is the dial target host and port.
//
// Returns error when the dial capability is not approved.
func (h *Hook) CheckNetDial(ctx context.Context, modulePath, network, address string) error {
	return h.consult(ctx, modulePath, "network.dial", network+":"+address)
}

// CheckNetListen consults the "network.listen" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes network (string) which is the transport, such as "tcp".
// Takes address (string) which is the listen address host and port.
//
// Returns error when the listen capability is not approved.
func (h *Hook) CheckNetListen(ctx context.Context, modulePath, network, address string) error {
	return h.consult(ctx, modulePath, "network.listen", network+":"+address)
}

// CheckGetenv consults the "env.read" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes name (string) which is the environment variable being read.
//
// Returns error when the env.read capability is not approved.
func (h *Hook) CheckGetenv(ctx context.Context, modulePath, name string) error {
	return h.consult(ctx, modulePath, "env.read", name)
}

// CheckSetenv consults the "env.write" axis.
//
// Takes modulePath (string) which identifies the calling module.
// Takes name (string) which is the environment variable being set.
//
// Returns error when the env.write capability is not approved.
func (h *Hook) CheckSetenv(ctx context.Context, modulePath, name, _ string) error {
	return h.consult(ctx, modulePath, "env.write", name)
}

// CheckSubprocess consults the "subprocess" axis (distinct from exec to cover
// syscall-level spawns).
//
// Takes modulePath (string) which identifies the calling module.
// Takes name (string) which is the subprocess being spawned.
//
// Returns error when the subprocess capability is not approved.
func (h *Hook) CheckSubprocess(ctx context.Context, modulePath, name string, _ []string) error {
	return h.consult(ctx, modulePath, "subprocess", name)
}

// displayName returns a human-friendly label for a calling module.
//
// Takes modulePath (string) which identifies the module, empty for the main script.
//
// Returns string which is the module path, or the script label for the main script.
func (h *Hook) displayName(modulePath string) string {
	if modulePath != "" {
		return modulePath
	}
	if h.scriptLabel != "" {
		return h.scriptLabel
	}
	return "your script"
}

// consult is the central policy entrypoint shared by every Check* method.
//
// It resolves the (modulePath, axis, scope) claim against the lockfile and the active
// mode.
//
// Takes modulePath (string) which identifies the calling module.
// Takes axis (string) which names the capability axis.
// Takes scope (string) which narrows the claim within the axis.
//
// Returns error when the claim is unapproved and the mode refuses it.
//
// Concurrency: safe for concurrent use; the mutex serialises approval and prompting.
func (h *Hook) consult(ctx context.Context, modulePath, axis, scope string) error {
	if h.mode == HookModePermissive {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	claim := axis + "(" + scope + ")"
	if h.alreadyApproved(modulePath, claim) {
		return nil
	}

	switch h.mode {
	case HookModeFrozen:
		return fmt.Errorf("modloader: %s wants capability %s but it is not approved; run interactively without --autodeny to grant, or add it to the lockfile", h.displayName(modulePath), claim)
	case HookModeNoPrompt:
		return fmt.Errorf("modloader: %s wants capability %s but it is not approved; run `pipit module freeze` interactively to grant", h.displayName(modulePath), claim)
	case HookModeInteractive:
		return h.promptAndPersist(ctx, modulePath, axis, scope, claim)
	default:
		return nil
	}
}

// alreadyApproved reports whether the lockfile records prior approval for (modulePath,
// claim).
//
// claim is the canonical "axis(scope)" string stored in
// LockedModule.ApprovedCapabilities.
//
// Takes modulePath (string) which identifies the calling module.
// Takes claim (string) which is the canonical "axis(scope)" string.
//
// Returns bool which is true when a matching approval exists.
func (h *Hook) alreadyApproved(modulePath, claim string) bool {
	if h.store == nil {
		return false
	}

	snapshot := h.store.Snapshot()
	for _, locked := range snapshot.Modules {
		if locked.Path != modulePath {
			continue
		}
		if slices.Contains(locked.ApprovedCapabilities, claim) {
			return true
		}
	}
	return false
}

// promptAndPersist asks the operator for approval and, on approval, records the claim in
// the lockfile.
//
// Takes modulePath (string) which identifies the calling module.
// Takes axis (string) which names the capability axis.
// Takes scope (string) which narrows the claim within the axis.
// Takes claim (string) which is the canonical "axis(scope)" string.
//
// Returns error when the prompt fails or the operator denies approval.
func (h *Hook) promptAndPersist(ctx context.Context, modulePath, axis, scope, claim string) error {
	if h.prompter == nil {
		return fmt.Errorf("modloader: %s wants capability %s but no prompter is configured", h.displayName(modulePath), claim)
	}
	approve, err := h.prompter.Confirm(ctx, h.displayName(modulePath), axis, scope)
	if err != nil {
		return fmt.Errorf("modloader: prompting for capability %s: %w", claim, err)
	}
	if !approve {
		return fmt.Errorf("modloader: capability %s for %s denied by operator", claim, h.displayName(modulePath))
	}
	h.recordApproval(modulePath, claim)
	return nil
}

// recordApproval adds claim to the lockfile entry for modulePath.
//
// It creates a placeholder entry when no prior LockedModule exists, as happens for the
// main policy or script that had no resolution step.
//
// Takes modulePath (string) which identifies the calling module.
// Takes claim (string) which is the canonical "axis(scope)" string.
func (h *Hook) recordApproval(modulePath, claim string) {
	if h.store == nil {
		return
	}
	snapshot := h.store.Snapshot()
	var existing LockedModule
	found := false
	for _, locked := range snapshot.Modules {
		if locked.Path == modulePath {
			existing = locked
			found = true
			break
		}
	}
	if !found {
		existing = LockedModule{Path: modulePath, ApprovedAt: time.Time{}, Version: "", BundleDigest: "", ApprovedVia: "", ApprovedCapabilities: nil}
	}
	if !slices.Contains(existing.ApprovedCapabilities, claim) {
		existing.ApprovedCapabilities = append(existing.ApprovedCapabilities, claim)
	}
	existing.ApprovedVia = "interactive"
	h.store.Upsert(existing)
}

// Prompter is the interactive-prompt abstraction the Hook calls when a new capability
// claim needs operator approval. The default implementation reads from os.Stdin and
// writes to os.Stderr; tests inject their own.
type Prompter interface {
	// Confirm asks the operator whether to approve the supplied claim.
	//
	// Takes modulePath (string) which identifies the calling module.
	// Takes axis (string) which names the capability axis.
	// Takes scope (string) which narrows the claim within the axis.
	//
	// Returns bool which is true to allow and persist, or false to deny and propagate
	// ErrCapabilityDenied to the interpreted code.
	// Returns error when the prompt interaction fails.
	Confirm(ctx context.Context, modulePath, axis, scope string) (approve bool, err error)
}

// StdinPrompter is the default Prompter. It reads y/n from os.Stdin and writes the prompt
// to os.Stderr, used by pipit run on a TTY.
type StdinPrompter struct{}

// Confirm prints the prompt and reads a single line response.
//
// "y" or "yes" (case-insensitive) approve; anything else denies.
//
// Takes modulePath (string) which identifies the calling module.
// Takes axis (string) which names the capability axis.
// Takes scope (string) which narrows the claim within the axis.
//
// Returns bool which is true when the operator approves the claim.
// Returns error when reading the response from stdin fails.
func (StdinPrompter) Confirm(_ context.Context, modulePath, axis, scope string) (bool, error) {
	scopeNote := ""
	if scope != "" {
		scopeNote = "(" + scope + ") "
	}
	fmt.Fprintf(os.Stderr, "\npipit: %s wants capability: %s %s\n[y/n] ", modulePath, axis, scopeNote)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	trimmed := strings.TrimSpace(strings.ToLower(line))
	return trimmed == "y" || trimmed == "yes", nil
}
