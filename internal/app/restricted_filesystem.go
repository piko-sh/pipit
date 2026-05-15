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
	"slices"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/symtab"
)

// NewFilesystemRestrictedInterpreter installs fixed worker-local filesystem RPC stubs for
// a natively confined worker.
//
// Takes config (RestrictedConfig) which must import pipit/fs.
// Takes proxy (*sandboxbroker.FilesystemProxy) which is the private protocol proxy.
//
// Returns a restricted interpreter with no general registry or native object exposure.
func NewFilesystemRestrictedInterpreter(config RestrictedConfig, proxy *sandboxbroker.FilesystemProxy) (*RestrictedInterpreter, error) {
	return newFilesystemRestrictedInterpreter(config, proxy, nil)
}

// NewFilesystemRestrictedInterpreterWithProvider is NewFilesystemRestrictedInterpreter
// over the full registry the provider supplies, exposing only the allowlisted packages
// plus pipit/fs.
//
// Takes config (RestrictedConfig) which must import pipit/fs.
// Takes proxy (*sandboxbroker.FilesystemProxy) which is the private protocol proxy.
// Takes provider (symtab.SymbolProviderPort) which supplies the full symbol registry.
//
// Returns a filesystem-capable restricted interpreter, or a configuration error.
func NewFilesystemRestrictedInterpreterWithProvider(config RestrictedConfig, proxy *sandboxbroker.FilesystemProxy, provider symtab.SymbolProviderPort) (*RestrictedInterpreter, error) {
	if provider == nil {
		return nil, ErrInvalidRestrictedConfig
	}
	return newFilesystemRestrictedInterpreter(config, proxy, provider)
}

// newFilesystemRestrictedInterpreter shares the filesystem-attach path between the direct
// and provider constructors.
//
// Takes config (RestrictedConfig) which holds limits and imports.
// Takes proxy (*sandboxbroker.FilesystemProxy) which is the protocol proxy.
// Takes provider (symtab.SymbolProviderPort) which may be nil for the math-only surface.
//
// Returns the interpreter, or a configuration error.
func newFilesystemRestrictedInterpreter(config RestrictedConfig, proxy *sandboxbroker.FilesystemProxy, provider symtab.SymbolProviderPort) (*RestrictedInterpreter, error) {
	if proxy == nil || !slices.Contains(config.Imports, "pipit/fs") {
		return nil, ErrInvalidRestrictedConfig
	}
	imports := slices.Clone(config.Imports)
	config.Imports = slices.DeleteFunc(imports, func(path string) bool { return path == "pipit/fs" })
	interpreter, err := newRestrictedInterpreter(config, provider)
	if err != nil {
		return nil, err
	}
	interpreter.config.Imports = append(interpreter.config.Imports, "pipit/fs")
	interpreter.filesystem = proxy
	return interpreter, nil
}
