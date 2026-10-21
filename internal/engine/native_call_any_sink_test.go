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
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestNativesAreClassifiedByWhatTheyDoWithAnyOperands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		function any
		name     string
		want     nativeAnySink
	}{
		{name: "json.Marshal", function: json.Marshal, want: anySinkJSONEncode},
		{name: "json.MarshalIndent", function: json.MarshalIndent, want: anySinkJSONEncode},
		{name: "json.Unmarshal", function: json.Unmarshal, want: anySinkJSONDecode},
		{name: "xml.Marshal", function: xml.Marshal, want: anySinkXMLEncode},
		{name: "xml.Unmarshal", function: xml.Unmarshal, want: anySinkXMLDecode},
		{name: "fmt.Sprintf", function: fmt.Sprintf, want: anySinkPrint},
		{name: "fmt.Sscan", function: fmt.Sscan, want: anySinkScan},
		{name: "log.Printf", function: log.Printf, want: anySinkPrint},
		{name: "errors.New", function: errors.New, want: anySinkValue},
		{name: "strings.ToUpper", function: strings.ToUpper, want: anySinkValue},
		{name: "a function literal", function: func(any) {}, want: anySinkValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, nativeFunctionAnySink(reflect.ValueOf(tt.function)))
		})
	}
	require.Equal(t, anySinkValue, nativeFunctionAnySink(reflect.Value{}))
	require.Equal(t, anySinkValue, nativeFunctionAnySink(reflect.ValueOf((func())(nil))))
}

func TestMethodsOnNativeReceiversAreClassifiedByReceiverAndName(t *testing.T) {
	t.Parallel()

	require.Equal(t, anySinkJSONEncode, nativeMethodAnySink(reflect.TypeFor[*json.Encoder](), "Encode"))
	require.Equal(t, anySinkJSONDecode, nativeMethodAnySink(reflect.TypeFor[*json.Decoder](), "Decode"))
	require.Equal(t, anySinkXMLDecode, nativeMethodAnySink(reflect.TypeFor[*xml.Decoder](), "DecodeElement"))
	require.Equal(t, anySinkGobDecode, nativeMethodAnySink(reflect.TypeFor[*gob.Decoder](), "Decode"))
	require.Equal(t, anySinkPrint, nativeMethodAnySink(reflect.TypeFor[*log.Logger](), "Printf"))
	require.Equal(t, anySinkValue, nativeMethodAnySink(reflect.TypeFor[*log.Logger](), "SetPrefix"))
	require.Equal(t, anySinkValue, nativeMethodAnySink(reflect.TypeFor[strings.Builder](), "String"))
	require.Equal(t, anySinkValue, nativeMethodAnySink(nil, "Encode"))
	require.Equal(t, "(*encoding/gob.Decoder).Decode", nativeMethodLabel(reflect.TypeFor[*gob.Decoder](), "Decode"))
	require.Equal(t, "strings.Builder.String", nativeMethodLabel(reflect.TypeFor[strings.Builder](), "String"))
	require.True(t, anySinkJSONEncode.encodes())
	require.False(t, anySinkJSONDecode.encodes())
}

func TestAnOperandForAPlainNativeIsPassedAsItself(t *testing.T) {
	t.Parallel()

	type record struct{ Name string }
	value := reflect.ValueOf(&record{Name: "kept"})

	require.Equal(t, value, prepareAnySinkArgument(nil, anySinkValue, value, argumentTypeContext{}))
	held := reflect.New(reflect.TypeFor[any]()).Elem()
	held.Set(value)
	prepared := prepareAnySinkArgument(nil, anySinkJSONDecode, held, argumentTypeContext{})
	require.Equal(t, value.Pointer(), prepared.Pointer(), "a decoding target reaches the decoder as itself")
	require.False(t, prepareAnySinkArgument(nil, anySinkJSONDecode, reflect.Value{}, argumentTypeContext{}).IsValid())
}

func TestLooseFmtVerbsReadTheFormatOfAPrintfStyleCall(t *testing.T) {
	t.Parallel()

	printf := reflect.TypeFor[func(string, ...any) string]()
	arguments := []reflect.Value{reflect.ValueOf("%d and %T"), reflect.ValueOf(1), reflect.ValueOf(2)}
	require.Equal(t, []byte{'d', 'T'}, looseFmtVerbs(printf, arguments, false))
	require.Nil(t, looseFmtVerbs(printf, arguments, true), "spread operands are not counted")
	require.Nil(t, looseFmtVerbs(reflect.TypeFor[func(...any) string](), arguments[1:], false), "no format string")
	require.Nil(t, looseFmtVerbs(reflect.TypeFor[func(int)](), arguments[1:2], false))
}

