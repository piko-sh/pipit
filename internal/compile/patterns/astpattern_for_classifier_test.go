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

package patterns

import (
	"context"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func parseLoop(t *testing.T, source string) *ast.ForStmt {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", "package main\nfunc f() {\n"+source+"\n}\n", 0)
	require.NoError(t, err, "the fixture loop must parse")

	declaration, ok := file.Decls[0].(*ast.FuncDecl)
	require.True(t, ok, "the fixture declares a function")
	for _, statement := range declaration.Body.List {
		if loop, isLoop := statement.(*ast.ForStmt); isLoop {
			return loop
		}
	}
	t.Fatal("the fixture must contain a for statement")
	return nil
}

func TestClassifyingALoopInitClauseAcceptsOnlyACounterStartingAtZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   InitShape
	}{
		{name: "a counter declared at zero", source: "for i := 0; i < 4; i++ {}", want: ForInitConstZeroDecl},
		{name: "a counter assigned zero", source: "i := 0\n_ = i\nfor i = 0; i < 4; i++ {}", want: ForInitConstZeroAssign},
		{name: "no init clause at all", source: "for ; true; {}", want: forInitNone},
		{name: "a counter starting at one", source: "for i := 1; i < 4; i++ {}", want: forInitOther},
		{name: "a counter starting at a named constant", source: "const n = 0\nfor i := n; i < 4; i++ {}", want: forInitOther},
		{name: "two counters declared together", source: "for i, j := 0, 0; i < 4; i++ { _ = j }", want: forInitOther},
		{name: "an init clause that is not an assignment", source: "for f(); ; {}", want: forInitOther},
		{name: "an init target that is not a plain name", source: "a := make([]int, 4)\nfor a[0] = 0; a[0] < 4; a[0]++ {}", want: forInitOther},
		{name: "a counter started with a compound operator", source: "i := 1\nfor i += 0; i < 4; i++ {}", want: forInitOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop := parseLoop(t, tt.source)

			require.Equal(t, tt.want, classifyForStmtInit(loop.Init))
		})
	}
}

func TestClassifyingALoopConditionSeparatesLengthBoundsFromConstantBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		want      CondShape
		wantBound int64
		wantKey   bool
	}{
		{name: "a counter below a length", source: "for i := 0; i < len(a); i++ {}", want: ForCondLtLen, wantKey: true},
		{name: "a counter at or below a length", source: "for i := 0; i <= len(a); i++ {}", want: forCondLeLen, wantKey: true},
		{name: "a counter below a literal", source: "for i := 0; i < 8; i++ {}", want: ForCondLtConst, wantBound: 8, wantKey: true},
		{name: "a counter at or below a literal", source: "for i := 0; i <= 8; i++ {}", want: forCondLeConst, wantBound: 8, wantKey: true},
		{name: "a counter above a length", source: "for i := 0; i > len(a); i++ {}", want: forCondOther, wantKey: true},

		{name: "a counter not equal to a literal", source: "for i := 0; i != 8; i++ {}", want: forCondOther, wantBound: 8, wantKey: true},
		{name: "no condition at all", source: "for i := 0; ; i++ {}", want: forCondOther},
		{name: "a condition that is not a comparison", source: "for i := 0; ok; i++ {}", want: forCondOther},
		{name: "a left-hand side that is not a plain name", source: "for i := 0; a[0] < 8; i++ {}", want: forCondOther},
		{name: "a bound that is neither a length nor a constant", source: "for i := 0; i < n; i++ {}", want: forCondOther},
		{name: "a length of something that is not a plain name", source: "for i := 0; i < len(a[0]); i++ {}", want: forCondOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop := parseLoop(t, tt.source)

			var fingerprint Fingerprint
			got := classifyForStmtCond(loop.Cond, nil, &fingerprint)

			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantBound, fingerprint.ConstUpperBound,
				"only a constant bound has a value to record for the unroller to gate on")
			if tt.wantKey {
				require.NotNil(t, fingerprint.KeyExprs[ForStmtKeyExprUpperBound],
					"a recognised bound is handed to the recogniser rather than re-walked")
			}
		})
	}
}

