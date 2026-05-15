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

	"pipit.sh/pipit/internal/isa"
)

type sentinelStruct struct {
	Value       int
	_pipitID_at struct{}
}

type sentinelEmbedded struct {
	PipitEmbed_inner int
	Value            int
}

func TestQualifiedNameFromSentinelReadsTheMarkerField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sentinel reflect.Type
		name     string
		want     string
	}{
		{name: "a struct with no marker has no name", sentinel: reflect.TypeFor[identityNone](), want: ""},
		{name: "an empty struct has no name", sentinel: reflect.TypeFor[struct{}](), want: ""},
		{name: "a nil type has no name", sentinel: nil, want: ""},
		{name: "a plain integer has no name", sentinel: reflect.TypeFor[int](), want: ""},
		{name: "a map has no name", sentinel: reflect.TypeFor[map[string]int](), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, qualifiedNameFromSentinel(tt.sentinel))
		})
	}

	t.Run("a marked struct yields its package-qualified type name", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "engine.at", qualifiedNameFromSentinel(reflect.TypeFor[sentinelStruct]()),
			"the marker field's package path supplies the qualifier")
	})

	t.Run("the qualified form differs from the bare name by its package", func(t *testing.T) {
		t.Parallel()
		sentinel := reflect.TypeFor[sentinelStruct]()

		require.Equal(t, "at", bareSentinelName(sentinel))
		require.Equal(t, "engine."+bareSentinelName(sentinel), qualifiedNameFromSentinel(sentinel),
			"the two accessors differ only in whether the package qualifies the name")
	})
}

func TestQualifiedNameRecursesThroughTheContainerKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		container  reflect.Type
		name       string
		wantPrefix string
		wantOK     bool
	}{
		{name: "a pointer", container: reflect.TypeFor[*sentinelStruct](), wantPrefix: "*", wantOK: true},
		{name: "a slice", container: reflect.TypeFor[[]sentinelStruct](), wantPrefix: "[]", wantOK: true},
		{name: "an array", container: reflect.TypeFor[[3]sentinelStruct](), wantPrefix: "[3]", wantOK: true},
		{name: "a struct is not a container", container: reflect.TypeFor[sentinelStruct](), wantOK: false},
		{name: "a map is not a recursed container", container: reflect.TypeFor[map[string]int](), wantOK: false},
		{name: "an integer is not a container", container: reflect.TypeFor[int](), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prefix, _, ok := qualifiedNameRecurseInner(tt.container)

			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantPrefix, prefix)
			}
		})
	}

	t.Run("the prefix is carried onto the inner name", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "*engine.at", qualifiedNameFromSentinel(reflect.TypeFor[*sentinelStruct]()))
		require.Equal(t, "[]engine.at", qualifiedNameFromSentinel(reflect.TypeFor[[]sentinelStruct]()))
		require.Equal(t, "[3]engine.at", qualifiedNameFromSentinel(reflect.TypeFor[[3]sentinelStruct]()))
	})

	t.Run("a container of unmarked elements has no name", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, qualifiedNameFromSentinel(reflect.TypeFor[[]int]()),
			"an element with no marker leaves nothing for the prefix to qualify")
	})
}

func TestHasRenamedEmbeddedFieldFollowsPointers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		structType reflect.Type
		name       string
		want       bool
	}{
		{name: "a struct with a renamed embed", structType: reflect.TypeFor[sentinelEmbedded](), want: true},
		{name: "a pointer to one is followed", structType: reflect.TypeFor[*sentinelEmbedded](), want: true},
		{name: "a plain struct has none", structType: reflect.TypeFor[identityNone](), want: false},
		{name: "a non-struct has none", structType: reflect.TypeFor[int](), want: false},
		{name: "a slice has none", structType: reflect.TypeFor[[]int](), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, hasRenamedEmbeddedField(tt.structType))
		})
	}
}

func TestPipitStructFieldsNeedRewriteSpotsBothReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		structType reflect.Type
		name       string
		userFields int
		want       bool
	}{
		{
			name:       "a marker field means the count differs",
			structType: reflect.TypeFor[sentinelStruct](), userFields: 1, want: true,
		},
		{
			name:       "a renamed embed means a rewrite even when the count matches",
			structType: reflect.TypeFor[sentinelEmbedded](), userFields: 2, want: true,
		},
		{
			name:       "a plain struct with a matching count needs none",
			structType: reflect.TypeFor[identityNone](), userFields: 2, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, pipitStructFieldsNeedRewrite(tt.structType, tt.userFields))
		})
	}
}

