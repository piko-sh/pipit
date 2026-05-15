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
	"os"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const abiOffsetsGoldenPath = "testdata/abi_offsets.golden"

func TestABIOffsetsGolden(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	writeStructLayout(&builder, reflect.TypeFor[dispatchContext]())
	writeStructLayout(&builder, reflect.TypeFor[CallFrame]())
	writeStructLayout(&builder, reflect.TypeFor[AsmCallInfo]())
	writeStructLayout(&builder, reflect.TypeFor[program.VarLocation]())
	writeProvidedOffsets(&builder, "ProvideDispatchContextOffsets", "", reflect.ValueOf(ProvideDispatchContextOffsets()))
	writeProvidedOffsets(&builder, "ProvideCallFrameOffsets", "", reflect.ValueOf(ProvideCallFrameOffsets()))
	writeProvidedOffsets(&builder, "ProvideASMCallInfoOffsets", "", reflect.ValueOf(ProvideASMCallInfoOffsets()))
	writeProvidedOffsets(&builder, "ProvideVarLocationOffsets", "", reflect.ValueOf(ProvideVarLocationOffsets()))
	actual := builder.String()

	if os.Getenv("PIPIT_GOLDEN_UPDATE") != "" {
		require.NoError(t, os.WriteFile(abiOffsetsGoldenPath, []byte(actual), 0o644), "writing %s", abiOffsetsGoldenPath)
		return
	}

	expected, err := os.ReadFile(abiOffsetsGoldenPath)
	require.NoError(t, err, "reading %s (run with PIPIT_GOLDEN_UPDATE=1 to record)", abiOffsetsGoldenPath)
	if string(expected) == actual {
		return
	}
	assert.Failf(t, "ABI layout drift", "ABI layout differs from %s; review the assembly that reads each changed field, then re-record with PIPIT_GOLDEN_UPDATE=1\n%s",
		abiOffsetsGoldenPath, firstLayoutDifference(string(expected), actual))
}

func writeStructLayout(builder *strings.Builder, typ reflect.Type) {
	fmt.Fprintf(builder, "struct %s size=%d align=%d\n", typ.Name(), typ.Size(), typ.Align())
	for field := range typ.Fields() {
		fmt.Fprintf(builder, "  %-40s offset=%-5d size=%d\n", field.Name, field.Offset, field.Type.Size())
	}
	builder.WriteString("\n")
}

func writeProvidedOffsets(builder *strings.Builder, heading, prefix string, value reflect.Value) {
	value = reflect.Indirect(value)
	if prefix == "" {
		fmt.Fprintf(builder, "%s\n", heading)
	}
	for index := range value.NumField() {
		field := value.Type().Field(index)
		if !field.IsExported() {
			continue
		}
		fieldValue := value.Field(index)
		name := prefix + field.Name
		switch fieldValue.Kind() {
		case reflect.Struct:
			writeProvidedOffsets(builder, heading, name+".", fieldValue)
		case reflect.Uintptr, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fmt.Fprintf(builder, "  %-40s = %d\n", name, fieldValue.Uint())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fmt.Fprintf(builder, "  %-40s = %d\n", name, fieldValue.Int())
		}
	}
	if prefix == "" {
		builder.WriteString("\n")
	}
}

func firstLayoutDifference(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	for index := range max(len(expectedLines), len(actualLines)) {
		var want, got string
		if index < len(expectedLines) {
			want = expectedLines[index]
		}
		if index < len(actualLines) {
			got = actualLines[index]
		}
		if want != got {
			return fmt.Sprintf("line %d:\n  recorded: %q\n  actual:   %q", index+1, want, got)
		}
	}
	return "(snapshots differ only in length)"
}

func TestStructFieldLayoutSize(t *testing.T) {
	t.Parallel()
	var layout program.StructFieldLayout
	assert.Equalf(t, uintptr(16), unsafe.Sizeof(layout), "structFieldLayout size (LAYOUT_SIZE_SHIFT=4)")
}

func TestStructFieldLayoutOffsets(t *testing.T) {
	t.Parallel()
	var layout program.StructFieldLayout
	tests := []struct {
		name   string
		got    uintptr
		expect uintptr
	}{
		{name: "Offset", got: unsafe.Offsetof(layout.Offset), expect: 0},
		{name: "TypeIndex", got: unsafe.Offsetof(layout.TypeIndex), expect: 4},
		{name: "Kind", got: unsafe.Offsetof(layout.Kind), expect: 11},
		{name: "RegisterKind", got: unsafe.Offsetof(layout.RegisterKind), expect: 12},
		{name: "Flags", got: unsafe.Offsetof(layout.Flags), expect: 13},
		{name: "FieldTypeIndex", got: unsafe.Offsetof(layout.FieldTypeIndex), expect: 14},
	}
	for _, tt := range tests {
		assert.Equalf(t, tt.expect, tt.got, "structFieldLayout.%s offset", tt.name)
	}
}