func TestClassifyingALoopPostClauseAcceptsOnlyASingleStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   postShape
	}{
		{name: "a counter incremented", source: "for i := 0; i < 4; i++ {}", want: ForPostPlusPlus},
		{name: "a counter decremented", source: "for i := 4; i > 0; i-- {}", want: forPostMinusMinus},
		{name: "no post clause at all", source: "for i := 0; i < 4; {}", want: forPostOther},
		{name: "a counter advanced by an assignment", source: "for i := 0; i < 4; i += 2 {}", want: forPostOther},
		{name: "a step of something that is not a plain name", source: "a := make([]int, 4)\nfor i := 0; i < 4; a[0]++ { _ = i }", want: forPostOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop := parseLoop(t, tt.source)

			require.Equal(t, tt.want, classifyForStmtPost(loop.Post))
		})
	}
}

func TestClassifyingALoopBodySeparatesTheShapesRecognisersCareAbout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		want      BodyShape
		wantCount uint8
	}{
		{name: "an empty body", source: "for i := 0; i < 4; i++ {}", want: ForBodyEmpty},
		{name: "an accumulation of a plain value", source: "for i := 0; i < 4; i++ { s += n }", want: ForBodySingleAssign, wantCount: 1},
		{name: "an accumulation of an indexed value", source: "for i := 0; i < 4; i++ { s += a[i] }", want: ForBodySingleAssignIndex, wantCount: 1},
		{name: "a sum of two indexed values", source: "for i := 0; i < 4; i++ { s += a[i] + b[i] }", want: ForBodySingleAssignBinaryIndexIndex, wantCount: 1},
		{name: "a product of two indexed values written to an element", source: "for i := 0; i < 4; i++ { d[i] = a[i] * b[i] }", want: ForBodySingleAssignBinaryIndexIndex, wantCount: 1},
		{name: "an indexed value scaled by a constant", source: "for i := 0; i < 4; i++ { d[i] = a[i] * 2 }", want: ForBodySingleAssignBinaryIndex, wantCount: 1},
		{name: "a constant scaled by an indexed value", source: "for i := 0; i < 4; i++ { d[i] = 2 * a[i] }", want: ForBodySingleAssignBinaryIndex, wantCount: 1},
		{name: "an element copied from another slice", source: "for i := 0; i < 4; i++ { d[i] = a[i] }", want: ForBodySingleAssignIndex, wantCount: 1},
		{name: "a maximum update", source: "for i := 0; i < 4; i++ { if a[i] > m { m = a[i] } }", want: ForBodySingleIfMaxMin, wantCount: 1},
		{name: "a minimum update", source: "for i := 0; i < 4; i++ { if a[i] < m { m = a[i] } }", want: ForBodySingleIfMaxMin, wantCount: 1},
		{name: "a maximum update with an inclusive comparison", source: "for i := 0; i < 4; i++ { if a[i] >= m { m = a[i] } }", want: ForBodySingleIfMaxMin, wantCount: 1},
		{name: "two statements", source: "for i := 0; i < 4; i++ { s += a[i]; t += b[i] }", want: ForBodyOther, wantCount: 2},
		{name: "a statement that is neither an assignment nor an if", source: "for i := 0; i < 4; i++ { f() }", want: ForBodyOther, wantCount: 1},
		{name: "an if with an else branch", source: "for i := 0; i < 4; i++ { if a[i] > m { m = a[i] } else { m = 0 } }", want: ForBodyOther, wantCount: 1},
		{name: "an if with its own init clause", source: "for i := 0; i < 4; i++ { if v := a[i]; v > m { m = v } }", want: ForBodyOther, wantCount: 1},
		{name: "an if whose condition is not a comparison", source: "for i := 0; i < 4; i++ { if ok { m = 1 } }", want: ForBodyOther, wantCount: 1},
		{name: "an if comparing two plain names", source: "for i := 0; i < 4; i++ { if n > m { m = n } }", want: ForBodyOther, wantCount: 1},
		{name: "an if whose body updates through a compound operator", source: "for i := 0; i < 4; i++ { if a[i] > m { m += a[i] } }", want: ForBodyOther, wantCount: 1},
		{name: "an if whose body holds two statements", source: "for i := 0; i < 4; i++ { if a[i] > m { m = a[i]; n = i } }", want: ForBodyOther, wantCount: 1},
		{name: "an assignment of two values at once", source: "for i := 0; i < 4; i++ { m, n = a[i], i }", want: ForBodyOther, wantCount: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop := parseLoop(t, tt.source)

			var fingerprint Fingerprint
			classifyForStmtBody(loop.Body, &fingerprint)

			require.Equal(t, tt.want, fingerprint.BodyShape)
			require.Equal(t, tt.wantCount, fingerprint.BodyStmtCount,
				"the statement count is part of the fingerprint, so it has to follow the body it describes")
		})
	}
}

