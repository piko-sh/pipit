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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHitCondition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		source  string
		want    HitCondition
		wantErr bool
	}{
		{name: "empty means none", source: "", want: HitCondition{count: 0, op: hitOpNone}},
		{name: "bare number means equal", source: "5", want: HitCondition{count: 5, op: hitOpEqual}},
		{name: "spaces are ignored", source: "  >= 3 ", want: HitCondition{count: 3, op: hitOpGreaterEqual}},
		{name: "greater", source: ">2", want: HitCondition{count: 2, op: hitOpGreater}},
		{name: "less", source: "< 4", want: HitCondition{count: 4, op: hitOpLess}},
		{name: "less or equal", source: "<=4", want: HitCondition{count: 4, op: hitOpLessEqual}},
		{name: "explicit equal", source: "== 7", want: HitCondition{count: 7, op: hitOpEqual}},
		{name: "modulo", source: "% 3", want: HitCondition{count: 3, op: hitOpModulo}},
		{name: "missing number", source: ">=", wantErr: true},
		{name: "zero is refused", source: "0", wantErr: true},
		{name: "negative is refused", source: "% -2", wantErr: true},
		{name: "garbage is refused", source: "every third", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseHitCondition(tc.source)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestHitConditionMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		condition HitCondition
		hits      []int
		want      []bool
	}{
		{name: "none matches every hit", condition: HitCondition{count: 0, op: hitOpNone}, hits: []int{1, 2, 3}, want: []bool{true, true, true}},
		{name: "equal matches once", condition: HitCondition{count: 2, op: hitOpEqual}, hits: []int{1, 2, 3}, want: []bool{false, true, false}},
		{name: "greater", condition: HitCondition{count: 2, op: hitOpGreater}, hits: []int{1, 2, 3}, want: []bool{false, false, true}},
		{name: "greater or equal", condition: HitCondition{count: 2, op: hitOpGreaterEqual}, hits: []int{1, 2, 3}, want: []bool{false, true, true}},
		{name: "less", condition: HitCondition{count: 2, op: hitOpLess}, hits: []int{1, 2, 3}, want: []bool{true, false, false}},
		{name: "less or equal", condition: HitCondition{count: 2, op: hitOpLessEqual}, hits: []int{1, 2, 3}, want: []bool{true, true, false}},
		{name: "modulo", condition: HitCondition{count: 2, op: hitOpModulo}, hits: []int{1, 2, 3, 4}, want: []bool{false, true, false, true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for index, hits := range tc.hits {
				require.Equal(t, tc.want[index], tc.condition.Matches(hits), "hit %d", hits)
			}
		})
	}
}
