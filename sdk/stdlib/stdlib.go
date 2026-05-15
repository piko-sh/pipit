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

package stdlib

import (
	"maps"
	"reflect"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib/codec"
	"pipit.sh/pipit/sdk/stdlib/core"
	"pipit.sh/pipit/sdk/stdlib/crypto"
	"pipit.sh/pipit/sdk/stdlib/gotool"
	"pipit.sh/pipit/sdk/stdlib/image"
	"pipit.sh/pipit/sdk/stdlib/net"
	"pipit.sh/pipit/sdk/stdlib/system"
	"pipit.sh/pipit/sdk/stdlib/text"
)

// WithStandardLibrary registers every bundle of the Go standard library.
//
// Returns pipit.Option applying the registration.
func WithStandardLibrary() pipit.Option {
	return pipit.WithSymbolProvider(Providers()...)
}

// Providers returns one provider per bundle, for composing with host symbols by hand.
//
// Returns []pipit.SymbolProviderPort in bundle order.
func Providers() []pipit.SymbolProviderPort {
	return []pipit.SymbolProviderPort{
		core.Provider(),
		codec.Provider(),
		crypto.Provider(),
		net.Provider(),
		system.Provider(),
		text.Provider(),
		image.Provider(),
		gotool.Provider(),
	}
}

// Exports returns every bundle's symbols merged into one table, for callers that inspect
// the library rather than run it: `pipit doc` and `pipit symbols` are the two in this
// repository.
//
// The table is freshly allocated, so the caller may retain and mutate it.
//
// Returns pipit.SymbolExports keyed by import path, then by symbol name.
func Exports() pipit.SymbolExports {
	merged := make(pipit.SymbolExports)
	for _, provider := range Providers() {
		for path, symbols := range provider.Exports() {
			if merged[path] == nil {
				merged[path] = make(map[string]reflect.Value, len(symbols))
			}
			maps.Copy(merged[path], symbols)
		}
	}
	return merged
}