func TestClassifyingALoopBodyExposesTheOperandsARecogniserNeeds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantAssign   bool
		wantBinaries bool
	}{
		{name: "a sum of two indexed values", source: "for i := 0; i < 4; i++ { s += a[i] * b[i] }", wantAssign: true, wantBinaries: true},
		{name: "an indexed value scaled by a constant", source: "for i := 0; i < 4; i++ { d[i] = a[i] * 2 }", wantAssign: true, wantBinaries: true},
		{name: "an element copied from another slice", source: "for i := 0; i < 4; i++ { d[i] = a[i] }", wantAssign: true, wantBinaries: false},
		{name: "a maximum update", source: "for i := 0; i < 4; i++ { if a[i] > m { m = a[i] } }", wantAssign: true, wantBinaries: true},
		{name: "a body no recogniser claims", source: "for i := 0; i < 4; i++ { f() }", wantAssign: false, wantBinaries: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop := parseLoop(t, tt.source)

			var fingerprint Fingerprint
			classifyForStmtBody(loop.Body, &fingerprint)

			if tt.wantAssign {
				require.NotNil(t, fingerprint.KeyExprs[forStmtKeyExprAssignLHS])
				require.NotNil(t, fingerprint.KeyExprs[ForStmtKeyExprAssignRHS])
			} else {
				require.Nil(t, fingerprint.KeyExprs[forStmtKeyExprAssignLHS])
			}
			if tt.wantBinaries {
				require.NotNil(t, fingerprint.KeyExprs[forStmtKeyExprBinaryLHS])
				require.NotNil(t, fingerprint.KeyExprs[forStmtKeyExprBinaryRHS])
			} else {
				require.Nil(t, fingerprint.KeyExprs[forStmtKeyExprBinaryLHS])
			}
		})
	}
}

func TestClassifyingAWholeLoopFillsEveryShapeOfItsFingerprint(t *testing.T) {
	t.Parallel()

	loop := parseLoop(t, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")

	fingerprint := classifyForStmt(loop, nil)

	require.Equal(t, FingerprintSignature{
		InitShape: ForInitConstZeroDecl,
		CondShape: ForCondLtLen,
		PostShape: ForPostPlusPlus,
		BodyShape: ForBodySingleAssignBinaryIndexIndex,
	}, fingerprint.signature(),
		"the signature is the dispatch key, so every shape of the canonical dot-product loop has to land in it")
	require.Equal(t, uint8(1), fingerprint.BodyStmtCount)
}

func TestResolvingAConstantBoundPrefersWhatTheTypeCheckerFolded(t *testing.T) {
	t.Parallel()

	expression, err := parser.ParseExpr("n")
	require.NoError(t, err)

	folded := &types.Info{Types: map[ast.Expr]types.TypeAndValue{
		expression: {Type: types.Typ[types.Int], Value: constant.MakeInt64(8)},
	}}

	value, ok := resolveIntegerConstant(expression, folded)

	require.True(t, ok, "a named constant is only a constant once the type-checker has folded it")
	require.Equal(t, int64(8), value)
}

func TestResolvingAConstantBoundFallsBackToABareLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   int64
		wantOK bool
	}{
		{name: "a decimal literal", source: "8", want: 8, wantOK: true},
		{name: "a hexadecimal literal", source: "0x10", want: 16, wantOK: true},
		{name: "a literal with digit separators", source: "1_000", want: 1000, wantOK: true},
		{name: "a floating-point literal is not an integer bound", source: "8.0", wantOK: false},
		{name: "a string literal is not an integer bound", source: "\"8\"", wantOK: false},
		{name: "a name the checker never folded", source: "n", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expression, err := parser.ParseExpr(tt.source)
			require.NoError(t, err)

			value, ok := resolveIntegerConstant(expression, nil)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, value)
		})
	}
}

type countingRecogniser struct {
	name       string
	priority   int
	signatures []FingerprintSignature
	matches    bool
	asked      *[]string
}

