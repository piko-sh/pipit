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

package isa

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type goExitReason string

const (
	goExitChangesFrame    goExitReason = "calls, unwinds or starts a goroutine, so the frame or the stack changes under the loop"
	goExitBlocks          goExitReason = "can park the goroutine"
	goExitNeverDispatched goExitReason = "is padding or the data of another operation, which the loop steps over"
	goExitRare            goExitReason = "ran in under 0.1% of the dispatches of every program measured, the benchmark suite and both Doom phases"
)

func goExitReasons() map[string]goExitReason {
	reasons := map[string]goExitReason{}
	for reason, keys := range map[goExitReason][]string{
		goExitChangesFrame: {
			"0:DEFER", "0:GO", "1:CALL_BUILTIN", "1:CALL_IIFE", "1:CALL_METHOD_INLINEABLE", "1:CALL_NATIVE", "2:PANIC",
			"2:PANIC_RANGE_FUNCTION_CONTINUED_INT64", "2:RECOVER",
		},
		goExitBlocks: {
			"0:SEND_TO_CHANNEL", "1:RECEIVE_FROM_CHANNEL", "1:SELECT",
		},
		goExitNeverDispatched: {
			"0:EXTENSION", "3:NO_OPERATION",
		},
		goExitRare: {
			"0:ADD_GENERAL", "0:APPEND_BYTE_IN_PLACE", "0:APPEND_IN_PLACE", "0:APPEND_SPREAD_IN_PLACE", "0:BOX_AS_TYPE",
			"0:DIVIDE_GENERAL", "0:GET_ELEMENT_FIELD_BOOL", "0:GET_ELEMENT_FIELD_STRING", "0:GET_ELEMENT_FIELD_UINT64",
			"0:GET_ELEMENT_INT64_UNCHECKED", "0:GET_FIELD_INT64", "0:GREATER_OR_EQUAL_STRING", "0:GREATER_THAN_STRING",
			"0:LESS_OR_EQUAL_STRING", "0:LESS_THAN_STRING", "0:LOAD_COMPOSITE_ZERO_IN_PLACE", "0:MULTIPLY_GENERAL",
			"0:RANGE_NEXT_SLICE_INT64", "0:REMAINDER_GENERAL", "0:SET_ELEMENT_INT64_UNCHECKED", "0:SET_FIELD_INT64",
			"0:SUBTRACT_GENERAL", "1:APPEND_BOOL_WIDE", "1:APPEND_FLOAT64_WIDE", "1:APPEND_INT64_WIDE",
			"1:APPEND_SPREAD_BYTE_IN_PLACE", "1:APPEND_STRING_WIDE", "1:APPEND_UINT64_WIDE_IN_PLACE", "1:BIND_METHOD_BY_NAME",
			"1:BIT_NOT_UINT64", "1:CAPACITY_SLICE_BOOL", "1:CAPACITY_SLICE_BYTE", "1:CAPACITY_SLICE_FLOAT64",
			"1:CAPACITY_SLICE_INT64", "1:CAPACITY_SLICE_STRING", "1:CAPACITY_SLICE_UINT64", "1:CONVERT_RUNE_TO_STRING",
			"1:CONVERT_STRING_TO_BYTES", "1:COPY_SLICE_BOOL", "1:COPY_SLICE_BYTE", "1:COPY_SLICE_FLOAT64",
			"1:COPY_SLICE_INT64", "1:COPY_SLICE_STRING", "1:COPY_SLICE_UINT64", "1:EQUAL_INTERFACE_STRICT",
			"1:EQUAL_STRING_CONSTANT_THEN_JUMP_IF_FALSE", "1:GET_STRUCT_FIELD_BOOL_WIDE", "1:GET_STRUCT_FIELD_FLOAT64_WIDE",
			"1:GET_STRUCT_FIELD_INT64_WIDE", "1:GET_STRUCT_FIELD_UINT64_WIDE", "1:MAKE_CHANNEL", "1:MAKE_METHOD_EXPRESSION",
			"1:NOT_EQUAL_INTERFACE_STRICT", "1:RELOAD", "1:ROUND_TO_COMPLEX64", "1:ROUND_TO_FLOAT32",
			"1:SET_STRUCT_FIELD_BOOL_WIDE", "1:SET_STRUCT_FIELD_FLOAT64_WIDE", "1:SET_STRUCT_FIELD_INT64_WIDE",
			"1:SET_STRUCT_FIELD_SLICE_BOOL", "1:SET_STRUCT_FIELD_SLICE_BYTE", "1:SET_STRUCT_FIELD_SLICE_FLOAT64",
			"1:SET_STRUCT_FIELD_SLICE_INT64", "1:SET_STRUCT_FIELD_SLICE_STRING", "1:SET_STRUCT_FIELD_SLICE_UINT64",
			"1:SET_STRUCT_FIELD_UINT64_WIDE", "1:SIMD_ADD_FLOAT64", "1:SIMD_DOT_PRODUCT_FLOAT64", "1:SIMD_SCALE_FLOAT64",
			"1:SIMD_SUM_FLOAT64", "1:SPILL", "1:STRINGS_TO_LOWER", "1:STRINGS_TO_UPPER", "1:STRINGS_TRIM_SPACE",
			"1:UNBOX_FLOAT64", "1:UNBOX_STRING", "1:UNSAFE_SLICE_DATA", "1:UNSAFE_STRING_DATA", "2:CHECK_SHIFT_COUNT",
			"2:CLOSE_CHANNEL", "2:LOAD_NIL", "2:MAKE_INTERFACE_METHOD_EXPRESSION", "2:SET_ZERO_GENERAL",
			"3:SYNCHRONISE_IIFE_CAPTURED_VARIABLES",
		},
	} {
		for _, key := range keys {
			reasons[key] = reason
		}
	}
	return reasons
}

func TestEveryGoExitRowIsJustified(t *testing.T) {
	t.Parallel()

	reasons := goExitReasons()
	taken := map[string]bool{}
	for _, spec := range AllSpecs() {
		if spec.Dispatch() != DispatchGo {
			continue
		}
		key := fmt.Sprintf("%d:%s", spec.Tier, spec.Name)
		taken[key] = true
		require.Containsf(t, reasons, key,
			"%s leaves the assembly loop by the generic exit: give it an assembly body, a shim or an exit stub, or list it with the reason it has none", key)
	}
	for key, reason := range reasons {
		require.Truef(t, taken[key], "%s no longer takes the generic exit, so it needs no reason (%s)", key, reason)
	}
}
