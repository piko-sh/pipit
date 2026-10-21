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

// Package stdlink holds the non-generic siblings of the standard library's generic
// functions that the interpreter calls through linked calls.
//
// Go instantiates generic functions when it compiles them, so the standard library ships
// no code for an instantiation the interpreter meets at run time. Each sibling here takes
// the instantiated type arguments as leading reflect.Type values, which the compiler
// resolves from types.Info.Instances, followed by the generic's own arguments. The
// standard-library symbol tables register them. The engine replaces the siblings whose
// meaning depends on script types (errors.AsType, reflect.TypeAssert, runtime.AddCleanup)
// with versions that consult the program's method tables and storage.
package stdlink