func (r *countingRecogniser) Name() string { return r.name }

func (r *countingRecogniser) Priority() int { return r.priority }

func (r *countingRecogniser) AcceptedSignatures() []FingerprintSignature { return r.signatures }

func (r *countingRecogniser) Match(RecogniseContext, *ast.ForStmt, Fingerprint) (any, bool) {
	*r.asked = append(*r.asked, r.name)
	return r.name, r.matches
}

func (r *countingRecogniser) Emit(context.Context, Emitter, *ast.ForStmt, any) (program.VarLocation, error) {
	return program.VarLocation{}, nil
}

func dotProductSignature() FingerprintSignature {
	return FingerprintSignature{
		InitShape: ForInitConstZeroDecl,
		CondShape: ForCondLtLen,
		PostShape: ForPostPlusPlus,
		BodyShape: ForBodySingleAssignBinaryIndexIndex,
	}
}

func TestAnEmptyRegistryRecognisesNothing(t *testing.T) {
	t.Parallel()

	loop := parseLoop(t, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")

	matched, matchToken, ok := NewRegistry().TryRecogniseForStmt(RecogniseContext{}, loop)

	require.False(t, ok, "a registry with no recognisers has nothing to dispatch to")
	require.Nil(t, matched)
	require.Nil(t, matchToken)
}

func TestARegistryOnlyAsksTheRecognisersIndexedUnderTheLoopSignature(t *testing.T) {
	t.Parallel()

	var asked []string
	registry := NewRegistry()
	registry.Register(&countingRecogniser{name: "matching.shape", signatures: []FingerprintSignature{dotProductSignature()}, matches: true, asked: &asked})
	registry.Register(&countingRecogniser{
		name:       "other.shape",
		signatures: []FingerprintSignature{{InitShape: forInitNone, CondShape: forCondOther, PostShape: forPostOther, BodyShape: ForBodyEmpty}},
		matches:    true,
		asked:      &asked,
	})

	loop := parseLoop(t, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")
	matched, matchToken, ok := registry.TryRecogniseForStmt(RecogniseContext{}, loop)

	require.True(t, ok)
	require.Equal(t, "matching.shape", matched.Name())
	require.Equal(t, "matching.shape", matchToken,
		"the token the winning recogniser returned is what the emitter is handed back")
	require.Equal(t, []string{"matching.shape"}, asked,
		"dispatch is by signature, so a recogniser registered for another shape is never even asked")
}

func TestARegistryAsksHigherPriorityRecognisersFirstAndFallsThroughOnRefusal(t *testing.T) {
	t.Parallel()

	var asked []string
	registry := NewRegistry()
	registry.Register(&countingRecogniser{name: "general", priority: 10, signatures: []FingerprintSignature{dotProductSignature()}, matches: true, asked: &asked})
	registry.Register(&countingRecogniser{name: "specialised", priority: 100, signatures: []FingerprintSignature{dotProductSignature()}, matches: false, asked: &asked})

	loop := parseLoop(t, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")
	matched, _, ok := registry.TryRecogniseForStmt(RecogniseContext{}, loop)

	require.True(t, ok)
	require.Equal(t, "general", matched.Name(),
		"a narrow recogniser that refuses must not stop the broader one behind it from matching")
	require.Equal(t, []string{"specialised", "general"}, asked,
		"the narrower recogniser is asked first whatever order the two were registered in")
}

func TestARegistryReportsNoMatchWhenEveryCandidateRefuses(t *testing.T) {
	t.Parallel()

	var asked []string
	registry := NewRegistry()
	registry.Register(&countingRecogniser{name: "declines", signatures: []FingerprintSignature{dotProductSignature()}, matches: false, asked: &asked})

	loop := parseLoop(t, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")
	matched, _, ok := registry.TryRecogniseForStmt(RecogniseContext{}, loop)

	require.False(t, ok)
	require.Nil(t, matched)
	require.Equal(t, []string{"declines"}, asked, "the candidate still has to be offered the loop")
}

func TestANilRegistryRecognisesNothing(t *testing.T) {
	t.Parallel()

	var registry *Registry
	loop := parseLoop(t, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")

	_, _, ok := registry.TryRecogniseForStmt(RecogniseContext{}, loop)

	require.False(t, ok, "a compilation with no pattern registry at all compiles every loop the plain way")
}
