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
)

func TestFmtArgumentVerbsPairsOperandsWithTheirVerb(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		count  int
		want   []byte
	}{
		{name: "one verb per operand in order", format: "%d %s", count: 2, want: []byte{'d', 's'}},
		{name: "a doubled percent consumes no operand", format: "100%% done %d", count: 1, want: []byte{'d'}},
		{name: "flags do not disturb the pairing", format: "%+d %-8s", count: 2, want: []byte{'d', 's'}},
		{name: "the sharp flag marks a go-syntax operand", format: "%#v", count: 1, want: []byte{fmtVerbSharpV}},
		{name: "a star width charges its own operand", format: "%*d", count: 2, want: []byte{'*', 'd'}},
		{name: "a star precision charges its own operand", format: "%.*f", count: 2, want: []byte{'*', 'f'}},
		{name: "an explicit index selects the operand", format: "%[2]s", count: 2, want: []byte{'v', 's'}},
		{name: "operands the format never reaches keep the default verb", format: "%d", count: 3, want: []byte{'d', 'v', 'v'}},
		{name: "a format ending mid-verb assigns nothing", format: "trailing %", count: 1, want: []byte{'v'}},
		{name: "a numeric width is skipped", format: "%08.3f", count: 1, want: []byte{'f'}},
		{name: "no verbs at all leaves every operand on the default", format: "plain", count: 2, want: []byte{'v', 'v'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, fmtArgumentVerbs(tt.format, tt.count))
		})
	}
}

func TestFmtSkipOperandIndexReadsTheBracketedSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		format       string
		cursor       int
		argument     int
		wantPosition int
		wantOperand  int
	}{
		{name: "no bracket leaves both alone", format: "%d", cursor: 1, argument: 2, wantPosition: 1, wantOperand: 2},
		{name: "a bracketed index selects one-based", format: "[3]d", cursor: 0, argument: 0, wantPosition: 3, wantOperand: 2},
		{name: "an unclosed bracket leaves both alone", format: "[3d", cursor: 0, argument: 1, wantPosition: 0, wantOperand: 1},
		{name: "a non-numeric selector is skipped without effect", format: "[x]d", cursor: 0, argument: 1, wantPosition: 3, wantOperand: 1},
		{name: "a zero selector is ignored", format: "[0]d", cursor: 0, argument: 4, wantPosition: 3, wantOperand: 4},
		{name: "a cursor past the end leaves both alone", format: "ab", cursor: 5, argument: 1, wantPosition: 5, wantOperand: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			position, operand := fmtSkipOperandIndex(tt.format, tt.cursor, tt.argument)
			require.Equal(t, tt.wantPosition, position)
			require.Equal(t, tt.wantOperand, operand)
		})
	}
}

func TestFmtVerbCallsStringMethodsNamesTheStringLikeVerbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		verb byte
		want bool
	}{
		{name: "the default verb consults String", verb: 'v', want: true},
		{name: "the string verb consults String", verb: 's', want: true},
		{name: "the quoted verb consults String", verb: 'q', want: true},
		{name: "the hex verbs consult String", verb: 'x', want: true},
		{name: "the upper hex verb consults String", verb: 'X', want: true},
		{name: "the wrap verb consults Error", verb: 'w', want: true},
		{name: "the decimal verb prints the value itself", verb: 'd', want: false},
		{name: "the float verb prints the value itself", verb: 'f', want: false},
		{name: "the bool verb prints the value itself", verb: 't', want: false},
		{name: "the go-syntax verb consults GoStringer instead", verb: fmtVerbSharpV, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, fmtVerbCallsStringMethods(tt.verb))
		})
	}
}

func TestVerbSpecificationSkippersStopAtTheVerbRune(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		cursor int
		want   int
	}{
		{name: "no flags at all", format: "%d", cursor: 1, want: 1},
		{name: "every flag rune is consumed", format: "%+-# 0d", cursor: 1, want: 6},
		{name: "a numeric width is consumed", format: "%12d", cursor: 1, want: 3},
		{name: "a star width is consumed", format: "%*d", cursor: 1, want: 2},
		{name: "a precision is consumed", format: "%.3f", cursor: 1, want: 3},
		{name: "a star precision is consumed", format: "%.*f", cursor: 1, want: 3},
		{name: "a width and precision together are consumed", format: "%8.3f", cursor: 1, want: 4},
		{name: "a format ending mid-specification stops at the end", format: "%+", cursor: 1, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, skipVerbFlagsAndWidth([]rune(tt.format), tt.cursor))
		})
	}
}

func TestIndexOfRuneSearchesFromTheCursor(t *testing.T) {
	t.Parallel()

	runes := []rune("a]b]c")

	tests := []struct {
		name   string
		cursor int
		target rune
		want   int
	}{
		{name: "the first match from the start", cursor: 0, target: ']', want: 1},
		{name: "the next match past the cursor", cursor: 2, target: ']', want: 3},
		{name: "an absent rune reports minus one", cursor: 0, target: 'z', want: -1},
		{name: "a cursor past the end reports minus one", cursor: 9, target: 'a', want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, indexOfRune(runes, tt.cursor, tt.target))
		})
	}
}