func TestUpvalueCellOffsets(t *testing.T) {
	t.Parallel()
	var cell program.UpvalueCell
	tests := []struct {
		name   string
		got    uintptr
		expect uintptr
	}{
		{name: "intValue", got: unsafe.Offsetof(cell.IntValue), expect: 248},
		{name: "floatValue", got: unsafe.Offsetof(cell.FloatValue), expect: 240},
		{name: "uintValue", got: unsafe.Offsetof(cell.UintValue), expect: 216},
		{name: "boolValue", got: unsafe.Offsetof(cell.BoolValue), expect: 256},
		{name: "isIndirect", got: unsafe.Offsetof(cell.IsIndirect), expect: 258},
	}
	for _, tt := range tests {
		assert.Equalf(t, tt.expect, tt.got, "upvalueCell.%s offset", tt.name)
	}
}

func TestRegisterKindConstants(t *testing.T) {
	t.Parallel()
	assert.EqualValuesf(t, 0, isa.RegisterInt, "RegisterInt")
	assert.EqualValuesf(t, 1, isa.RegisterFloat, "RegisterFloat")
	assert.EqualValuesf(t, 4, isa.RegisterBool, "RegisterBool")
	assert.EqualValuesf(t, 5, isa.RegisterUint, "RegisterUint")
}

func TestASMDispatchSaveOffsets(t *testing.T) {
	t.Parallel()
	var save asmDispatchSave
	tests := []struct {
		name   string
		got    uintptr
		expect uintptr
	}{
		{name: "codeBase", got: unsafe.Offsetof(save.codeBase), expect: 0},
		{name: "codeLength", got: unsafe.Offsetof(save.codeLength), expect: 8},
		{name: "intConstantsBase", got: unsafe.Offsetof(save.intConstantsBase), expect: 16},
		{name: "floatConstantsBase", got: unsafe.Offsetof(save.floatConstantsBase), expect: 24},
		{name: "stringConstantsBase", got: unsafe.Offsetof(save.stringConstantsBase), expect: 32},
		{name: "boolConstantsBase", got: unsafe.Offsetof(save.boolConstantsBase), expect: 40},
		{name: "uintConstantsBase", got: unsafe.Offsetof(save.uintConstantsBase), expect: 48},
	}
	for _, tt := range tests {
		assert.Equalf(t, tt.expect, tt.got, "asmDispatchSave.%s offset", tt.name)
	}
	assert.Equalf(t, uintptr(64), unsafe.Sizeof(save), "asmDispatchSave size (DS_SIZE_SHIFT 6)")
}

func TestReflectKindConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   reflect.Kind
		expect uintptr
	}{
		{name: "Invalid", kind: reflect.Invalid, expect: 0},
		{name: "Bool", kind: reflect.Bool, expect: 1},
		{name: "Int", kind: reflect.Int, expect: 2},
		{name: "Int8", kind: reflect.Int8, expect: 3},
		{name: "Int16", kind: reflect.Int16, expect: 4},
		{name: "Int32", kind: reflect.Int32, expect: 5},
		{name: "Int64", kind: reflect.Int64, expect: 6},
		{name: "Uint", kind: reflect.Uint, expect: 7},
		{name: "Uint8", kind: reflect.Uint8, expect: 8},
		{name: "Uint16", kind: reflect.Uint16, expect: 9},
		{name: "Uint32", kind: reflect.Uint32, expect: 10},
		{name: "Uint64", kind: reflect.Uint64, expect: 11},
		{name: "Uintptr", kind: reflect.Uintptr, expect: 12},
		{name: "Float32", kind: reflect.Float32, expect: 13},
		{name: "Float64", kind: reflect.Float64, expect: 14},
		{name: "Interface", kind: reflect.Interface, expect: 20},
		{name: "Pointer", kind: reflect.Pointer, expect: 22},
	}
	for _, tt := range tests {
		assert.Equalf(t, tt.expect, uintptr(tt.kind), "reflect.%s (the ASM REFLECT_%s define needs update)", tt.name, tt.name)
	}
}

func TestStructFieldLayoutFlagConstants(t *testing.T) {
	t.Parallel()
	assert.Equalf(t, uint8(1), program.StructFieldLayoutFlagEmbedded, "LAYOUT_FLAG_EMBEDDED define needs update")
	assert.Equalf(t, uint8(2), program.StructFieldLayoutFlagCycleBroken, "LAYOUT_FLAG_CYCLE_BROKEN define needs update")
}

func TestReflectValueFlagConstants(t *testing.T) {
	t.Parallel()
	assert.Equalf(t, uintptr(0x1F), flagKindMask, "flagKindMask")
	assert.Equalf(t, uintptr(0x80), flagIndir, "flagIndir")
	assert.Equalf(t, uintptr(0x100), flagAddr, "flagAddr")
}
