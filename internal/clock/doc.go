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

// Package clock supplies the time source interpreted programs observe.
//
// The interpreter resolves time through Clock rather than calling the time package
// directly, so a host can run interpreted code against a fake clock and get deterministic
// results from anything that sleeps, ticks, or measures elapsed time.
//
// ClockOverrideSymbols() produces the symbol map that replaces the interpreted time
// package's entry points, which is how the substitution reaches interpreted source.
package clock