func TestParsePositiveIntRefusesAnythingButDigits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		text   string
		want   int
		wantOK bool
	}{
		{name: "a single digit", text: "7", want: 7, wantOK: true},
		{name: "several digits", text: "123", want: 123, wantOK: true},
		{name: "an empty string", text: "", want: 0, wantOK: false},
		{name: "a zero is not positive", text: "0", want: 0, wantOK: false},
		{name: "a signed value is not digits", text: "-1", want: 0, wantOK: false},
		{name: "a trailing letter is not digits", text: "12a", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parsePositiveInt(tt.text)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStaticTypeIsBareInterfaceRecognisesEverySpelling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		static string
		want   bool
	}{
		{name: "the compact spelling", static: "interface{}", want: true},
		{name: "the spaced spelling", static: "interface {}", want: true},
		{name: "the alias", static: "any", want: true},
		{name: "a non-empty interface is not bare", static: "interface{ String() string }", want: false},
		{name: "a concrete type is not bare", static: "int", want: false},
		{name: "an empty static string is not bare", static: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, staticTypeIsBareInterface(tt.static))
		})
	}
}

func TestExtractPipitSentinelTypeNameQualifiesTheSourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel reflect.Type
		want     string
	}{
		{name: "a value sentinel", sentinel: reflect.TypeFor[identityTrailing](), want: "main.at"},
		{name: "a pointer sentinel keeps the star", sentinel: reflect.TypeFor[*identityTrailing](), want: "*main.at"},
		{name: "a leading sentinel is found too", sentinel: reflect.TypeFor[identityLeading](), want: "main.at"},
		{name: "an instantiated generic carries its type arguments", sentinel: reflect.TypeFor[genericPair](), want: "main.Pair[int,string]"},
		{name: "a struct with no sentinel has no name", sentinel: reflect.TypeFor[identityNone](), want: ""},
		{name: "a non-struct has no name", sentinel: reflect.TypeFor[int](), want: ""},
		{name: "a nil type has no name", sentinel: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, extractPipitSentinelTypeName(tt.sentinel))
		})
	}
}

func TestStripPipitSentinelFromTypeStringRemovesTheMarkerField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rendered string
		want     string
	}{
		{
			name:     "a sentinel after another field is cut at the separator",
			rendered: "struct { A int; _pipitID_at struct {} }",
			want:     "struct { A int }",
		},
		{
			name:     "a leading sentinel is cut at the opening brace",
			rendered: "struct { _pipitID_at struct {}; A int }",
			want:     "struct { A int }",
		},
		{name: "a string with no sentinel is unchanged", rendered: "main.Point", want: "main.Point"},
		{
			name:     "the sentinel alone leaves an empty field list",
			rendered: "struct { _pipitID_at struct {} }",
			want:     "struct { }",
		},
		{name: "a sentinel with no delimiter at all is left alone", rendered: "_pipitID_at struct", want: "_pipitID_at struct"},
		{name: "a sentinel with no opening context is left alone", rendered: "_pipitID_at }", want: "_pipitID_at }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, stripPipitSentinelFromTypeString(tt.rendered))
		})
	}
}

func TestStripPipitSentinelHandlesRealSynthesisedRenderings(t *testing.T) {
	t.Parallel()

	sentinel := reflect.StructField{Name: pipitIDFieldPrefix + "Point", Type: reflect.TypeFor[struct{}](), PkgPath: "main"}
	field := reflect.StructField{Name: "X", Type: reflect.TypeFor[int]()}

	tests := []struct {
		name   string
		fields []reflect.StructField
		want   string
	}{
		{name: "the sentinel trails the user fields", fields: []reflect.StructField{field, sentinel}, want: "struct { X int }"},
		{name: "the sentinel leads the user fields", fields: []reflect.StructField{sentinel, field}, want: "struct { X int }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			structType := reflect.StructOf(tt.fields)
			require.Equal(t, tt.want, stripPipitSentinelFromTypeString(structType.String()),
				"a synthesised struct reached through a container still has to render as valid Go")
			require.Equal(t, "[]"+tt.want, stripPipitSentinelFromTypeString(reflect.SliceOf(structType).String()),
				"a slice of one is how %T actually reaches this helper")
		})
	}
}

func TestScriptTypeStringNamesTypesTheWayTheVerbDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reflectType reflect.Type
		want        string
	}{
		{name: "a synthesised struct reports its sentinel name", reflectType: reflect.TypeFor[identityTrailing](), want: "main.at"},
		{name: "an ordinary type reports reflect's rendering", reflectType: reflect.TypeFor[int](), want: "int"},
		{name: "a slice reports its element type", reflectType: reflect.TypeFor[[]string](), want: "[]string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, scriptTypeString(tt.reflectType))
		})
	}
}

