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

package fault_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

type sentinel struct {
	name string
	err  error
}

var sentinels = []sentinel{
	{name: "ErrCompilation", err: fault.ErrCompilation},
	{name: "ErrEntrypointNotFound", err: fault.ErrEntrypointNotFound},
	{name: "ErrCallArgumentCount", err: fault.ErrCallArgumentCount},
	{name: "ErrExecutionCancelled", err: fault.ErrExecutionCancelled},
	{name: "ErrFeatureNotAllowed", err: fault.ErrFeatureNotAllowed},
	{name: "ErrMainReturned", err: fault.ErrMainReturned},
	{name: "ErrPackageNotInRegistry", err: fault.ErrPackageNotInRegistry},
	{name: "ErrNoSymbolProvider", err: fault.ErrNoSymbolProvider},
	{name: "ErrDivisionByZero", err: fault.ErrDivisionByZero},
	{name: "ErrStackOverflow", err: fault.ErrStackOverflow},
	{name: "ErrIndexOutOfRange", err: fault.ErrIndexOutOfRange},
	{name: "ErrSliceOutOfRange", err: fault.ErrSliceOutOfRange},
	{name: "ErrAllocationLimit", err: fault.ErrAllocationLimit},
	{name: "ErrGoroutineLimit", err: fault.ErrGoroutineLimit},
	{name: "ErrOutputLimit", err: fault.ErrOutputLimit},
	{name: "ErrCostBudgetExceeded", err: fault.ErrCostBudgetExceeded},
	{name: "ErrGoroutineJoinTimeout", err: fault.ErrGoroutineJoinTimeout},
	{name: "ErrInterpreterInvariant", err: fault.ErrInterpreterInvariant},
	{name: "ErrUncaughtPanic", err: fault.ErrUncaughtPanic},
	{name: "ErrDeadlock", err: fault.ErrDeadlock},
	{name: "ErrUnsupportedInterfaceArgument", err: fault.ErrUnsupportedInterfaceArgument},
	{name: "ErrUnsupportedInterfaceField", err: fault.ErrUnsupportedInterfaceField},
	{name: "ErrTypeMismatch", err: fault.ErrTypeMismatch},
	{name: "ErrGoexit", err: fault.ErrGoexit},
	{name: "ErrArenaBudgetExceeded", err: fault.ErrArenaBudgetExceeded},
	{name: "ErrDebuggerStop", err: fault.ErrDebuggerStop},
	{name: "ErrParse", err: fault.ErrParse},
	{name: "ErrTypeCheck", err: fault.ErrTypeCheck},
	{name: "ErrCyclicImport", err: fault.ErrCyclicImport},
	{name: "ErrNoBytecodeStore", err: fault.ErrNoBytecodeStore},
	{name: "ErrSourceSizeLimit", err: fault.ErrSourceSizeLimit},
	{name: "ErrCompilePanic", err: fault.ErrCompilePanic},
	{name: "ErrBytecodeVerification", err: fault.ErrBytecodeVerification},
	{name: "ErrCompileDepthLimit", err: fault.ErrCompileDepthLimit},
	{name: "ErrLiteralElementLimit", err: fault.ErrLiteralElementLimit},
	{name: "ErrSpillAreaExhausted", err: fault.ErrSpillAreaExhausted},
	{name: "ErrSpillUnsupportedBank", err: fault.ErrSpillUnsupportedBank},
	{name: "ErrCompileNamedScalarPoolExhausted", err: fault.ErrCompileNamedScalarPoolExhausted},
	{name: "ErrCompilePackageVariableNotSettable", err: fault.ErrCompilePackageVariableNotSettable},
	{name: "ErrCompileEmbedUnsupported", err: fault.ErrCompileEmbedUnsupported},
	{name: "ErrCompileTypeConversionArgCount", err: fault.ErrCompileTypeConversionArgCount},
	{name: "ErrCompileDereferenceRequiresPointer", err: fault.ErrCompileDereferenceRequiresPointer},
	{name: "ErrCompileDereferenceAssignRequiresPointer", err: fault.ErrCompileDereferenceAssignRequiresPointer},
	{name: "ErrCompileMapLiteralExpectKeyValue", err: fault.ErrCompileMapLiteralExpectKeyValue},
	{name: "ErrCompileSliceIndexMustBeInteger", err: fault.ErrCompileSliceIndexMustBeInteger},
	{name: "ErrCompileUnaryMinusUnsupported", err: fault.ErrCompileUnaryMinusUnsupported},
	{name: "ErrCompileUnaryXorRequiresInteger", err: fault.ErrCompileUnaryXorRequiresInteger},
	{name: "ErrCompileChannelReceiveRequiresGeneral", err: fault.ErrCompileChannelReceiveRequiresGeneral},
	{name: "ErrCompileBreakOutsideLoopOrSwitch", err: fault.ErrCompileBreakOutsideLoopOrSwitch},
	{name: "ErrCompileContinueOutsideLoop", err: fault.ErrCompileContinueOutsideLoop},
	{name: "ErrCompileFallthroughOutsideSwitch", err: fault.ErrCompileFallthroughOutsideSwitch},
	{name: "ErrCompileTailCallTargetNotIdent", err: fault.ErrCompileTailCallTargetNotIdent},
	{name: "ErrCompileTypeSwitchAssignNotTypeAssert", err: fault.ErrCompileTypeSwitchAssignNotTypeAssert},
	{name: "ErrCompileTypeSwitchExprNotTypeAssert", err: fault.ErrCompileTypeSwitchExprNotTypeAssert},
	{name: "ErrCompileMethodExprMissingReceiver", err: fault.ErrCompileMethodExprMissingReceiver},
	{name: "ErrCompileIncDecSelectorNumeric", err: fault.ErrCompileIncDecSelectorNumeric},
	{name: "ErrCompileIncDecRequiresNumeric", err: fault.ErrCompileIncDecRequiresNumeric},
	{name: "ErrCompileMapCommaOkValueNotIdent", err: fault.ErrCompileMapCommaOkValueNotIdent},
	{name: "ErrCompileMapCommaOkOkNotIdent", err: fault.ErrCompileMapCommaOkOkNotIdent},
	{name: "ErrCompileMapCommaOkSourceNotMap", err: fault.ErrCompileMapCommaOkSourceNotMap},
	{name: "ErrCompileChanRecvCommaOkSourceNotChan", err: fault.ErrCompileChanRecvCommaOkSourceNotChan},
	{name: "ErrCompileChanRecvCommaOkValueNotIdent", err: fault.ErrCompileChanRecvCommaOkValueNotIdent},
	{name: "ErrCompileChanRecvCommaOkOkNotIdent", err: fault.ErrCompileChanRecvCommaOkOkNotIdent},
	{name: "ErrCompileTypeAssertCommaOkValueNotIdent", err: fault.ErrCompileTypeAssertCommaOkValueNotIdent},
	{name: "ErrCompileTypeAssertCommaOkOkNotIdent", err: fault.ErrCompileTypeAssertCommaOkOkNotIdent},
	{name: "ErrCompileArithFloatUnsupported", err: fault.ErrCompileArithFloatUnsupported},
	{name: "ErrCompileArithStringUnsupported", err: fault.ErrCompileArithStringUnsupported},
	{name: "ErrCompileArithGeneralUnsupported", err: fault.ErrCompileArithGeneralUnsupported},
	{name: "ErrCompileArithUintUnsupported", err: fault.ErrCompileArithUintUnsupported},
	{name: "ErrCompileArithComplexUnsupported", err: fault.ErrCompileArithComplexUnsupported},
	{name: "ErrCompileCompareFloatUnsupported", err: fault.ErrCompileCompareFloatUnsupported},
	{name: "ErrCompileCompareStringUnsupported", err: fault.ErrCompileCompareStringUnsupported},
	{name: "ErrCompileCompareGeneralUnsupported", err: fault.ErrCompileCompareGeneralUnsupported},
	{name: "ErrCompileCompareComplexOrdering", err: fault.ErrCompileCompareComplexOrdering},
	{name: "ErrCompileBitwiseRequiresInteger", err: fault.ErrCompileBitwiseRequiresInteger},
	{name: "ErrCompileBuiltinLenArgCount", err: fault.ErrCompileBuiltinLenArgCount},
	{name: "ErrCompileBuiltinAppendArgCount", err: fault.ErrCompileBuiltinAppendArgCount},
	{name: "ErrCompileBuiltinDeleteArgCount", err: fault.ErrCompileBuiltinDeleteArgCount},
	{name: "ErrCompileBuiltinComplexArgCount", err: fault.ErrCompileBuiltinComplexArgCount},
	{name: "ErrCompileBuiltinComplexRequiresFloat", err: fault.ErrCompileBuiltinComplexRequiresFloat},
	{name: "ErrCompileBuiltinCapArgCount", err: fault.ErrCompileBuiltinCapArgCount},
	{name: "ErrCompileBuiltinCopyArgCount", err: fault.ErrCompileBuiltinCopyArgCount},
	{name: "ErrCompileBuiltinClearArgCount", err: fault.ErrCompileBuiltinClearArgCount},
	{name: "ErrCompileBuiltinMinMaxArgCount", err: fault.ErrCompileBuiltinMinMaxArgCount},
	{name: "ErrCompileBuiltinPanicArgCount", err: fault.ErrCompileBuiltinPanicArgCount},
	{name: "ErrCompileBuiltinCloseArgCount", err: fault.ErrCompileBuiltinCloseArgCount},
	{name: "ErrCompileUnsafeStringDataArgCount", err: fault.ErrCompileUnsafeStringDataArgCount},
	{name: "ErrCompileUnsafeSliceDataArgCount", err: fault.ErrCompileUnsafeSliceDataArgCount},
	{name: "ErrCompileGenericMethodNotSpecialisable", err: fault.ErrCompileGenericMethodNotSpecialisable},
	{name: "ErrCompileGenericMethodSpecialisationLimit", err: fault.ErrCompileGenericMethodSpecialisationLimit},
	{name: "ErrCompileGenericMethodTypeArgLimit", err: fault.ErrCompileGenericMethodTypeArgLimit},
	{name: "ErrCompileGenericCallerRequiresSpecialisation", err: fault.ErrCompileGenericCallerRequiresSpecialisation},
	{name: "ErrCompileGenericValueNotSpecialisable", err: fault.ErrCompileGenericValueNotSpecialisable},
	{name: "ErrCompileUninstantiatedGeneric", err: fault.ErrCompileUninstantiatedGeneric},
	{name: "ErrCompileInvalidStructLiteralKey", err: fault.ErrCompileInvalidStructLiteralKey},
	{name: "ErrLinkedCallNoInstance", err: fault.ErrLinkedCallNoInstance},
	{name: "ErrLinkedCallArityMismatch", err: fault.ErrLinkedCallArityMismatch},
	{name: "ErrLinkedCallTypeArgUnresolvable", err: fault.ErrLinkedCallTypeArgUnresolvable},
	{name: "ErrLinkedCallTooManyTypeArgs", err: fault.ErrLinkedCallTooManyTypeArgs},
	{name: "ErrDebugNotPaused", err: fault.ErrDebugNotPaused},
	{name: "ErrDebugNotRunning", err: fault.ErrDebugNotRunning},
	{name: "ErrDebugAlreadyPaused", err: fault.ErrDebugAlreadyPaused},
	{name: "ErrDebugUnknownThread", err: fault.ErrDebugUnknownThread},
	{name: "ErrDebugFrameOutOfRange", err: fault.ErrDebugFrameOutOfRange},
	{name: "ErrDebugExited", err: fault.ErrDebugExited},
	{name: "ErrNoProgramBinding", err: fault.ErrNoProgramBinding},
	{name: "ErrVariableUnavailable", err: fault.ErrVariableUnavailable},
	{name: "ErrVariableNotWritable", err: fault.ErrVariableNotWritable},
	{name: "ErrConditionNotBool", err: fault.ErrConditionNotBool},
	{name: "ErrCallsNotAllowed", err: fault.ErrCallsNotAllowed},
}

