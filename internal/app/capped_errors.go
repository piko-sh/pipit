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

package app

import (
	"errors"
	"fmt"
)

// cappedErrors keeps errors up to a limit and counts the remainder.
type cappedErrors struct {
	// kept holds the errors within the limit, in the order they were added.
	kept []error

	// limit is the most errors kept.
	limit int

	// dropped counts the errors past the limit.
	dropped int
}

// newCappedErrors returns a collector that keeps up to limit errors.
//
// Takes limit (int) which is the most errors kept.
//
// Returns *cappedErrors which is empty.
func newCappedErrors(limit int) *cappedErrors {
	return &cappedErrors{kept: nil, limit: limit, dropped: 0}
}

// full reports whether the next error will only be counted.
//
// Returns bool which is true once limit errors are kept.
func (c *cappedErrors) full() bool {
	return len(c.kept) >= c.limit
}

// empty reports whether no error has been kept.
//
// Returns bool which is true before the first error is added.
func (c *cappedErrors) empty() bool {
	return len(c.kept) == 0
}

// add keeps err while the limit allows and counts it otherwise.
//
// Takes err (error) which is the error to collect; nil is ignored.
func (c *cappedErrors) add(err error) {
	if err == nil {
		return
	}
	if c.full() {
		c.dropped++
		return
	}
	c.kept = append(c.kept, err)
}

// join returns the kept errors in order, ending with one that wraps tooMany and says how
// many were left out.
//
// Takes tooMany (error) which marks that errors were left out.
//
// Returns error which is nil when nothing was added.
func (c *cappedErrors) join(tooMany error) error {
	if c.dropped == 0 {
		return errors.Join(c.kept...)
	}
	return errors.Join(append(c.kept, fmt.Errorf("%w (%d more not shown)", tooMany, c.dropped))...)
}
