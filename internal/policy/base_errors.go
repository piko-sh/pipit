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

import "errors"

var (
	// ErrInvalidConfig reports an invalid configuration.
	ErrInvalidConfig = errors.New("pipit: invalid configuration")

	// ErrUnavailable reports a capability the current platform cannot provide.
	ErrUnavailable = errors.New("pipit: capability unavailable")

	// ErrBusy reports a resource that is occupied.
	ErrBusy = errors.New("pipit: resource busy")

	// ErrClosed reports a resource that is closed or already consumed.
	ErrClosed = errors.New("pipit: resource closed")

	// ErrLimit reports an exceeded resource limit.
	ErrLimit = errors.New("pipit: resource limit exceeded")
)
