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

// PeepholeAnnotation records a single rewrite event for a compiled instruction.
type PeepholeAnnotation struct {
	// Kind classifies the rewrite so callers can pick the appropriate human-readable phrase.
	Kind PeepholeRewriteKind

	// Origin is the PC the rewritten instruction derives from, or zero when the kind does
	// not require an origin.
	Origin int

	// OriginFunction is the function-table index the rewritten instruction was copied from.
	// Only meaningful for PeepholeRewriteInline; -1 otherwise.
	OriginFunction int
}

// PeepholeRewriteKind classifies a single peephole rewrite for the disassembler's per-PC
// annotation lookup. The kind determines the human-readable phrase prefixed to the inline
// comment ("CSE'd from", "LICM hoist of", etc.).
type PeepholeRewriteKind uint8
