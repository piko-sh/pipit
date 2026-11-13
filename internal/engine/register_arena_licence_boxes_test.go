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

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestBoxShareCallKeepsValuesThatCannotBeShared(t *testing.T) {
	describe := func(value any) int { return 0 }
	site := &program.CallSite{}
	addressable := reflect.ValueOf(new(string)).Elem()
	cases := []struct {
		call boxShareCall
		site *program.CallSite
		raw  reflect.Value
		name string
	}{
		{name: "no arena", call: newBoxShareCall(nil, reflect.ValueOf(describe)), site: site, raw: addressable},
		{name: "a spread call", call: newBoxShareCall(NewRegisterArena(), reflect.ValueOf(describe)), site: &program.CallSite{IsEllipsisSpread: true}, raw: addressable},
		{name: "no value", call: newBoxShareCall(NewRegisterArena(), reflect.ValueOf(describe)), site: site, raw: reflect.Value{}},
		{name: "an interface", call: newBoxShareCall(NewRegisterArena(), reflect.ValueOf(describe)), site: site, raw: reflect.ValueOf(new(any)).Elem()},
		{name: "a value that is not addressable", call: newBoxShareCall(NewRegisterArena(), reflect.ValueOf(describe)), site: site, raw: reflect.ValueOf("text")},
		{name: "an arena without a licence", call: newBoxShareCall(NewRegisterArena(), reflect.ValueOf(describe)), site: site, raw: addressable},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, shared := testCase.call.share(testCase.site, 0, isa.RegisterString, testCase.raw)
			require.False(t, shared)
			require.Equal(t, testCase.raw, got)
		})
	}
}

func TestTrueBoxIsOnlyAScalarRegistersBox(t *testing.T) {
	arena := NewRegisterArena()
	heap := reflect.ValueOf(new(string)).Elem()
	require.False(t, arena.trueBox(heap, isa.RegisterString), "a heap string is no arena box")
	require.False(t, arena.trueBox(heap, isa.RegisterGeneral), "a general register's value is never shared")
}

func TestBoxShareNoteSkipsAnArenaWithoutALicence(t *testing.T) {
	call := newBoxShareCall(NewRegisterArena(), reflect.ValueOf(func(any) {}))
	call.note(reflect.ValueOf("text"))
	require.False(t, call.decided, "noting decides nothing")
}
