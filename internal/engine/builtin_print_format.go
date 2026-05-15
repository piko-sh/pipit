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
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

const (
	// float64Bits is the precision strconv formats a float64 component with.
	float64Bits = 64

	// float32Bits is the precision strconv formats a float32 component with.
	float32Bits = 32
)

// formatBuiltinPrint renders the operands of print or println the way the Go runtime
// does: print concatenates them, println separates them with spaces and ends the line.
//
// Takes arguments ([]any) which are the operand values.
// Takes newline (bool) which selects println's spacing and trailing newline.
//
// Returns the text to write.
func formatBuiltinPrint(arguments []any, newline bool) string {
	var builder strings.Builder
	for i, argument := range arguments {
		if newline && i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(formatBuiltinPrintArg(argument))
	}
	if newline {
		builder.WriteByte('\n')
	}
	return builder.String()
}

// formatBuiltinPrintArg renders one print operand: numbers in their shortest form,
// pointers, channels, maps and funcs as addresses (0x0 when nil), slices as
// [len/cap]address and a nil interface as (0x0,0x0), matching the runtime's printer.
//
// Takes argument (any) which is the operand.
//
// Returns the operand's text.
func formatBuiltinPrintArg(argument any) string {
	switch value := argument.(type) {
	case nil:
		return "(0x0,0x0)"
	case bool:
		return strconv.FormatBool(value)
	case string:
		return value
	case int64:
		return strconv.FormatInt(value, 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, float64Bits)
	case float32:
		return strconv.FormatFloat(float64(value), 'g', -1, float32Bits)
	case complex128:
		return formatBuiltinPrintComplex(real(value), imag(value), float64Bits)
	case complex64:
		return formatBuiltinPrintComplex(float64(real(value)), float64(imag(value)), float32Bits)
	default:
		return formatBuiltinPrintReflect(reflect.ValueOf(argument))
	}
}

// formatBuiltinPrintReflect renders operands whose static type is not one of the register
// banks' scalars: sized integers, floats and the reference kinds.
//
// Takes value (reflect.Value) which wraps the operand.
//
// Returns the operand's text.
func formatBuiltinPrintReflect(value reflect.Value) string {
	switch value.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(value.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(value.Float(), 'g', -1, float32Bits)
	case reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'g', -1, float64Bits)
	case reflect.String:
		return value.String()
	case reflect.Slice:
		if value.IsNil() {
			return "[0/0]0x0"
		}
		return fmt.Sprintf("[%d/%d]%#x", value.Len(), value.Cap(), value.Pointer())
	case reflect.Pointer, reflect.Chan, reflect.Map, reflect.Func, reflect.UnsafePointer:
		if value.IsNil() {
			return "0x0"
		}
		return fmt.Sprintf("%#x", value.Pointer())
	default:
		return fmt.Sprint(value.Interface())
	}
}

// formatBuiltinPrintComplex renders a complex operand as (re+imi) with the imaginary
// part's sign always present.
//
// Takes re (float64) which is the real part.
// Takes im (float64) which is the imaginary part.
// Takes bits (int) which is the component precision, 32 or 64.
//
// Returns the operand's text.
func formatBuiltinPrintComplex(re, im float64, bits int) string {
	imaginary := strconv.FormatFloat(im, 'g', -1, bits)
	if !strings.HasPrefix(imaginary, "-") && !strings.HasPrefix(imaginary, "+") {
		imaginary = "+" + imaginary
	}
	return "(" + strconv.FormatFloat(re, 'g', -1, bits) + imaginary + "i)"
}
