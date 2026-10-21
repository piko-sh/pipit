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

package opnames

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const retiredNamesPath = "testdata/retired_operation_names.txt"

const moduleRoot = "../../.."

var retiredNameSkippedDirectories = []string{
	".git", ".bench", ".piko-extract", "bin", "node_modules", "testdata",
	"sdk/selfhost", "sdk/stdlib", "tests/bench/cross_language",
}

var tierConstantPrefixes = []string{"Op", "SubOp", "SubOpTier2", "SubOpTier3"}

var symbolInitialisms = map[string]bool{"IIFE": true}

var derivedSymbolPrefixes = []string{
	"Op", "SubOp", "SubOpTier2", "SubOpTier3", "handle", "handler", "Handle", "exit", "processExit",
	"asmCall", "asmCallHandle", "goSymbol",
}

var retiredNameDeclarations = map[string]string{
	"AddComplexConstant":                                  "Adds a value to the complex constant pool; not ADD_COMPLEX.",
	"AddFloatConstant":                                    "Adds a value to the float constant pool; not ADD_FLOAT.",
	"AddIntConstant":                                      "Adds a value to the int constant pool; not ADD_INT.",
	"AddUintConstant":                                     "Adds a value to the uint constant pool; not ADD_UINT.",
	"ArenaSliceStringIndex":                               "Offset of the arena's string-slice bump index.",
	"ArenaStringIndex":                                    "Offset of the arena's string bump index.",
	"BenchmarkMapSetArenaString":                          "Benchmarks Go's m[k] = v with an arena string.",
	"CompiledFunctionAddComplexConstants":                 "Test of the pool method AddComplexConstant.",
	"CompiledFunctionAddFloatConstants":                   "Test of the pool method AddFloatConstant.",
	"CompiledFunctionAddIntConstants":                     "Test of the pool method AddIntConstant.",
	"CompiledFunctionAddUintConstants":                    "Test of the pool method AddUintConstant.",
	"CopySliceFloatToSliceFloat":                          "Call-argument copy between Registers.SlicesFloat entries; names the bank fields.",
	"CopySliceIntToSliceInt":                              "Call-argument copy between Registers.SlicesInt entries; names the bank fields.",
	"CopySliceUintToSliceUint":                            "Call-argument copy between Registers.SlicesUint entries; names the bank fields.",
	"ErrCompileChannelReceiveRequiresGeneral":             "Go's channel receive.",
	"PackTypedSliceStringToGeneral":                       "Wraps a typed slice as a general value of a given type; serves BOX_AS_TYPE and the BOX_SLICE_* operations.",
	"SlicesStringIndex":                                   "The arena's string-slice bump index.",
	"StringIndex":                                         "The arena's string bump index.",
	"TestArenaBytesToStringAtLengthBoundaries":            "Tests arenaBytesToString.",
	"TestArenaRuneToStringAcrossEncodedWidths":            "Tests arenaRuneToString.",
	"TestChannelReceiveUnblocksOnContextCancel":           "Tests Go's channel receive.",
	"TestChannelSendReceiveAndSelect":                     "Tests Go's channel send, receive and select.",
	"TestChannelSendUnblocksOnContextCancel":              "Tests Go's channel send.",
	"TestEvalStringIndexSliceAndConcat":                   "Tests Go's string indexing.",
	"TestEvalSubIntSliceCallBoundary":                     "Slices of sub-word integers (int8 to int32).",
	"TestEvalSubIntSliceMutationPropagation":              "Slices of sub-word integers (int8 to int32).",
	"TestIsTier2ReturnIgnoresTheValueCount":               "Tests isTier2Return.",
	"TestMapDelete":                                       "Tests Go's delete builtin.",
	"TestPackTypedSliceHelpersWrapWithoutCopyingElements": "Tests the packTypedSlice helpers.",
	"TestSaturatingFloatToIntConvertDeclinesOtherShapes":  "Tests saturatingFloatToIntConvert.",
	"TestSaturatingFloatToIntConvertMatchesGoCasts":       "Tests saturatingFloatToIntConvert.",
	"TestStringIndexSliceAndConcat":                       "Tests Go's string indexing.",
	"TestUintBitNot":                                      "Tests Go's ^x on unsigned integers.",
	"addFloatConst":                                       "Test builder method adding a float constant to the pool.",
	"addInt":                                              "Test helper adding two Go ints.",
	"addIntConst":                                         "Test builder method adding an int constant to the pool.",
	"appendBoolInPlace":                                   "Appends a bool in place; bool is the Go type (APPEND_BOOL_WIDE's helper).",
	"appendBoolSliceFastPath":                             "Appends to a []bool; bool is the Go type (APPEND_BOOL_WIDE's helper).",
	"arenaAppendBool":                                     "Appends to an arena []bool; bool is the Go type.",
	"arenaBytesToString":                                  "Arena conversion of Go's string(b); names what it does, used by several operations.",
	"arenaBytesToStringAvgBytes":                          "Sizing of arenaBytesToString.",
	"arenaRuneToString":                                   "Arena conversion of Go's string(r); names what it does.",
	"arenaRuneToStringMaxBytes":                           "Sizing of arenaRuneToString.",
	"arenaSliceStringIndex":                               "The arena's string-slice bump index.",
	"arenaStringIndex":                                    "The arena's string bump index.",
	"blockingChannelSend":                                 "A blocking Go channel send.",
	"compileChannelReceiveCommaOk":                        "Compiles Go's v, ok := <-ch.",
	"compileMapIndex":                                     "Compiles Go's m[k].",
	"deferredMapDelete":                                   "Go's deferred delete(m, k).",
	"elideRedundantBitAndAfterTruncate":                   "A bitwise AND after a truncation, of either bank.",
	"elideRedundantTruncateAfterBitAnd":                   "A truncation after a bitwise AND, of either bank.",
	"intToUintArithOp":                                    "Maps an int64-bank arithmetic operation to its uint64 twin.",
	"intToUintCmpOp":                                      "Maps an int64-bank comparison to its uint64 twin.",
	"isStringToBytesConversion":                           "Go's []byte(s).",
	"isTier2Return":                                       "Tier-2 RETURN (the tier and the current name).",
	"mapIndexStringKey":                                   "Go's m[k] with a string key; serves several operations.",
	"mapSetFast64Pointer":                                 "Go's m[k] = v through a 64-bit key; serves several operations.",
	"mapSetFast64PointerKeyed":                            "Go's m[k] = v through a 64-bit key; serves several operations.",
	"mapSetReflectSlow":                                   "Go's m[k] = v through reflect; serves several operations.",
	"mapSetStringProbeFirst":                              "Go's m[k] = v with a string key; serves several operations.",
	"matchBitAndAfterTruncate":                            "A bitwise AND after a truncation, of either bank.",
	"matchesTruncateAfterBitAnd":                          "A truncation after a bitwise AND, of either bank.",
	"minBitAndAfterTruncateWindow":                        "A bitwise AND after a truncation, of either bank.",
	"minTruncateAfterBitAndWindow":                        "A truncation after a bitwise AND, of either bank.",
	"nativeBoxFloatBoxThenWrite":                          "A native Go function in a test that boxes a float.",
	"packInterfaceMethodSets":                             "Serialises interface method sets (pack as in encode).",
	"packTypedSliceBoolToGeneral":                         "Wraps a typed slice as a general value of a given type; serves several operations.",
	"packTypedSliceFloatToGeneral":                        "Wraps a typed slice as a general value of a given type; serves several operations.",
	"packTypedSliceToGeneral":                             "Wraps a typed slice as a general value of a given type; serves several operations.",
	"packTypedSliceUintToGeneral":                         "Wraps a typed slice as a general value of a given type; serves several operations.",
	"placeStringToBytes":                                  "Places the result of Go's []byte(s).",
	"routeChannelSend":                                    "The hand-off route of a Go channel send.",
	"saturatingFloatToIntConvert":                         "Go's saturating float-to-int conversion; serves several operations.",
	"saturatingFloatToIntConvertFallback":                 "Go's saturating float-to-int conversion.",
	"selectChannelReceive":                                "A receive case of Go's select.",
	"selectChannelSend":                                   "A send case of Go's select.",
	"shiftRightUint":                                      "Shifts a uint64 range bound as the VM does, in range analysis.",
	"tier2Return":                                         "Tier-2 RETURN (the tier and the current name).",
	"tryChannelReceiveCommaOkShortVar":                    "Compiles Go's v, ok := <-ch.",
	"tryFoldStringIndex":                                  "Folds Go's s[i] on a constant string.",
	"unpackInterfaceMethodSets":                           "Deserialises interface method sets (unpack as in decode).",
	"writeChannelReceiveResult":                           "Stores the result of a Go channel receive, for RECEIVE_FROM_CHANNEL and select.",
}