func TestADecoderIsRefusedATargetWithANestedScriptMethod(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "at.UnmarshalJSON", stringReturningMethod(t, "UnmarshalJSON", "unused"))
	decode := reflect.ValueOf(json.Unmarshal)
	type wrapper struct{ Inner identityTrailing }

	err := checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&wrapper{})}, false)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceArgument)
	require.ErrorContains(t, err, "UnmarshalJSON")

	err = checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&[]map[string]identityTrailing{})}, false)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceArgument, "through slices and map values")

	type plain struct{ A []int }
	require.NoError(t, checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&plain{})}, false))
	require.NoError(t, checkAnySinkArguments(vm, anySinkJSONEncode, reflect.ValueOf(json.Marshal), nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf(&wrapper{})}, false), "an encoder's methods reach it through the shadow")
	require.NoError(t, checkAnySinkArguments(vm, anySinkValue, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&wrapper{})}, false))
}

func TestAMethodValueBoundOutsideTheVMRefusesValuesWithMethodsANativeMightLookUp(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	unknown, _ := vm.calleeAnySink(reflect.ValueOf(json.NewEncoder(io.Discard)).MethodByName("Encode"))
	require.Contains(t, anySinkRefusedMethods[unknown], "MarshalJSON")
	require.Contains(t, anySinkRefusedMethods[unknown], "String")
	require.Contains(t, anySinkRefusedMethods[unknown], "LogValue")
}

func TestAMethodValueIsClassifiedByTheReceiverAndMethodItWasBoundFor(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	encoder := json.NewEncoder(io.Discard)
	bound := reflect.ValueOf(encoder).MethodByName("Encode")
	sink, callee := vm.calleeAnySink(bound)
	require.Equal(t, anySinkUnknownMethodValue, sink, "a method value the VM did not bind is one bound outside it, whose method cannot be told")
	require.Equal(t, unknownMethodValueDescription, callee.label(bound))

	vm.noteMethodValueClass(bound, reflect.TypeOf(encoder), "Encode")
	sink, callee = vm.calleeAnySink(bound)
	require.Equal(t, anySinkJSONEncode, sink)
	require.Equal(t, "(*encoding/json.Encoder).Encode", callee.label(bound))

	logger := log.New(io.Discard, "", 0)
	prefix := reflect.ValueOf(logger).MethodByName("SetPrefix")
	vm.noteMethodValueClass(prefix, reflect.TypeOf(logger), "SetPrefix")
	sink, _ = vm.calleeAnySink(prefix)
	require.Equal(t, anySinkValue, sink, "a method value the VM bound is recorded even when it receives values as they are")
	for range methodValueSinkRecordMax + 1 {
		vm.noteMethodValueClass(reflect.ValueOf(log.New(io.Discard, "", 0)).MethodByName("Println"), reflect.TypeOf(logger), "Println")
	}
	require.LessOrEqual(t, len(vm.methodValueClasses), methodValueSinkRecordMax, "the record stays bounded")
}

func TestAFreshVMRecordsItsFirstMethodValueWithoutReservingTheWholeRecord(t *testing.T) {
	vm := newTestVM(t)
	logger := log.New(io.Discard, "", 0)
	bound := reflect.ValueOf(logger).MethodByName("SetPrefix")
	receiverType := reflect.TypeOf(logger)
	const runs = 64
	var before runtime.MemStats
	var after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		vm.methodValueClasses = nil
		vm.noteMethodValueClass(bound, receiverType, "SetPrefix")
	}
	runtime.ReadMemStats(&after)
	perRun := (after.TotalAlloc - before.TotalAlloc) / runs
	t.Logf("bytes allocated recording one method value on a fresh VM: %d", perRun)

	require.Less(t, perRun, uint64(4096),
		"a recycled VM starts with no record, and binding one native method value must not allocate room for methodValueSinkRecordMax")
}

func TestRecordingAndClassifyingAMethodValueAllocateNothing(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	vm := newTestVM(t)
	encoder := json.NewEncoder(io.Discard)
	bound := reflect.ValueOf(encoder).MethodByName("Encode")
	receiverType := reflect.TypeOf(encoder)
	vm.noteMethodValueClass(bound, receiverType, "Encode")

	allocations := testing.AllocsPerRun(100, func() {
		vm.noteMethodValueClass(bound, receiverType, "Encode")
	})
	require.Zero(t, allocations, "every handleBindMethodByNameBody on a native receiver records the binding, so the record must not build a label or symbol each time")
	allocations = testing.AllocsPerRun(100, func() {
		_, _ = vm.calleeAnySink(bound)
	})
	require.Zero(t, allocations, "every call through the method value classifies it, so classifying must not build its label")
	sink, callee := vm.calleeAnySink(bound)
	require.Equal(t, anySinkJSONEncode, sink)
	require.Equal(t, "(*encoding/json.Encoder).Encode", callee.label(bound), "the label is still built when a refusal needs it")
}

