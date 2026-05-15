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

const (
	// MaxSmallConstant is the largest value that fits in isa.SubOpLoadIntConstSmall
	// (single-byte immediate 0-255).
	MaxSmallConstant int64 = 255

	// OptimisationLoopCheckMask is the iteration-count mask used by long-running
	// optimisation passes to amortise the cost of polling ctx.Err() across every (mask+1)
	// iterations.
	OptimisationLoopCheckMask = 1023
)
