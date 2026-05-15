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

package program

import "pipit.sh/pipit/internal/isa"

// IsInlineableShape reports whether a call-site matches a fused-dispatch shape, which is
// exactly two arguments (the first on the general bank) and one uint return.
//
// Takes site (*CallSite) which is the call-site descriptor to check.
//
// Returns true when the site's signature matches a candidate shape.
func IsInlineableShape(site *CallSite) bool {
	if len(site.Arguments) != 2 || len(site.Returns) != 1 {
		return false
	}
	if site.Arguments[0].Kind != isa.RegisterGeneral {
		return false
	}
	if site.Returns[0].Kind != isa.RegisterUint {
		return false
	}
	return true
}
