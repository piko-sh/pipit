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

// Package typemap maps go/types types onto the interpreter's register model.
//
// Every value the interpreter holds lives in a typed register bank, so compilation has to
// decide, for each type, which bank holds it and how wide it is. The mapping owns that
// decision, along with the type substitution that turns a generic declaration into a
// concrete instantiation.
//
// Keeping it separate means the mapping can be reviewed against the Go type system on its
// own, without the emitter around it.
package typemap
