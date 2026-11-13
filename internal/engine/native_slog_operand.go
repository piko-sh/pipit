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
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"

	"piko.sh/safeconv"

	"pipit.sh/pipit/internal/fault"
)

var (
	// slogOperandMethods are the script methods log/slog's handlers look up on an `any`
	// operand, through fmt or directly.
	slogOperandMethods = []string{"String", "Error", "Format", "GoString", "MarshalText", "MarshalJSON", "LogValue"}
)

// slogOperand carries a script value to log/slog's handlers, which format it later and
// differently.
type slogOperand struct {
	// AdapterCore holds the script value and the methods its type declares.
	*AdapterCore
}

// slogOperandError is a slogOperand whose script type declares Error but not MarshalJSON.
type slogOperandError struct {
	// slogOperand carries the script value.
	slogOperand
}

// slogOperandErrorText adds MarshalText to slogOperandError.
type slogOperandErrorText struct {
	// slogOperand carries the script value.
	slogOperand
}

// slogOperandJSON is a slogOperand whose script type declares none of Error, MarshalText
// and LogValue.
type slogOperandJSON struct {
	// slogOperand carries the script value.
	slogOperand
}

// slogOperandJSONText adds MarshalText to slogOperandJSON.
type slogOperandJSONText struct {
	// slogOperand carries the script value.
	slogOperand
}

// slogOperandJSONError is a slogOperand whose script type declares Error and MarshalJSON.
type slogOperandJSONError struct {
	// slogOperand carries the script value.
	slogOperand
}

// slogOperandJSONErrorText adds MarshalText to slogOperandJSONError.
type slogOperandJSONErrorText struct {
	// slogOperand carries the script value.
	slogOperand
}

// slogOperandLogValuer is a slogOperand whose script type declares LogValue, which every
// handler resolves before formatting, so no other method is reached.
type slogOperandLogValuer struct {
	// slogOperand carries the script value.
	slogOperand
}

// Format formats the script value as fmt would, reaching its String, Error, Format and
// GoString methods.
//
// Takes state (fmt.State) which receives the output.
// Takes verb (rune) which is the verb.
func (o slogOperand) Format(state fmt.State, verb rune) {
	operand := wrapPipitSynthesisedFmtArgForVerb(o.vm, adapterHeldValueInterface(o.underlying), safeconv.RuneToByte(verb), o.typeName)
	_, _ = fmt.Fprintf(state, fmt.FormatString(state, verb), operand)
}

