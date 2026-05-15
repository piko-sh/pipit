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

package pipit

import (
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

var (
	// ErrInvalidRestrictedConfig reports an invalid restricted configuration.
	ErrInvalidRestrictedConfig = app.ErrInvalidRestrictedConfig

	// ErrRestrictedBusy reports a concurrent restricted submission.
	ErrRestrictedBusy = app.ErrRestrictedBusy

	// ErrRestrictedLimit reports a source, output, or returned value limit.
	ErrRestrictedLimit = app.ErrRestrictedLimit

	// ErrRestrictedResult reports a result that cannot be a JSON scalar.
	ErrRestrictedResult = app.ErrRestrictedResult
)

// RestrictedConfig selects finite, cooperative limits and reviewed imports. Zero limits
// select defaults; negative limits are invalid.
type RestrictedConfig = app.RestrictedConfig

// RestrictedResult contains captured output and an optional bounded JSON scalar.
type RestrictedResult = app.RestrictedResult

// RestrictedInterpreter provides fresh state per call without exposing a mutable
// registry. It rejects concurrent submissions.
type RestrictedInterpreter = app.RestrictedInterpreter

// NewRestrictedInterpreter creates an opt-in restricted interpreter over the given symbol
// providers. NewInterpreter remains trusted and fast.
//
// The reviewed import set is the intersection of the allowlist and what the providers
// actually register, so a narrow set of providers narrows the tier further. At least one
// is required: the tier reviews imports rather than inventing them.
//
// Takes config (RestrictedConfig) which is validated and defensively copied.
// Takes providers (...SymbolProviderPort) which supply the reviewable symbols.
//
// Returns *RestrictedInterpreter which evaluates source with fresh state per call.
// Returns error when no provider is given, a limit is negative, or an import has no
// reviewed manifest.
func NewRestrictedInterpreter(config RestrictedConfig, providers ...SymbolProviderPort) (*RestrictedInterpreter, error) {
	switch len(providers) {
	case 0:
		return nil, ErrNoSymbolProvider
	case 1:
		return app.NewRestrictedInterpreterWithProvider(config, providers[0])
	default:
		return app.NewRestrictedInterpreterWithProvider(config, symtab.NewCompositeSymbolProvider(providers...))
	}
}
