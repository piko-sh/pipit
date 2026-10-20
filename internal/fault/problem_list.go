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

package fault

import (
	"errors"
	"strings"
)

// ProblemList reports several independent problems found by one compilation, so a host
// can show them together instead of one per attempt. Find it in an error chain with
// errors.As; errors.Is matches the sentinel of any problem.
type ProblemList struct {
	// Problems holds each problem in the order it was found.
	Problems []error
}

// Error renders each problem, separated by blank lines, so the message starts with the
// first problem as it would if that were the only one.
//
// Returns string which is the combined message.
func (p *ProblemList) Error() string {
	var builder strings.Builder
	for index, problem := range p.Problems {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(problem.Error())
	}
	return builder.String()
}

// Unwrap returns the problems so errors.Is and errors.As search each of them.
//
// Returns []error which is the problem slice.
func (p *ProblemList) Unwrap() []error {
	return p.Problems
}

// JoinProblems combines compilation errors, dropping nils and flattening nested lists.
// A single remaining problem is returned unchanged.
//
// Takes problems ([]error) which are the problems found.
//
// Returns error which is nil, the single problem, or a *ProblemList.
func JoinProblems(problems []error) error {
	flat := make([]error, 0, len(problems))
	for _, problem := range problems {
		if problem == nil {
			continue
		}
		if list, ok := problem.(*ProblemList); ok { //nolint:errorlint // flattens unwrapped lists only
			flat = append(flat, list.Problems...)
			continue
		}
		flat = append(flat, problem)
	}
	switch len(flat) {
	case 0:
		return nil
	case 1:
		return flat[0]
	default:
		return &ProblemList{Problems: flat}
	}
}

// Problems returns the entries of the first *ProblemList in err's chain, or err itself
// if no list exists.
//
// Takes err (error) which is the error to split; nil gives nil.
//
// Returns []error which holds at least one problem for a non-nil err.
func Problems(err error) []error {
	if err == nil {
		return nil
	}
	if list, ok := errors.AsType[*ProblemList](err); ok {
		return list.Problems
	}
	return []error{err}
}
