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

//go:build js && wasm

package selfhost

import "go/types"

// TypesPackages has no export data to serve on js/wasm, where the generated loader
// carries none either; the self-hosting lane does not run there.
//
// Returns an empty map.
func (*Provider) TypesPackages() map[string]*types.Package {
	return map[string]*types.Package{}
}

// TypesPackagesLoadError reports nothing to load on js/wasm.
//
// Returns nil.
func (*Provider) TypesPackagesLoadError() error {
	return nil
}