func TestADecoderIsRefusedAnInterfaceHoldingAPointerToATypeWithAScriptMethod(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "at.UnmarshalJSON", stringReturningMethod(t, "UnmarshalJSON", "unused"))
	decode := reflect.ValueOf(json.Unmarshal)
	type holder struct{ Payload any }

	err := checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&holder{Payload: &identityTrailing{}})}, false)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceArgument, "the decoder decodes into what the interface points at")

	require.NoError(t, checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&holder{Payload: 3})}, false))
	require.NoError(t, checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("{}")), reflect.ValueOf(&holder{})}, false))

	require.True(t, typeCanHoldInterface(reflect.TypeFor[holder]()))
	require.False(t, typeCanHoldInterface(reflect.TypeFor[struct{ A []int }]()), "a part without an interface is not walked")
}

func TestATemplateIsRefusedDataWhosePrintingMethodItCannotCall(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "at.String", stringReturningMethod(t, "String", "unused"))
	execute := reflect.ValueOf(func(any) error { return nil })
	err := checkAnySinkArguments(vm, anySinkTemplate, execute, nativeCalleeName{receiverType: nil, methodName: "", description: "(*text/template.Template).Execute"},
		[]reflect.Value{reflect.ValueOf(struct{ Item identityTrailing }{})}, false)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceArgument)
	require.ErrorContains(t, err, "String")
	require.True(t, NativeAnySinkClassified("text/template.(*Template).Execute"))
	require.True(t, NativeAnySinkClassified("log/slog.(*Logger).Info"))
	require.True(t, NativeAnySinkClassified("fmt.Sprint"))
	require.False(t, NativeAnySinkClassified("strings.ToUpper"))
}

func TestADecoderTargetWalkVisitsEachSharedPartOnce(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "at.UnmarshalJSON", stringReturningMethod(t, "UnmarshalJSON", "unused"))
	decode := reflect.ValueOf(json.Unmarshal)
	target := make([]any, 100)
	for index := range target {
		target[index] = &target
	}

	err := checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("null")), reflect.ValueOf(&target)}, false)
	require.NoError(t, err, "a target whose elements all point back at it is walked once, not once per path")

	target[len(target)-1] = &identityTrailing{}
	err = checkAnySinkArguments(vm, anySinkJSONDecode, decode, nativeCalleeName{receiverType: nil, methodName: "", description: ""},
		[]reflect.Value{reflect.ValueOf([]byte("null")), reflect.ValueOf(&target)}, false)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceArgument, "a refused type behind the shared parts is still found")
}

func TestAFullMethodValueRecordKeepsItsNewestEntries(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	receiverType := reflect.TypeFor[*log.Logger]()
	bound := make([]reflect.Value, methodValueSinkRecordMax+1)
	for index := range bound {
		bound[index] = reflect.ValueOf(log.New(io.Discard, "", 0)).MethodByName("Println")
		vm.noteMethodValueClass(bound[index], receiverType, "Println")
	}
	require.LessOrEqual(t, len(vm.methodValueClasses), methodValueSinkRecordMax)
	require.Greater(t, len(vm.methodValueClasses), methodValueSinkRecordMax/4, "only the older half is dropped, not the whole record")
	sink, _ := vm.calleeAnySink(bound[len(bound)-1])
	require.Equal(t, anySinkPrint, sink, "the newest binding stays classified")
	sink, _ = vm.calleeAnySink(bound[len(bound)-2])
	require.Equal(t, anySinkPrint, sink, "and so does the one before it")
	sink, _ = vm.calleeAnySink(bound[0])
	require.Equal(t, anySinkUnknownMethodValue, sink, "the oldest binding is the one dropped")
}

func TestAFullMethodValueRecordDropsHalfWhateverOtherVMsNumbered(t *testing.T) {
	vm := newTestVM(t)
	receiverType := reflect.TypeFor[*log.Logger]()
	bound := make([]reflect.Value, methodValueSinkRecordMax+1)
	for index := range bound {
		if index == methodValueSinkRecordMax-8 {
			methodValueClassSequence.Add(1 << 20)
		}
		bound[index] = reflect.ValueOf(log.New(io.Discard, "", 0)).MethodByName("Println")
		vm.noteMethodValueClass(bound[index], receiverType, "Println")
	}
	require.Greater(t, len(vm.methodValueClasses), methodValueSinkRecordMax/4,
		"records other VMs numbered meanwhile must not make the drop take more than the older half")
}