func symbolStem(name string) string {
	var stem strings.Builder
	for word := range strings.SplitSeq(name, "_") {
		if symbolInitialisms[word] || word == "" {
			stem.WriteString(word)
			continue
		}
		stem.WriteString(word[:1])
		stem.WriteString(strings.ToLower(word[1:]))
	}
	return stem.String()
}

func stemWords(identifier string) []string {
	runes := []rune(identifier)
	var words []string
	start := 0
	for index := 1; index < len(runes); index++ {
		letter, previous := runes[index], runes[index-1]
		if !unicode.IsUpper(letter) {
			continue
		}
		nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && nextIsLower) {
			words = append(words, string(runes[start:index]))
			start = index
		}
	}
	return append(words, string(runes[start:]))
}

func wordRunAt(words, stem []string, position int) bool {
	if position+len(stem) > len(words) {
		return false
	}
	for offset, word := range stem {
		got := words[position+offset]
		if position+offset == 0 {
			got = strings.ToUpper(got[:1]) + got[1:]
		}
		if got != word {
			return false
		}
	}
	return true
}

func retiredStemsIn(identifier string, retired, current [][]string) []string {
	words := stemWords(identifier)
	covered := make([]bool, len(words))
	for _, prefix := range tierConstantPrefixes {
		if prefixWords := stemWords(prefix); wordRunAt(words, prefixWords, 0) {
			for offset := range prefixWords {
				covered[offset] = true
			}
		}
	}
	for position := range words {
		for _, stem := range current {
			if wordRunAt(words, stem, position) {
				for offset := range stem {
					covered[position+offset] = true
				}
			}
		}
	}
	var found []string
	for position := range words {
		for _, stem := range retired {
			if !wordRunAt(words, stem, position) || !slices.Contains(covered[position:position+len(stem)], false) {
				continue
			}
			if len(stem) == 1 && !hasDerivedSymbolShape(identifier, stem[0]) {
				continue
			}
			found = append(found, strings.Join(stem, ""))
		}
	}
	return found
}