func TestRestorePipitStructFieldUndoesTheEmbedRenaming(t *testing.T) {
	t.Parallel()

	t.Run("a renamed embed is restored and marked anonymous", func(t *testing.T) {
		t.Parallel()
		field := reflect.StructField{Name: isa.EmbeddedUnexportedPrefix + "inner", Type: reflect.TypeFor[int]()}

		restored := restorePipitStructField(field)

		require.Equal(t, "inner", restored.Name)
		require.True(t, restored.Anonymous,
			"the source declared this as an embedded field, so reflect must report it as one")
	})

	t.Run("an ordinary field is untouched", func(t *testing.T) {
		t.Parallel()
		field := reflect.StructField{Name: "Value", Type: reflect.TypeFor[int]()}

		restored := restorePipitStructField(field)

		require.Equal(t, "Value", restored.Name)
		require.False(t, restored.Anonymous)
	})
}

func TestRestorePipitStructFieldHidesTheCycleBrokenMarker(t *testing.T) {
	t.Parallel()

	cycleMarker := isa.CycleBrokenTagKey + `:"` + isa.CycleBrokenTagValue + `"`

	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "a field tagged only by the marker", tag: cycleMarker, want: ""},
		{name: "the marker appended to a tag the program wrote", tag: `json:"next" ` + cycleMarker, want: `json:"next"`},
		{name: "the marker ahead of a tag the program wrote", tag: cycleMarker + ` json:"next"`, want: `json:"next"`},
		{name: "the marker between two tags the program wrote", tag: `json:"next" ` + cycleMarker + ` xml:"next"`, want: `json:"next" xml:"next"`},
		{name: "a field the program tagged and no marker", tag: `json:"next,omitempty"`, want: `json:"next,omitempty"`},
		{name: "a field with no tag at all", tag: "", want: ""},
		{name: "a value that merely contains the key as text", tag: `json:"` + isa.CycleBrokenTagKey + `"`, want: `json:"` + isa.CycleBrokenTagKey + `"`},
		{name: "a key that merely starts with the marker key", tag: isa.CycleBrokenTagKey + `_of:"1"`, want: isa.CycleBrokenTagKey + `_of:"1"`},
		{name: "a value carrying an escaped quote", tag: `json:"a\"b" ` + cycleMarker, want: `json:"a\"b"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			field := reflect.StructField{Name: "Next", Type: reflect.TypeFor[any](), Tag: reflect.StructTag(tt.tag)}

			restored := restorePipitStructField(field)

			require.Equal(t, tt.want, string(restored.Tag),
				"the program never declared the cycle marker, so it must not be able to read one back")
		})
	}
}

func TestRestorePipitStructFieldKeepsTheTagsTheProgramCanStillRead(t *testing.T) {
	t.Parallel()

	cycleMarker := isa.CycleBrokenTagKey + `:"` + isa.CycleBrokenTagValue + `"`
	field := reflect.StructField{
		Name: "Next",
		Type: reflect.TypeFor[any](),
		Tag:  reflect.StructTag(`json:"next,omitempty" ` + cycleMarker + ` xml:"Next"`),
	}

	restored := restorePipitStructField(field)

	require.Equal(t, "next,omitempty", restored.Tag.Get("json"),
		"stripping the marker must leave every other tag readable through Get")
	require.Equal(t, "Next", restored.Tag.Get("xml"))
	require.Empty(t, restored.Tag.Get(isa.CycleBrokenTagKey),
		"the marker itself must no longer be readable")
}

func TestStrippingAMalformedTagKeepsWhatTheProgramWrote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "a pair with no closing quote", tag: `json:"next`, want: `json:"next`},
		{name: "a bare word", tag: "loose", want: "loose"},
		{name: "a good pair followed by a malformed one", tag: `json:"next" broken`, want: `json:"next" broken`},
		{name: "only spaces", tag: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, stripStructTagKey(tt.tag, isa.CycleBrokenTagKey),
				"a tag the grammar does not fit is the program's own text, so it is kept rather than discarded")
		})
	}
}

func TestSentinelTypeArgumentsRenderTheirSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sentinel reflect.Type
		name     string
	}{
		{name: "a plain sentinel has no type arguments", sentinel: reflect.TypeFor[sentinelStruct]()},
		{name: "a struct with no marker has none", sentinel: reflect.TypeFor[identityNone]()},
		{name: "an empty struct has none", sentinel: reflect.TypeFor[struct{}]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, sentinelTypeArgsSuffix(tt.sentinel))
			require.Empty(t, sentinelTypeArgs(tt.sentinel))
		})
	}
}

func TestBareSentinelNameStripsOnlyThePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sentinel reflect.Type
		name     string
		want     string
	}{
		{name: "a marked struct", sentinel: reflect.TypeFor[sentinelStruct](), want: "at"},
		{name: "a pointer to a marked struct", sentinel: reflect.TypeFor[*sentinelStruct](), want: "at"},
		{name: "an unmarked struct", sentinel: reflect.TypeFor[identityNone](), want: ""},
		{name: "a slice", sentinel: reflect.TypeFor[[]int](), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, bareSentinelName(tt.sentinel))
		})
	}
}
