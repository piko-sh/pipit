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

package engine

import "pipit.sh/pipit/internal/isa"

func testRegCounts() [isa.NumRegisterKinds]uint32 {
	var counts [isa.NumRegisterKinds]uint32
	counts[isa.RegisterInt] = 4
	counts[isa.RegisterFloat] = 4
	counts[isa.RegisterString] = 4
	counts[isa.RegisterGeneral] = 4
	counts[isa.RegisterBool] = 4
	counts[isa.RegisterUint] = 4
	counts[isa.RegisterComplex] = 4
	return counts
}
