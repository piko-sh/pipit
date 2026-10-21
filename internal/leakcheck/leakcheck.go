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

package leakcheck

import (
	"testing"

	"go.uber.org/goleak"
)

var (
	// baseOptions holds the default goroutine leak detection ignore rules shared across all
	// tests on every platform. platformOptions() adds the ones a single platform needs.
	baseOptions = []goleak.Option{
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	}
)

// VerifyTestMain wraps goleak.VerifyTestMain() with Pipit's standard ignore rules. Use in
// simple TestMain() functions that only need leak checking.
//
// Takes m (*testing.M) which is the test main instance.
// Takes extraOptions (...goleak.Option) which provides additional ignore rules.
func VerifyTestMain(m *testing.M, extraOptions ...goleak.Option) {
	goleak.VerifyTestMain(m, buildOptions(extraOptions)...)
}

// FindLeaks wraps goleak.Find() with Pipit's standard ignore rules.
//
// Use in complex TestMain() functions that need teardown before exit, since
// VerifyTestMain() calls os.Exit() internally.
//
// Takes extraOptions (...goleak.Option) which provides additional ignore rules.
//
// Returns error when goroutine leaks are detected.
func FindLeaks(extraOptions ...goleak.Option) error {
	return goleak.Find(buildOptions(extraOptions)...)
}

// buildOptions combines the base options, the platform's and the extra ones into a single
// slice.
//
// Takes extraOptions ([]goleak.Option) which provides additional options to append.
//
// Returns []goleak.Option which contains baseOptions, then platformOptions(), then
// extraOptions.
func buildOptions(extraOptions []goleak.Option) []goleak.Option {
	platform := platformOptions()
	options := make([]goleak.Option, 0, len(baseOptions)+len(platform)+len(extraOptions))
	options = append(options, baseOptions...)
	options = append(options, platform...)
	options = append(options, extraOptions...)
	return options
}
