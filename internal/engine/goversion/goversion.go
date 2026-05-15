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

// Package goversion gates interpreter start-up on the running Go toolchain and names the
// Go language version interpreted source is checked against.
//
// The engine reads reflect.Value internals through unsafe and therefore only starts on Go
// minors whose layout the pinning tests have exercised. Two checks run at start-up: a
// layout probe the engine supplies (never skipped) and a version-prefix allowlist that
// the AllowUntestedEnvVar escape hatch can waive. The allowlist, the language version and
// the error types live here. The probes stay in the engine because they read engine
// internals.
package goversion

import (
	"strings"
)

const (
	// AllowUntestedEnvVar names the escape hatch that lets the interpreter start on a Go
	// minor absent from SupportedPrefixes. Setting it to "1" skips only the prefix
	// allowlist; the layout probe always runs, so a runtime whose reflect internals have
	// actually moved still refuses to start.
	AllowUntestedEnvVar = "PIPIT_UNSAFE_ALLOW_UNTESTED_GO"

	// allowUntestedEnabled is the only value of AllowUntestedEnvVar that enables the escape
	// hatch.
	allowUntestedEnabled = "1"

	// InterpretedGoVersion is the Go language version the type checker accepts for
	// interpreted source, in the "go1.N" form types.Config.GoVersion expects.
	InterpretedGoVersion = "go1.27"
)

// SupportedPrefixes lists the runtime.Version prefixes whose unsafe Value layout has been
// exercised by the pinning tests.
var SupportedPrefixes = [...]string{
	"go1.27",
}

// VersionError is the start-up failure for a Go minor outside the allowlist.
type VersionError struct {
	// version is the rejected runtime.Version string.
	version string
}

// Error renders the rejection with the remediation steps.
//
// Returns the message string.
func (e *VersionError) Error() string {
	return "pipit engine requires a tested Go 1.27+ runtime; running on " + e.version +
		"; extend goversion.SupportedPrefixes after running TestReflectValueLayout, " +
		"TestUnsafeNewAtParity and TestUnsafeRuntimeLayoutSelfCheck against this release, " +
		"or set " + AllowUntestedEnvVar + "=" + allowUntestedEnabled +
		" to skip the allowlist (the layout probes still run)"
}

// LayoutError is the start-up failure for a runtime whose unsafe layout probes failed. It
// is never bypassed by the escape hatch.
type LayoutError struct {
	// cause is the failing probe's error.
	cause error

	// version is the runtime.Version string the probes ran on.
	version string
}

// Error renders the rejection with the probe failure and the pinning-test instruction.
//
// Returns the message string.
func (e *LayoutError) Error() string {
	return "pipit engine unsafe runtime layout self-check failed on " + e.version + ": " +
		e.cause.Error() + "; run TestReflectValueLayout, TestUnsafeNewAtParity and " +
		"TestUnsafeRuntimeLayoutSelfCheck against this release and update " +
		"reflect_value_unsafe.go before extending goversion.SupportedPrefixes"
}

// Unwrap exposes the failing probe's error.
//
// Returns the wrapped cause.
func (e *LayoutError) Unwrap() error {
	return e.cause
}

// Check gates interpreter start-up on the running Go toolchain. The layout probe runs
// first and is never skipped; the prefix allowlist runs second and is the only part the
// escape hatch bypasses.
//
// Takes version (string) which is the runtime.Version string.
// Takes allowUntested (string) which is the value of AllowUntestedEnvVar.
// Takes probe (func() error) which verifies the unsafe runtime layout assumptions.
//
// Returns an error describing why the toolchain is rejected, or nil when it is accepted.
func Check(version string, allowUntested string, probe func() error) error {
	if err := probe(); err != nil {
		return &LayoutError{version: version, cause: err}
	}
	if PrefixSupported(version, allowUntested) {
		return nil
	}
	return &VersionError{version: version}
}

// PrefixSupported reports whether the version passes the prefix allowlist, or whether the
// escape hatch waives it.
//
// Takes version (string) which is the runtime.Version string.
// Takes allowUntested (string) which is the value of AllowUntestedEnvVar.
//
// Returns true when the version is allowlisted or the escape hatch is enabled.
func PrefixSupported(version string, allowUntested string) bool {
	if allowUntested == allowUntestedEnabled {
		return true
	}
	for _, prefix := range SupportedPrefixes {
		if strings.HasPrefix(version, prefix) {
			return true
		}
	}
	return false
}
