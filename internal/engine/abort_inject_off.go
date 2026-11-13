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

//go:build !pipit_testhooks

package engine

// abortInjectBuilt is false outside the pipit_testhooks build.
//
// Every injection hook sits behind a constant-false branch the compiler removes.
const abortInjectBuilt = false

// abortInjectArmed reports whether a test has armed an injection; never true in this
// build.
//
// Returns bool which is false.
func abortInjectArmed() bool { return false }

// abortInjectFires is the injection hook; never true in this build.
//
// Returns bool which is false.
func abortInjectFires(_ abortSite) bool { return false }

// injectAllocationAbort is the bump allocators' injection hook; a no-op in this build.
func injectAllocationAbort(_ *RegisterArena) {}

// injectHandOffAbort is the hand-off injection hook; a no-op in this build.
func injectHandOffAbort(_ *RegisterArena) {}

// abortInjectedError is the injected abort's value; never raised in this build.
//
// Returns error which is nil.
func abortInjectedError(_ abortSite) error { return nil }

// injectLockWindowStop is the lock-window site's stop; never reached in this build.
//
// Returns OpResult which is opContinue.
func (*VM) injectLockWindowStop() OpResult { return opContinue }