func TestTypeStringForFmtTPrefersTheRecordedStaticType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		site          *program.CallSite
		argumentIndex int
		value         any
		want          string
	}{
		{
			name: "a recorded static type wins over the runtime type",
			site: &program.CallSite{ArgumentStaticTypeStrings: []string{"MyInt"}}, value: int64(3), want: "MyInt",
		},
		{
			name: "a bare interface static type falls through to the runtime type",
			site: &program.CallSite{ArgumentStaticTypeStrings: []string{"any"}}, value: "text", want: "string",
		},
		{
			name: "an empty static type falls through to the runtime type",
			site: &program.CallSite{ArgumentStaticTypeStrings: []string{""}}, value: 3, want: "int",
		},
		{name: "an index past the recorded strings falls through", site: &program.CallSite{}, value: 3, want: "int"},
		{name: "no site at all falls through", value: true, want: "bool"},
		{name: "a nil operand reports the nil label", value: nil, want: "<nil>"},
		{
			name: "a synthesised struct reports its sentinel name", value: identityTrailing{},
			want: "main.at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, typeStringForFmtT(tt.site, tt.argumentIndex, tt.value))
		})
	}
}

func TestInterceptFmtFormatRewritesOnlyTheTypeVerb(t *testing.T) {
	t.Parallel()

	t.Run("a format with no T is left alone", func(t *testing.T) {
		t.Parallel()

		format, arguments, rewritten := interceptFmtFormat(nil, 0, "%d items", []any{3})
		require.False(t, rewritten)
		require.Equal(t, "%d items", format)
		require.Equal(t, []any{3}, arguments)
	})

	t.Run("a T outside a verb is left alone", func(t *testing.T) {
		t.Parallel()

		format, _, rewritten := interceptFmtFormat(nil, 0, "Total: %d", []any{3})
		require.False(t, rewritten, "the letter T in plain text is not a verb")
		require.Equal(t, "Total: %d", format)
	})

	t.Run("a type verb becomes a string verb carrying the type name", func(t *testing.T) {
		t.Parallel()

		arguments := []any{3}
		format, rewrittenArguments, rewritten := interceptFmtFormat(nil, 0, "%T", arguments)
		require.True(t, rewritten)
		require.Equal(t, "%s", format)
		require.Equal(t, "int", rewrittenArguments[0])
	})

	t.Run("an explicit index is preserved through the rewrite", func(t *testing.T) {
		t.Parallel()

		arguments := []any{3, "s"}
		format, rewrittenArguments, rewritten := interceptFmtFormat(nil, 0, "%[2]T", arguments)
		require.True(t, rewritten)
		require.Equal(t, "%[2]s", format)
		require.Equal(t, "string", rewrittenArguments[1])
	})

	t.Run("a doubled percent is copied through", func(t *testing.T) {
		t.Parallel()

		format, _, rewritten := interceptFmtFormat(nil, 0, "100%% of %T", []any{3})
		require.True(t, rewritten)
		require.Equal(t, "100%% of %s", format)
	})

	t.Run("other verbs keep their operand positions", func(t *testing.T) {
		t.Parallel()

		arguments := []any{3, "s"}
		format, rewrittenArguments, rewritten := interceptFmtFormat(nil, 0, "%d is %T", arguments)
		require.True(t, rewritten)
		require.Equal(t, "%d is %s", format)
		require.Equal(t, 3, rewrittenArguments[0], "the first operand still belongs to %d")
		require.Equal(t, "string", rewrittenArguments[1])
	})
}

func TestClosureTypeStringPrefersTheNamedType(t *testing.T) {
	t.Parallel()

	t.Run("a converted closure reports its named type", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, "main.Op", closureTypeString(&RuntimeClosure{namedType: "Op"}))
	})

	t.Run("a closure with neither name nor signature reports the erased shape", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, "func", closureTypeString(&RuntimeClosure{}))
	})
}

func TestFmtWrappedUnderlyingSeesThroughTheRenderer(t *testing.T) {
	t.Parallel()

	t.Run("a renderer hands back the value it wraps", func(t *testing.T) {
		t.Parallel()

		underlying, ok := fmtWrappedUnderlying(pipitFmtValue{underlying: reflect.ValueOf(4)})
		require.True(t, ok)
		require.Equal(t, 4, underlying.Interface())
	})

	t.Run("an empty renderer wraps nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := fmtWrappedUnderlying(pipitFmtValue{})
		require.False(t, ok)
	})

	t.Run("a plain value is not a wrapper", func(t *testing.T) {
		t.Parallel()

		_, ok := fmtWrappedUnderlying(4)
		require.False(t, ok)
	})
}
