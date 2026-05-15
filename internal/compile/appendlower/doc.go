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

// Package appendlower holds the analysis behind the in-place append lowering: which slice
// locals may be appended to in place (their register is the only handle on the slice
// header) and whether an assignment has the `x = append(x, ...)` shape that the lowering
// rewrites.
//
// Everything here reads go/ast and go/types only. The emitters that act on the answers
// stay in the compiler, which owns the scope stack the final register check needs.
package appendlower