func hasDerivedSymbolShape(identifier, stem string) bool {
	for _, prefix := range derivedSymbolPrefixes {
		rest, ok := strings.CutPrefix(identifier, prefix+stem)
		if ok && (rest == "" || rest == "Body" || rest == "Exit") {
			return true
		}
	}
	return false
}

func loadRetiredStems(t *testing.T) [][]string {
	t.Helper()
	file, err := os.Open(retiredNamesPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	var stems [][]string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		stems = append(stems, stemWords(symbolStem(line)))
	}
	require.NoError(t, scanner.Err())
	return stems
}

func declaredNames(file *ast.File) []string {
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.FuncDecl:
			names = append(names, declaration.Name.Name)
		case *ast.TypeSpec:
			names = append(names, declaration.Name.Name)
		case *ast.ValueSpec:
			for _, name := range declaration.Names {
				names = append(names, name.Name)
			}
		case *ast.StructType:
			for _, field := range declaration.Fields.List {
				for _, name := range field.Names {
					names = append(names, name.Name)
				}
			}
		case *ast.InterfaceType:
			for _, method := range declaration.Methods.List {
				for _, name := range method.Names {
					names = append(names, name.Name)
				}
			}
		}
		return true
	})
	return names
}

func TestNoDeclarationQuotesARetiredOperationName(t *testing.T) {
	t.Parallel()
	retired := loadRetiredStems(t)
	var current [][]string
	for _, row := range isa.AllSpecs() {
		if row.Name != "" {
			current = append(current, stemWords(symbolStem(row.Name)))
		}
	}
	quoting := map[string][]string{}
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if slices.Contains(retiredNameSkippedDirectories, filepath.ToSlash(relative)) ||
				slices.Contains(retiredNameSkippedDirectories, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, name := range declaredNames(file) {
			if _, reviewed := retiredNameDeclarations[name]; reviewed {
				continue
			}
			if stems := retiredStemsIn(name, retired, current); len(stems) > 0 {
				quoting[name+" ("+filepath.ToSlash(relative)+")"] = stems
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Emptyf(t, quoting,
		"these declarations quote a retired operation name (%s); rename them to the operation's current "+
			"stem, or, when the words mean something else, list them in retiredNameDeclarations with the reason",
		retiredNamesPath)
}

func TestRetiredStemsAreFoundOnlyWhereNoCurrentStemExplainsThem(t *testing.T) {
	t.Parallel()
	retired := [][]string{stemWords("BitAnd"), stemWords("AddIntConst"), {"Ext"}, stemWords("Tier2Return")}
	current := [][]string{stemWords("BitAndInt64"), stemWords("AddInt64Constant"), {"Extension"}, {"Return"}}
	tests := []struct {
		identifier string
		want       []string
	}{
		{identifier: "handleBitAndInt64", want: nil},
		{identifier: "elideRedundantBitAndAfterTruncate", want: []string{"BitAnd"}},
		{identifier: "addIntConstOperand", want: []string{"AddIntConst"}},
		{identifier: "OpAddInt64Constant", want: nil},
		{identifier: "handleExt", want: []string{"Ext"}},
		{identifier: "handleExtBody", want: []string{"Ext"}},
		{identifier: "readExtWord", want: nil},
		{identifier: "handleExtension", want: nil},
		{identifier: "SubOpTier2Return", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, retiredStemsIn(tt.identifier, retired, current))
		})
	}
}

func TestSymbolStemMatchesTheDerivedSymbols(t *testing.T) {
	t.Parallel()
	checked := 0
	for _, row := range isa.AllSpecs() {
		if row.Name == "" {
			continue
		}
		stem := symbolStem(row.Name)
		if row.Handler != "" {
			require.Equal(t, "handle"+stem, row.Handler, "the stem of %s", row.Name)
			checked++
		}
		if row.AsmBody != "" {
			require.Equal(t, "handler"+stem, row.AsmBody, "the stem of %s", row.Name)
			checked++
		}
	}
	require.Positive(t, checked, "isa derives no handler symbols to compare with")
}
