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

package module

import (
	"errors"
	"fmt"
	"strings"
)

// Ref identifies a module dependency. It is the value compiled code embeds when it
// imports a third-party module, and the lookup key a Provider resolves to produce a
// Bundle.
type Ref struct {
	// Path is the module's canonical identifier, mandatory.
	Path string

	// Version is the human-readable selector or empty for latest.
	Version string

	// Pin is the integrity hash a host can cross-check.
	Pin string
}

// String formats a Ref in canonical "path@version[#pin]" form.
//
// Empty Version is rendered without the @ separator; empty Pin is rendered without the #
// separator. Used by error messages, lockfile output, and CLI listings - not by wire
// serialisation.
//
// Returns string which is the formatted reference.
func (r Ref) String() string {
	var builder strings.Builder
	builder.WriteString(r.Path)
	if r.Version != "" {
		builder.WriteByte('@')
		builder.WriteString(r.Version)
	}
	if r.Pin != "" {
		builder.WriteByte('#')
		builder.WriteString(r.Pin)
	}
	return builder.String()
}

// IsZero reports whether the reference is the zero value. A zero reference is not a valid
// lookup key; providers return ErrNotFound when asked to resolve it.
//
// Returns true when Path, Version, and Pin are all empty.
func (r Ref) IsZero() bool {
	return r.Path == "" && r.Version == "" && r.Pin == ""
}

// ParseRef parses a "path[@version][#pin]" string into a reference.
//
// Whitespace around the input is trimmed. Empty input returns an error; otherwise parsing
// is total - invalid pin shapes are passed through unchanged for the caller to validate
// when checking against actual bundle bytes.
//
// Takes input (string) which is the reference in canonical form.
//
// Returns Ref which is the parsed reference.
// Returns error when input is empty or lacks a path component.
func ParseRef(input string) (Ref, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Ref{}, errors.New("module: empty module reference")
	}
	reference := Ref{Path: "", Version: "", Pin: ""}
	if pinIndex := strings.IndexByte(trimmed, '#'); pinIndex >= 0 {
		reference.Pin = trimmed[pinIndex+1:]
		trimmed = trimmed[:pinIndex]
	}
	if versionIndex := strings.IndexByte(trimmed, '@'); versionIndex >= 0 {
		reference.Version = trimmed[versionIndex+1:]
		trimmed = trimmed[:versionIndex]
	}
	reference.Path = trimmed
	if reference.Path == "" {
		return Ref{}, fmt.Errorf("module: module reference %q has empty path", input)
	}
	return reference, nil
}
