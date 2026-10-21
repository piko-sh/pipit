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

// Package checker provides the licencecheck analysers for structural properties of
// Pipit's engine that licensed reuse relies on and the compiler cannot check.
package checker

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// checkerConfig is licencecheck.json.
type checkerConfig struct {
	// UnsafeAllowed maps a function that creates unsafe or reflective write capability,
	// named as types.Func.FullName prints it ("pkgpath.F" or "(*pkgpath.T).M"), to its
	// justification (the unsafe-writes rule).
	UnsafeAllowed map[string]string `json:"unsafe_allowed"`

	// Structs maps a struct type ("pkgpath.Name") to its classified fields, each field name
	// to its classification (the roots, closure-fields and program-lifetime rules).
	Structs map[string]structRule `json:"structs"`

	// Exhaustive lists the enum types whose switches must name every constant, and the
	// switch sites ("pkgpath.Func") whose default clause is justified (the exhaustive-switch
	// rule).
	Exhaustive exhaustiveRule `json:"exhaustive"`

	// Handlers configures the handler-barriers rule.
	Handlers handlerRule `json:"handlers"`
}

// structRule classifies one struct type's fields.
type structRule struct {
	// Why says what the classification guards.
	Why string `json:"why"`

	// Fields maps each field name to its classification.
	Fields map[string]string `json:"fields"`

	// Rule names the property the struct's classification guards (roots, closure-fields or
	// program-lifetime); the struct-field reports print it.
	Rule string `json:"rule"`
}

// exhaustiveRule configures the exhaustive-switch rule.
type exhaustiveRule struct {
	// AllowedDefaults maps "pkgpath.Func" to the justification of its switch's default.
	AllowedDefaults map[string]string `json:"allowed_defaults"`

	// Types lists the enum types ("pkgpath.Name").
	Types []string `json:"types"`
}

// handlerRule configures the handler-barriers rule.
type handlerRule struct {
	// Prefix is the name prefix of an opcode handler (handle).
	Prefix string `json:"prefix"`

	// Barriers lists the barrier functions ("pkgpath.Name" or "pkgpath.(*T).Name"); a
	// handler that stores a reference must call one directly.
	Barriers []string `json:"barriers"`

	// Allowed maps a handler ("pkgpath.Func") that calls no barrier to the number of
	// reference stores it was reviewed with and the justification.
	Allowed map[string]allowedHandler `json:"allowed"`

	// Packages lists the package paths whose handlers are checked.
	Packages []string `json:"packages"`
}

// allowedHandler is one reviewed handler of the handler-barriers rule.
type allowedHandler struct {
	// Why is the justification.
	Why string `json:"why"`

	// Stores is the number of unbarriered reference stores reviewed.
	Stores int `json:"stores"`
}

// configJSON holds the embedded licence-checker configuration.
//
//go:embed licencecheck.json
var configJSON []byte

// testConfig, when set, replaces the embedded configuration (analysistest fixtures).
var testConfig *checkerConfig

// loadConfig parses the embedded licencecheck.json on its first call and returns the same
// result on every later call: the parsed configuration, or the error parsing returned.
var loadConfig = sync.OnceValues(parseEmbeddedConfig)

// parseEmbeddedConfig parses the embedded licencecheck.json.
//
// Returns *checkerConfig which holds the parsed configuration.
// Returns error when parsing the embedded configuration fails; nil on success.
func parseEmbeddedConfig() (*checkerConfig, error) {
	var parsed checkerConfig
	if err := json.Unmarshal(configJSON, &parsed); err != nil {
		return nil, fmt.Errorf("licencecheck.json: %w", err)
	}
	return &parsed, nil
}

// activeConfig returns the configuration the analysers use.
//
// Returns *checkerConfig and an error when the embedded file does not parse.
func activeConfig() (*checkerConfig, error) {
	if testConfig != nil {
		return testConfig, nil
	}
	return loadConfig()
}
