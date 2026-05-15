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

import "pipit.sh/pipit/internal/safeconv"

// SourcePosition records the source file location for a single bytecode instruction. The
// debugger uses it to map program counters back to source lines.
type SourcePosition struct {
	// Line is the 1-based source line number. 0 means unknown or synthetic, such as a NOP
	// inserted by the optimiser.
	Line int32

	// Column is the 1-based source column number. 0 means unknown.
	Column int16

	// FileID is an index into sourceMap.files, identifying which source file this
	// instruction came from.
	FileID uint16

	// Inlined is true when the instruction was spliced in from an inlined callee. The
	// debugger uses it to distinguish an inlined return from a new entry to the caller's
	// line.
	Inlined bool
}

// SourceMap maps program counter offsets to source file positions. The positions slice is
// parallel to CompiledFunction.body; each entry corresponds to the instruction at the
// same index.
type SourceMap struct {
	// Files points to the shared file table mapping FileID indices to source file paths.
	// Shared via pointer across functions compiled from the same compilation unit so that
	// appends from any sub-Compiler are visible to all.
	Files *[]string

	// Positions is parallel to CompiledFunction.body. positions[pc] gives the source
	// position for body[pc].
	Positions []SourcePosition

	// Epilogue is the position of the function's closing brace, reported for a frame that
	// ran off the end of its body: the line Go gives an implicit return.
	Epilogue SourcePosition
}

// Inlined reports whether the instruction at pc was spliced in from an inlined callee.
//
// Takes pc (int) which is the program counter to look up.
//
// Returns bool which is false when the source map is nil or pc is out of range.
func (sm *SourceMap) Inlined(pc int) bool {
	if sm == nil || pc < 0 || pc >= len(sm.Positions) {
		return false
	}
	return sm.Positions[pc].Inlined
}

// SourcePosition returns the file path, line, and column for the given program counter.
//
// Takes pc (int) which is the program counter to look up.
//
// Returns the file path, line number, and column number.
// Returns empty string and zeros when the source map is nil, pc is out of range, or the
// position is synthetic.
func (sm *SourceMap) SourcePosition(pc int) (file string, line int, column int) {
	if sm == nil || pc < 0 || pc >= len(sm.Positions) {
		return "", 0, 0
	}
	pos := sm.Positions[pc]
	if pos.Line == 0 {
		return "", 0, 0
	}
	if sm.Files == nil || int(pos.FileID) >= len(*sm.Files) {
		return "", int(pos.Line), int(pos.Column)
	}
	return (*sm.Files)[pos.FileID], int(pos.Line), int(pos.Column)
}

// FilePath returns the source path recorded for fileID, or the empty string when the
// identifier is out of range.
//
// Takes fileID (uint16) which indexes the shared file table.
//
// Returns the file path.
func (sm *SourceMap) FilePath(fileID uint16) string {
	if sm == nil || sm.Files == nil || int(fileID) >= len(*sm.Files) {
		return ""
	}
	return (*sm.Files)[fileID]
}

// FileIDFor returns the identifier of path in the source map's file table, appending the
// path when it is not yet present.
//
// Takes path (string) which is the source file path.
//
// Returns the file identifier.
func (sm *SourceMap) FileIDFor(path string) uint16 {
	if sm.Files == nil {
		sm.Files = &[]string{}
	}
	for i, existing := range *sm.Files {
		if existing == path {
			return uint16(i)
		}
	}
	*sm.Files = append(*sm.Files, path)
	return safeconv.MustIntToUint16(len(*sm.Files) - 1)
}

// EpiloguePosition returns the closing-brace position recorded for the function.
//
// Returns string which is the file, empty when none was recorded.
// Returns int which is the line, 0 when none was recorded.
func (sm *SourceMap) EpiloguePosition() (file string, line int) {
	if sm == nil || sm.Epilogue.Line == 0 {
		return "", 0
	}
	if sm.Files == nil || int(sm.Epilogue.FileID) >= len(*sm.Files) {
		return "", int(sm.Epilogue.Line)
	}
	return (*sm.Files)[sm.Epilogue.FileID], int(sm.Epilogue.Line)
}

// DebugVarEntry records debug information for a single variable declaration, mapping a
// variable name to its runtime location and the bytecode range where it is live.
type DebugVarEntry struct {
	// Name is the source-level variable name.
	Name string

	// Location is the register or upvalue location at runtime.
	Location VarLocation

	// StartPC is the first instruction (inclusive) where the variable is in scope.
	StartPC int

	// EndPC is the first instruction (exclusive) past the variable's scope. 0 means
	// end-of-function.
	EndPC int
}

// DebugVarTable holds variable debug information for a single compiled function.
type DebugVarTable struct {
	// Entries holds all variable declarations with their liveness ranges.
	Entries []DebugVarEntry
}

// LiveVariables returns the variable entries that are live at the given program counter.
//
// Takes pc (int) which is the program counter to check liveness at.
//
// Returns a slice of DebugVarEntry for variables whose scope contains the given pc.
func (dvt *DebugVarTable) LiveVariables(pc int) []DebugVarEntry {
	if dvt == nil {
		return nil
	}
	var live []DebugVarEntry
	for _, entry := range dvt.Entries {
		end := entry.EndPC
		if end == 0 {
			end = int(^uint(0) >> 1)
		}
		if pc >= entry.StartPC && pc < end {
			live = append(live, entry)
		}
	}
	return live
}

// HasDebugSourceMap reports whether the function has a source map.
//
// Returns bool which is true when a source map is present.
func (compiledFunction *CompiledFunction) HasDebugSourceMap() bool {
	return compiledFunction.DebugSourceMap != nil
}

// DebugSourcePosition returns the source position for the given program counter.
//
// Takes pc (int) which is the program counter to look up.
//
// Returns the file path, line number, and column number.
func (compiledFunction *CompiledFunction) DebugSourcePosition(pc int) (file string, line int, column int) {
	if compiledFunction.DebugSourceMap == nil {
		return "", 0, 0
	}
	return compiledFunction.DebugSourceMap.SourcePosition(pc)
}

// HasDebugVarTable reports whether the function has a variable table.
//
// Returns bool which is true when a variable table is present.
func (compiledFunction *CompiledFunction) HasDebugVarTable() bool {
	return compiledFunction.DebugVarTable != nil
}
