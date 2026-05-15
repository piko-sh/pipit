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

// Package stdlib registers the Go standard library with a pipit interpreter.
//
// An interpreter registers nothing on its own, so a script cannot import anything until a
// host says what it may reach. Most hosts want the whole library and say so in one line:
//
//	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary())
//
// The library is the largest thing a pipit binary links, so hosts that want less take
// bundles instead. Each subpackage is one bundle and links only what it registers:
//
//	interpreter := pipit.NewInterpreter(core.WithCore(), codec.WithCodec())
//
// A bundle left out is absent rather than filtered, so its packages cannot be reached
// even through a mistake in an import policy.
//
// Host symbols register the same way, through [pipit.WithSymbolProvider], so an
// application's own tables sit alongside these as equals:
//
//	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), myapp.WithSymbols())
package stdlib
