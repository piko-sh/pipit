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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"strings"
	"unicode/utf8"

	"pipit.sh/pipit/internal/mem"
)

// arenaIntrinsicMaxBytes is the largest intrinsic result placed in the byte slab; a
// larger one would grow the slab for one value.
const arenaIntrinsicMaxBytes = InitialByteSlabSize / 4

// arenaIntrinsicFits reports whether a result of n bytes goes to arena's byte slab.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes n (int) which is the result's length.
//
// Returns bool which is true when the result is built in the slab.
func arenaIntrinsicFits(arena *RegisterArena, n int) bool {
	return arena != nil && n > 0 && n <= arenaIntrinsicMaxBytes
}

// asciiCaseScan reports whether text is ASCII and whether any byte would change under the
// mapping from low to low+('A'-'a') or back.
//
// Takes text (string) which is the input.
// Takes low (byte) which is the first byte in the case-mapping range, inclusive.
// Takes high (byte) which is the last byte in the case-mapping range, inclusive.
//
// Returns isASCII (bool) which is true when every byte is ASCII.
// Returns changes (bool) which is true when the case mapping would change a byte.
func asciiCaseScan(text string, low, high byte) (isASCII, changes bool) {
	for i := range len(text) {
		character := text[i]
		if character >= utf8.RuneSelf {
			return false, changes
		}
		changes = changes || (character >= low && character <= high)
	}
	return true, changes
}

// arenaToUpper is strings.ToUpper with an ASCII result built in the byte slab.
//
// Takes arena (*RegisterArena) which provides the slab; may be nil.
// Takes text (string) which is the input.
//
// Returns string which is text itself when nothing changes.
func arenaToUpper(arena *RegisterArena, text string) string {
	isASCII, changes := asciiCaseScan(text, 'a', 'z')
	if !isASCII || !arenaIntrinsicFits(arena, len(text)) {
		return strings.ToUpper(text)
	}
	if !changes {
		return text
	}
	buffer := arena.AllocStringBytes(len(text))
	for i := range len(text) {
		character := text[i]
		if 'a' <= character && character <= 'z' {
			character -= 'a' - 'A'
		}
		buffer[i] = character
	}
	return mem.String(buffer)
}

// arenaToLower is strings.ToLower with an ASCII result built in the byte slab.
//
// Takes arena (*RegisterArena) which provides the slab; may be nil.
// Takes text (string) which is the input.
//
// Returns string which is text itself when nothing changes.
func arenaToLower(arena *RegisterArena, text string) string {
	isASCII, changes := asciiCaseScan(text, 'A', 'Z')
	if !isASCII || !arenaIntrinsicFits(arena, len(text)) {
		return strings.ToLower(text)
	}
	if !changes {
		return text
	}
	buffer := arena.AllocStringBytes(len(text))
	for i := range len(text) {
		character := text[i]
		if 'A' <= character && character <= 'Z' {
			character += 'a' - 'A'
		}
		buffer[i] = character
	}
	return mem.String(buffer)
}

// arenaRepeat is strings.Repeat with its result built in the byte slab. The caller has
// rejected a negative count; an overflowing length goes to strings.Repeat, which panics
// as it always did.
//
// Takes arena (*RegisterArena) which provides the slab; may be nil.
// Takes text (string) which is the input.
// Takes count (int) which is non-negative.
//
// Returns string which is the repetition.
func arenaRepeat(arena *RegisterArena, text string, count int) string {
	if len(text) == 0 || count == 0 {
		return ""
	}
	if count == 1 {
		return text
	}
	if count > arenaIntrinsicMaxBytes/len(text) || !arenaIntrinsicFits(arena, len(text)*count) {
		return strings.Repeat(text, count)
	}
	size := len(text) * count
	buffer := arena.AllocStringBytes(size)
	filled := copy(buffer, text)
	for filled < size {
		filled += copy(buffer[filled:], buffer[:filled])
	}
	return mem.String(buffer)
}

// arenaReplaceAll is strings.ReplaceAll with its result built in the byte slab. It
// follows strings.Replace step for step, including an empty old string, which inserts
// newText at every rune boundary.
//
// Takes arena (*RegisterArena) which provides the slab; may be nil.
// Takes text (string) which is the input.
// Takes old (string) which is the text replaced.
// Takes newText (string) which replaces it.
//
// Returns string which is text itself when nothing is replaced.
func arenaReplaceAll(arena *RegisterArena, text, old, newText string) string {
	if old == newText {
		return text
	}
	matches := strings.Count(text, old)
	if matches == 0 {
		return text
	}
	growth := len(newText) - len(old)
	if growth > 0 && matches > arenaIntrinsicMaxBytes/growth {
		return strings.ReplaceAll(text, old, newText)
	}
	size := len(text) + matches*growth
	if !arenaIntrinsicFits(arena, size) {
		return strings.ReplaceAll(text, old, newText)
	}
	buffer := arena.AllocStringBytes(size)
	written, start := 0, 0
	for i := range matches {
		j := start
		if len(old) == 0 {
			if i > 0 {
				_, width := utf8.DecodeRuneInString(text[start:])
				j += width
			}
		} else {
			j += strings.Index(text[start:], old)
		}
		written += copy(buffer[written:], text[start:j])
		written += copy(buffer[written:], newText)
		start = j + len(old)
	}
	copy(buffer[written:], text[start:])
	return mem.String(buffer)
}

// arenaJoin is strings.Join over the strings of parts, with its result built in the byte
// slab and no []string of the parts. One part is returned itself, as strings.Join does.
//
// Takes arena (*RegisterArena) which provides the slab; may be nil.
// Takes parts (reflect.Value) which is a slice or array; each element is read by String.
// Takes sep (string) which separates the parts; never returned itself.
//
// Returns string which is the joined string.
func arenaJoin(arena *RegisterArena, parts reflect.Value, sep string) string {
	count := parts.Len()
	switch count {
	case 0:
		return ""
	case 1:
		return parts.Index(0).String()
	}
	if arena == nil || len(sep) > arenaIntrinsicMaxBytes/(count-1) {
		return heapJoin(parts, sep)
	}
	total := len(sep) * (count - 1)
	for i := range count {
		total += len(parts.Index(i).String())
		if total > arenaIntrinsicMaxBytes {
			return heapJoin(parts, sep)
		}
	}
	if !arenaIntrinsicFits(arena, total) {
		return heapJoin(parts, sep)
	}
	buffer := arena.AllocStringBytes(total)
	written := copy(buffer, parts.Index(0).String())
	for i := 1; i < count; i++ {
		written += copy(buffer[written:], sep)
		written += copy(buffer[written:], parts.Index(i).String())
	}
	return mem.String(buffer)
}
