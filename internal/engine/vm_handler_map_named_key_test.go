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
)

type namedSlug string

type namedSlugSmall struct {
	Title string
	Order int
}

type namedSlugLarge struct {
	Title       string
	Description string
	Summary     string
	Image       string
	Series      string
	URL         string
	Byline      string
	Label       string
	Extra       string
	Order       int
}

func TestMapLookupStringKeyGeneralNamedKey(t *testing.T) {
	t.Parallel()

	const okRegister uint8 = 5

	elements := []struct {
		name  string
		build func() reflect.Value
		want  reflect.Value
		zero  reflect.Value
	}{
		{
			name: "an inline element",
			build: func() reflect.Value {
				return reflect.ValueOf(map[namedSlug]namedSlugSmall{"k": {Title: "t", Order: 7}})
			},
			want: reflect.ValueOf(namedSlugSmall{Title: "t", Order: 7}),
			zero: reflect.ValueOf(namedSlugSmall{}),
		},
		{
			name: "an element over the inline limit",
			build: func() reflect.Value {
				return reflect.ValueOf(map[namedSlug]namedSlugLarge{"k": {Title: "t", Extra: "e", Order: 7}})
			},
			want: reflect.ValueOf(namedSlugLarge{Title: "t", Extra: "e", Order: 7}),
			zero: reflect.ValueOf(namedSlugLarge{}),
		},
	}
	handlers := []struct {
		name     string
		handler  opcodeHandler
		reportOk bool
	}{
		{name: "index", handler: handleGetMapStringGeneral, reportOk: false},
		{name: "comma-ok", handler: handleGetMapStringGeneralOk, reportOk: true},
		{name: "comma-ok jump", handler: handleGetMapStringGeneralOkThenJumpIfFalse, reportOk: true},
	}
	keys := []struct {
		name    string
		key     string
		present bool
	}{
		{name: "a present key", key: "k", present: true},
		{name: "a missing key", key: "absent", present: false},
	}

	for _, element := range elements {
		for _, handler := range handlers {
			for _, key := range keys {
				t.Run(element.name+" "+handler.name+" "+key.name, func(t *testing.T) {
					t.Parallel()
					vm, frame, registers := newMapOkFrame(t, okRegister)
					registers.General[1] = element.build()
					registers.Strings[2] = key.key
					registers.Ints[okRegister] = 9

					require.Equal(t, opContinue, handler.handler(vm, frame, registers, op(0, 1, 2)))
					want := element.zero
					if key.present {
						want = element.want
					}
					require.Equal(t, want.Interface(), registers.General[0].Interface())
					if handler.reportOk {
						require.Equal(t, boolToInt64(key.present), registers.Ints[okRegister])
					}
				})
			}
		}
	}
}
