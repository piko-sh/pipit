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

// Package symtab is the registry of native Go symbols that interpreted code can reach.
//
// A host registers packages of reflect values, and the registry resolves a package path
// and symbol name to the underlying function, type, or variable. It also owns Type, the
// interpreter's view of a named Go type, and the descriptors that let a type or a
// constant survive serialisation and be reconstructed on load.
//
// The registry is the whole of the interpreter's contact with the host's Go code, so it
// is also where the cost of that contact is visible.
package symtab
