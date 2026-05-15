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

package program

import (
	"fmt"
	"strconv"
	"strings"
)

// hitOperator is the comparison a hit-count breakpoint condition applies to the number of
// times the breakpoint's condition has held.
type hitOperator uint8

const (
	// hitOpNone means the breakpoint has no hit condition and fires on every hit.
	hitOpNone hitOperator = iota

	// hitOpEqual fires only on the Nth hit.
	hitOpEqual

	// hitOpGreater fires from the (N+1)th hit onwards.
	hitOpGreater

	// hitOpGreaterEqual fires from the Nth hit onwards.
	hitOpGreaterEqual

	// hitOpLess fires on hits before the Nth.
	hitOpLess

	// hitOpLessEqual fires on hits up to and including the Nth.
	hitOpLessEqual

	// hitOpModulo fires on every Nth hit.
	hitOpModulo
)

// HitCondition is a parsed hit-count condition such as ">= 3" or "% 2".
type HitCondition struct {
	// count is the operand N.
	count int

	// op is the comparison applied to the hit count.
	op hitOperator
}

// ParseHitCondition parses the hit-condition forms debugger clients send: an empty string
// (no condition), a bare number N meaning "== N", or one of ">", ">=", "<", "<=", "=="
// and "%" followed by a number, with optional spaces.
//
// Takes source (string) which is the client's hit-condition text.
//
// Returns HitCondition which is the parsed condition; the zero value for an empty source.
// Returns error when the text is not one of the accepted forms or N is not positive.
func ParseHitCondition(source string) (HitCondition, error) {
	text := strings.TrimSpace(source)
	if text == "" {
		return HitCondition{count: 0, op: hitOpNone}, nil
	}
	operator := hitOpEqual
	operators := []struct {
		token string
		op    hitOperator
	}{
		{token: ">=", op: hitOpGreaterEqual},
		{token: "<=", op: hitOpLessEqual},
		{token: "==", op: hitOpEqual},
		{token: ">", op: hitOpGreater},
		{token: "<", op: hitOpLess},
		{token: "%", op: hitOpModulo},
	}
	for _, candidate := range operators {
		if strings.HasPrefix(text, candidate.token) {
			operator = candidate.op
			text = strings.TrimSpace(text[len(candidate.token):])
			break
		}
	}
	count, err := strconv.Atoi(text)
	if err != nil {
		return HitCondition{count: 0, op: hitOpNone}, fmt.Errorf("hit condition %q: expected a number after the operator", source)
	}
	if count <= 0 {
		return HitCondition{count: 0, op: hitOpNone}, fmt.Errorf("hit condition %q: the count must be positive", source)
	}
	return HitCondition{count: count, op: operator}, nil
}

// Matches reports whether a breakpoint whose condition has now held hits times should
// pause under this hit condition.
//
// Takes hits (int) which is the number of times the breakpoint's condition has held so
// far, including the current hit.
//
// Returns bool which is true when the breakpoint should pause.
func (condition HitCondition) Matches(hits int) bool {
	switch condition.op {
	case hitOpEqual:
		return hits == condition.count
	case hitOpGreater:
		return hits > condition.count
	case hitOpGreaterEqual:
		return hits >= condition.count
	case hitOpLess:
		return hits < condition.count
	case hitOpLessEqual:
		return hits <= condition.count
	case hitOpModulo:
		return hits%condition.count == 0
	default:

		return true
	}
}
