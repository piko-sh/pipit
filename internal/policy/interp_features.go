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

package policy

import (
	"fmt"
)

// InterpFeature is a bitmask selecting which interpreter language features (loops,
// closures, goroutines, etc.) are permitted in a compiled program. Use the
// InterpFeatures* constants for set members.
type InterpFeature uint32

const (
	// InterpFeatureForLoops allows for statements.
	InterpFeatureForLoops InterpFeature = 1 << iota

	// InterpFeatureRangeLoops allows range iterations.
	InterpFeatureRangeLoops

	// InterpFeatureRecursion allows direct and mutual recursion. Checked post-compilation
	// via call graph analysis.
	InterpFeatureRecursion

	// InterpFeatureGoroutines allows go statements.
	InterpFeatureGoroutines

	// InterpFeatureChannels allows channel operations (make, send, receive, close, select).
	InterpFeatureChannels

	// InterpFeatureDefer allows defer statements.
	InterpFeatureDefer

	// InterpFeatureGoto allows goto statements.
	InterpFeatureGoto

	// InterpFeatureClosures allows function literals.
	InterpFeatureClosures

	// InterpFeatureUnsafeOps allows unsafe package operations.
	InterpFeatureUnsafeOps

	// InterpFeaturePanicRecover allows panic and recover calls.
	InterpFeaturePanicRecover

	// InterpFeaturesNone is the default value that disables all language features.
	InterpFeaturesNone InterpFeature = 0

	// InterpFeaturesAll enables all language features. This is the default for Pipit dev
	// mode.
	InterpFeaturesAll = InterpFeatureForLoops | InterpFeatureRangeLoops |
		InterpFeatureRecursion | InterpFeatureGoroutines |
		InterpFeatureChannels | InterpFeatureDefer |
		InterpFeatureGoto | InterpFeatureClosures |
		InterpFeatureUnsafeOps | InterpFeaturePanicRecover

	// InterpFeaturesRestricted allows most features but disables goroutines, unsafe
	// operations, goto, and panic/recover. Suitable for CMS environments where concurrency
	// and low-level access are not needed.
	InterpFeaturesRestricted = InterpFeatureForLoops | InterpFeatureRangeLoops |
		InterpFeatureRecursion | InterpFeatureChannels |
		InterpFeatureDefer | InterpFeatureClosures

	// InterpFeaturesMinimal allows only basic sequential code with no loops, recursion,
	// goroutines, channels, defer, goto, closures, unsafe, or panic/recover. Suitable for
	// simple expression evaluation.
	InterpFeaturesMinimal InterpFeature = 0
)

// Has checks if the feature set includes the given feature.
//
// Takes feature (InterpFeature) which is the feature to check for.
//
// Returns bool which is true if the feature is present in the set.
func (f InterpFeature) Has(feature InterpFeature) bool {
	return f&feature == feature
}

// String returns a readable name for the feature for use in error messages.
//
// Returns string which is the display name shown in diagnostics.
func (f InterpFeature) String() string {
	switch f {
	case InterpFeatureForLoops:
		return "for loops"
	case InterpFeatureRangeLoops:
		return "range loops"
	case InterpFeatureRecursion:
		return "recursion"
	case InterpFeatureGoroutines:
		return "goroutines"
	case InterpFeatureChannels:
		return "channels"
	case InterpFeatureDefer:
		return "defer"
	case InterpFeatureGoto:
		return "goto"
	case InterpFeatureClosures:
		return "closures"
	case InterpFeatureUnsafeOps:
		return "unsafe operations"
	case InterpFeaturePanicRecover:
		return "panic/recover"
	default:
		return fmt.Sprintf("InterpFeature(%d)", uint32(f))
	}
}
