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

//go:build !pipit_arena_paranoid

package engine

const (
	// arenaParanoidEnabled gates the arena poisoning diagnostics; see the paranoid twin for
	// what the diagnostic build does. The default build compiles the hooks away entirely.
	arenaParanoidEnabled = false
)

// poisonReleasedPrimitives is a no-op in the default build; the paranoid twin overwrites
// released primitive-slab ranges with canary values on every frame pop.
//
// Takes _ (*ArenaSavePoint) which is unused in the default build.
func (*RegisterArena) poisonReleasedPrimitives(*ArenaSavePoint) {}

// poisonReleasedGenericBytes is a no-op in the default build; the paranoid twin
// overwrites the generic byte slab and its retired generations with canary bytes at
// Reset, so a value that outlived the arena reads loud garbage rather than plausible
// stale data.
func (*RegisterArena) poisonReleasedGenericBytes() {}