func TestSentinelsHaveNonEmptyDistinctMessages(t *testing.T) {
	t.Parallel()
	seenText := make(map[string]string, len(sentinels))
	for _, s := range sentinels {
		require.NotNil(t, s.err, "%s must be initialised", s.name)
		text := s.err.Error()
		require.NotEmpty(t, text, "%s must carry a message", s.name)
		require.Equal(t, strings.TrimSpace(text), text, "%s must not carry surrounding whitespace", s.name)
		if prior, dup := seenText[text]; dup {
			require.Failf(t, "duplicate sentinel message", "%s and %s share the message %q; doc.go forbids two sentinels with the same text", prior, s.name, text)
		}
		seenText[text] = s.name
	}
}

func TestSentinelsAreDistinctIdentities(t *testing.T) {
	t.Parallel()
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				require.ErrorIs(t, a.err, b.err)
				continue
			}
			require.NotErrorIs(t, a.err, b.err, "%s must not match %s", a.name, b.name)
		}
	}
}

func exportedErrNames(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, ident := range value.Names {
				if ident.IsExported() && strings.HasPrefix(ident.Name, "Err") {
					names = append(names, ident.Name)
				}
			}
		}
	}
	return names
}

func TestSentinelListMatchesSource(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var declared []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		require.NoError(t, parseErr)
		declared = append(declared, exportedErrNames(file)...)
	}

	listed := make([]string, 0, len(sentinels))
	for _, s := range sentinels {
		listed = append(listed, s.name)
	}
	require.ElementsMatch(t, declared, listed, "the hand list must name every exported Err* variable exactly once")
}

