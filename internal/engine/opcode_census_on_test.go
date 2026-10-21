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
//go:build pipit_opcode_census

package engine

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const opcodeCensusRuns = 1000

func runOpcodeCensusWorkload(t *testing.T) {
	t.Helper()
	function := buildIntBinaryOp(isa.OpAddInt64, 2, 3)
	for range opcodeCensusRuns {
		requireSyntheticResult(t, function, 5)
	}
}

func readOpcodeCensus(t *testing.T) OpcodeCensusReport {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, WriteOpcodeCensus(&buffer))
	var report OpcodeCensusReport
	require.NoError(t, json.Unmarshal(buffer.Bytes(), &report))
	return report
}

func TestOpcodeCensusSnapshotCarriesEachCountedFunction(t *testing.T) {
	OpcodeCensusReset()
	runOpcodeCensusWorkload(t)

	functions := OpcodeCensusSnapshot().Functions
	require.NotEmpty(t, functions)
	for _, function := range functions {
		require.NotNil(t, function.Function, "the counts are joined with the function's own words")
		require.Equal(t, function.Name, function.Function.Name)
		require.Len(t, function.Function.Body, function.Length)
	}
}

func TestOpcodeCensusCountsEveryDispatchedInstruction(t *testing.T) {
	OpcodeCensusReset()
	runOpcodeCensusWorkload(t)

	report := readOpcodeCensus(t)
	require.GreaterOrEqual(t, report.Total, uint64(4*opcodeCensusRuns), "each run dispatches four instructions")

	operationTotal := uint64(0)
	for _, operation := range report.Operations {
		require.NotEmpty(t, operation.Name)
		require.Len(t, operation.Slots, 1)
		require.Len(t, operation.Constants, 1)
		require.Regexp(t, `^(Op|SubOp)[A-Z]`, operation.Constants[0])
		operationTotal += operation.Count
	}
	require.Equal(t, report.Total, operationTotal, "every dispatch is counted under one operation")

	functionTotal := uint64(0)
	for _, function := range report.Functions {
		require.Len(t, function.Counts, function.Length)
		functionTotal += function.Total
	}
	require.Equal(t, report.Total, functionTotal, "every dispatch is counted under one function")

	require.NotEmpty(t, report.Pairs)
	require.GreaterOrEqual(t, report.Pairs[0].Count, uint64(opcodeCensusRuns*9/10), "the function's hottest pair repeats each run")
	require.Contains(t, report.Pairs[0].Name, " ; ")
	require.Len(t, report.Pairs[0].Slots, 2)
	for index := 1; index < len(report.Pairs); index++ {
		require.GreaterOrEqual(t, report.Pairs[index-1].Count, report.Pairs[index].Count)
	}
}

func TestOpcodeCensusResetDiscardsEarlierCounts(t *testing.T) {
	runOpcodeCensusWorkload(t)

	OpcodeCensusReset()
	report := readOpcodeCensus(t)
	require.Zero(t, report.Total)
	require.Empty(t, report.Operations)
	require.Empty(t, report.Pairs)
	require.Empty(t, report.Triples)
	require.Empty(t, report.Functions)
}

func TestSlowCallCensusCountsByKindAndSignature(t *testing.T) {
	OpcodeCensusReset()
	for range 3 {
		slowCallCensusRecord(slowCallNative, reflect.TypeFor[func() error]())
	}
	slowCallCensusRecord(slowCallClosure, reflect.TypeFor[func(int)]())
	slowCallCensusRecord(slowCallMethod, reflect.TypeFor[func(int)]())
	slowCallCensusRecord(slowCallMethod, reflect.TypeFor[func(int)]())

	require.Equal(t, []slowCallCensusCount{
		{Kind: slowCallNative, Signature: "func() error", Count: 3},
		{Kind: slowCallMethod, Signature: "func(int)", Count: 2},
		{Kind: slowCallClosure, Signature: "func(int)", Count: 1},
	}, slowCallCensusSnapshot())

	OpcodeCensusReset()
	require.Empty(t, slowCallCensusSnapshot(), "a reset discards the slow-call counts with the rest")
}
