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

// Package typemodel holds the interpreter's view of a Go type at the native boundary.
//
// It provides Type, the named-interface wrapper that keeps a user-declared interface's
// identity when a value crosses into reflect, the named-scalar pool that lends distinct
// reflect types to named basic types, and the linked-type and identity-transparency
// tables. The symbol registry imports typemodel, not the other way round.
package typemodel