// marshalJSON encodes the script value as encoding/json would, through the marshal
// shadow, without escaping HTML, as log/slog's JSON handler encodes.
//
// Returns the encoding and any error.
func (o slogOperand) marshalJSON() ([]byte, error) {
	shadow := shadowForMarshal(o.vm, o.underlying, anySinkJSONEncode)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(adapterHeldValueInterface(shadow)); err != nil {
		return nil, fmt.Errorf("log/slog operand %s: %w", o.typeName, err)
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n")), nil
}

// errorText calls the script Error method.
//
// Returns string which is its result.
func (o slogOperand) errorText() string {
	results := o.InvokeRecordingError("Error")
	if len(results) != 1 || results[0].Kind() != reflect.String {
		return fmt.Sprintf("%%!v(%v: Error of %s)", fault.ErrSlogOperandResult, o.typeName)
	}
	return results[0].String()
}

// marshalText calls the script MarshalText method.
//
// Returns the text and any error.
func (o slogOperand) marshalText() ([]byte, error) {
	results, err := o.Invoke("MarshalText")
	if err != nil {
		return nil, err
	}
	if len(results) != 2 {
		return nil, fmt.Errorf("%w: MarshalText of %s", fault.ErrSlogOperandResult, o.typeName)
	}
	text, ok := reflect.TypeAssert[[]byte](results[0])
	if !ok && results[0].IsValid() && results[0].Kind() == reflect.Slice && results[0].Type().Elem().Kind() == reflect.Uint8 {
		text, ok = results[0].Bytes(), true
	}
	if !ok {
		return nil, fmt.Errorf("%w: MarshalText of %s", fault.ErrSlogOperandResult, o.typeName)
	}
	return text, o.ResultError(results, 1)
}

// Error calls the script Error method.
//
// Returns string which is its result.
func (o slogOperandError) Error() string { return o.errorText() }

// Error calls the script Error method.
//
// Returns string which is its result.
func (o slogOperandErrorText) Error() string { return o.errorText() }

// MarshalText calls the script MarshalText method.
//
// Returns the text and any error.
func (o slogOperandErrorText) MarshalText() ([]byte, error) { return o.marshalText() }

// MarshalJSON encodes the value through the marshal shadow.
//
// Returns the encoding and any error.
func (o slogOperandJSON) MarshalJSON() ([]byte, error) { return o.marshalJSON() }

// MarshalJSON encodes the value through the marshal shadow.
//
// Returns the encoding and any error.
func (o slogOperandJSONText) MarshalJSON() ([]byte, error) { return o.marshalJSON() }

// MarshalText calls the script MarshalText method.
//
// Returns the text and any error.
func (o slogOperandJSONText) MarshalText() ([]byte, error) { return o.marshalText() }

// MarshalJSON calls the script MarshalJSON method through the marshal shadow.
//
// Returns the encoding and any error.
func (o slogOperandJSONError) MarshalJSON() ([]byte, error) { return o.marshalJSON() }

// Error calls the script Error method.
//
// Returns string which is its result.
func (o slogOperandJSONError) Error() string { return o.errorText() }

// MarshalJSON calls the script MarshalJSON method through the marshal shadow.
//
// Returns the encoding and any error.
func (o slogOperandJSONErrorText) MarshalJSON() ([]byte, error) { return o.marshalJSON() }

// Error calls the script Error method.
//
// Returns string which is its result.
func (o slogOperandJSONErrorText) Error() string { return o.errorText() }

// MarshalText calls the script MarshalText method.
//
// Returns the text and any error.
func (o slogOperandJSONErrorText) MarshalText() ([]byte, error) { return o.marshalText() }

// LogValue calls the script LogValue method.
//
// Returns slog.Value which is its result, or a string value naming the failure.
func (o slogOperandLogValuer) LogValue() slog.Value {
	results := o.InvokeRecordingError("LogValue")
	if len(results) == 1 {
		if value, ok := reflect.TypeAssert[slog.Value](results[0]); ok {
			return value
		}
	}
	return slog.StringValue(fmt.Sprintf("%%!v(%v: LogValue of %s)", fault.ErrSlogOperandResult, o.typeName))
}

// slogOperandFor wraps a script value bound for one of log/slog's `any` parameters in the
// operand adapter variant matching the methods its type declares.
//
// Takes vm (*VM) which owns the method tables.
// Takes argument (reflect.Value) which is the operand.
// Takes typeCtx (argumentTypeContext) which carries the call site's static type name.
//
// Returns reflect.Value which is the operand to pass.
func slogOperandFor(vm *VM, argument reflect.Value, typeCtx argumentTypeContext) reflect.Value {
	if !argument.IsValid() || !argument.CanInterface() {
		return argument
	}
	typeName := resolveAdapterTypeName(vm, argument, typeCtx)
	methods := slogOperandMethodsOf(vm, argument, typeName)
	if len(methods) == 0 && !typeContainsPipitSynthesised(argument.Type()) {
		return argument
	}
	vm.exportGuestCode()
	core := slogOperand{AdapterCore: &AdapterCore{vm: vm, underlying: adapterHeldValue(vm, argument), typeName: typeName, methods: methods}}
	return slogOperandVariant(core, methods)
}

// slogOperandMethodsOf finds the slog operand methods the script type typeName declares
// that argument's receiver can call.
//
// Takes vm (*VM) which owns the method tables.
// Takes argument (reflect.Value) which is the operand.
// Takes typeName (string) which is its script type's name, or empty for none.
//
// Returns map[string]adapterMethodRef which maps each method found, nil when none is.
func slogOperandMethodsOf(vm *VM, argument reflect.Value, typeName string) map[string]adapterMethodRef {
	if typeName == "" {
		return nil
	}
	var methods map[string]adapterMethodRef
	for _, method := range slogOperandMethods {
		root, index, found := lookupAdapterMethod(vm, typeName+"."+method)
		if !found || !methodReceiverSatisfiesValueIn(root, vm, index, argument) {
			continue
		}
		if methods == nil {
			methods = make(map[string]adapterMethodRef, len(slogOperandMethods))
		}
		methods[method] = adapterMethodRef{root: root, index: index, path: nil}
	}
	return methods
}

// slogOperandVariant picks the operand adapter variant whose methods match those the type
// declares, so each handler finds exactly the interfaces the script type has.
//
// Takes core (slogOperand) which carries the operand.
// Takes methods (map[string]adapterMethodRef) which are the methods found.
//
// Returns reflect.Value which is the adapter.
func slogOperandVariant(core slogOperand, methods map[string]adapterMethodRef) reflect.Value {
	_, hasError := methods["Error"]
	_, hasText := methods["MarshalText"]
	_, hasJSON := methods["MarshalJSON"]
	switch _, hasLogValue := methods["LogValue"]; {
	case hasLogValue:
		return reflect.ValueOf(slogOperandLogValuer{core})
	case hasError && !hasJSON && hasText:
		return reflect.ValueOf(slogOperandErrorText{core})
	case hasError && !hasJSON:
		return reflect.ValueOf(slogOperandError{core})
	case hasError && hasText:
		return reflect.ValueOf(slogOperandJSONErrorText{core})
	case hasError:
		return reflect.ValueOf(slogOperandJSONError{core})
	case hasText:
		return reflect.ValueOf(slogOperandJSONText{core})
	default:
		return reflect.ValueOf(slogOperandJSON{core})
	}
}

// adapterHeldValueInterface returns the interface value a held reflect.Value carries, or
// nil for an invalid one.
//
// Takes value (reflect.Value) which is the held value.
//
// Returns any which is the value.
func adapterHeldValueInterface(value reflect.Value) any {
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	return value.Interface()
}
