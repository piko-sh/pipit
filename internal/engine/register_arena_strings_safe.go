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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"strings"
)

// arenaToUpper is strings.ToUpper.
//
// Takes s (string) which is the input.
//
// Returns string which is the result.
func arenaToUpper(_ *RegisterArena, s string) string { return strings.ToUpper(s) }

// arenaToLower is strings.ToLower.
//
// Takes s (string) which is the input.
//
// Returns string which is the result.
func arenaToLower(_ *RegisterArena, s string) string { return strings.ToLower(s) }

// arenaRepeat is strings.Repeat.
//
// Takes s (string) which is the input.
// Takes count (int) which is non-negative.
//
// Returns string which is the result.
func arenaRepeat(_ *RegisterArena, s string, count int) string { return strings.Repeat(s, count) }

// arenaReplaceAll is strings.ReplaceAll.
//
// Takes s (string) which is the input.
// Takes old (string) which is the text replaced.
// Takes newText (string) which replaces it.
//
// Returns string which is the result.
func arenaReplaceAll(_ *RegisterArena, s, old, newText string) string {
	return strings.ReplaceAll(s, old, newText)
}

// arenaJoin is strings.Join over the strings of parts.
//
// Takes parts (reflect.Value) which is a slice or array; each element is read by String.
// Takes sep (string) which separates the parts; never returned itself.
//
// Returns string which is the result.
func arenaJoin(_ *RegisterArena, parts reflect.Value, sep string) string {
	return heapJoin(parts, sep)
}
