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

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestBuildOptionsKeepsEveryRuleItIsGiven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		extra []goleak.Option
	}{
		{name: "no extra rule", extra: nil},
		{name: "one extra rule", extra: []goleak.Option{goleak.IgnoreCurrent()}},
		{name: "two extra rules", extra: []goleak.Option{goleak.IgnoreCurrent(), goleak.IgnoreTopFunction("example.wait")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options := buildOptions(tt.extra)

			require.Len(t, options, len(baseOptions)+len(platformOptions())+len(tt.extra),
				"the shared rules, the platform's and the caller's are all passed on")
		})
	}
}

func TestFindLeaksReportsAGoroutineThatOutlivesItsTest(t *testing.T) {
	release, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		<-release
	}()

	leaked := FindLeaks()
	close(release)
	<-stopped

	require.Error(t, leaked, "a goroutine still blocked when the check runs is a leak")
	require.NoError(t, FindLeaks(), "nothing is left once it has stopped")
}