func TestErrChainFmtWrapsBothOperands(t *testing.T) {
	t.Parallel()
	inner := errors.New("inner detail")
	cases := []struct {
		name     string
		sentinel error
		wantText string
	}{
		{name: "compilation", sentinel: fault.ErrCompilation, wantText: "compilation failed: inner detail"},
		{name: "depth limit", sentinel: fault.ErrCompileDepthLimit, wantText: "compile recursion depth exceeded: inner detail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf(fault.ErrChainFmt, tc.sentinel, inner)
			require.ErrorIs(t, err, tc.sentinel)
			require.ErrorIs(t, err, inner)
			require.Equal(t, tc.wantText, err.Error())
		})
	}
}

func TestErrChainMessageFmtWrapsBothOperandsAroundMessage(t *testing.T) {
	t.Parallel()
	inner := errors.New("inner detail")
	err := fmt.Errorf(fault.ErrChainMessageFmt, fault.ErrEntrypointNotFound, "main.run", inner)
	require.ErrorIs(t, err, fault.ErrEntrypointNotFound)
	require.ErrorIs(t, err, inner)
	require.Equal(t, "entrypoint not found: main.run: inner detail", err.Error())
}

func TestErrChainFmtDoesNotMatchUnrelatedSentinel(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, fault.ErrFeatureNotAllowed)
	require.ErrorIs(t, err, fault.ErrCompilation)
	require.ErrorIs(t, err, fault.ErrFeatureNotAllowed)
	require.NotErrorIs(t, err, fault.ErrMainReturned)
}
