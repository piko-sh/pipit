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

package engine

// safeError separates a user-safe message from internal error detail. The contract with
// the host is structural, not nominal, so changes here must match the host's error shape.
type safeError struct {
	// cause is the underlying error carrying internal detail.
	cause error

	// safeMessage is the message suitable for end users in production.
	safeMessage string
}

// Error returns the internal error message from the cause chain.
//
// Returns string which is the detail that gets logged but is never shown to users in
// production.
func (e *safeError) Error() string { return e.cause.Error() }

// SafeMessage returns the user-safe message suitable for responses and error pages.
//
// Returns string which is the sanitised message.
func (e *safeError) SafeMessage() string { return e.safeMessage }

// Unwrap returns the underlying cause, preserving the chain for errors.Is and errors.As.
//
// Returns error which is the wrapped cause.
func (e *safeError) Unwrap() error { return e.cause }

// NewSafeError wraps cause with a user-safe message.
//
// Takes safeMessage (string) which is the message safe for end users.
// Takes cause (error) which is the underlying error carrying internal detail.
//
// Returns error which carries both the safe and the internal message.
func NewSafeError(safeMessage string, cause error) error {
	return &safeError{
		safeMessage: safeMessage,
		cause:       cause,
	}
}
