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
	"runtime"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// fmtFormatParameterSpan is the format string's offset from the parameter-list end.
const fmtFormatParameterSpan = 2

// isFmtScanName reports whether a symbol name is one of package fmt's scanning functions.
//
// Takes name (string) which is the symbol name runtime.FuncForPC reports.
//
// Returns bool which is true for a fmt scanning function.
func isFmtScanName(name string) bool {
	return strings.HasPrefix(name, "fmt.Scan") || strings.HasPrefix(name, "fmt.Sscan") || strings.HasPrefix(name, "fmt.Fscan")
}

// isFmtVariadicAny reports whether the native is a fmt printing function taking ...any
// (Println, Printf, Fprintln and the rest), whose operands the intercept must wrap.
//
// Takes reflectedFunction (reflect.Value) which is the native function.
// Takes functionType (reflect.Type) which is its signature.
//
// Returns true for fmt's variadic ...any functions.
func isFmtVariadicAny(reflectedFunction reflect.Value, functionType reflect.Type) bool {
	if !functionType.IsVariadic() || functionType.NumIn() == 0 {
		return false
	}
	last := functionType.In(functionType.NumIn() - 1)
	if last.Kind() != reflect.Slice || last.Elem().Kind() != reflect.Interface || last.Elem().NumMethod() != 0 {
		return false
	}
	symbol := runtime.FuncForPC(reflectedFunction.Pointer())
	return symbol != nil && strings.HasPrefix(symbol.Name(), "fmt.")
}

// rewriteFmtTypeVerbs rewrites %T verbs in the format string so that Printf, Fprintf and
// Errorf name script types correctly.
//
// fmt resolves %T without consulting Formatter, so rewriting the verb before the call is
// the only way to print a pool-backed named type.
//
// Takes site (*program.CallSite) which carries the static argument type strings.
// Takes arguments ([]reflect.Value) which are the coerced call arguments, edited in
// place.
// Takes parameterTypes ([]reflect.Type) which are the native's parameter types.
func rewriteFmtTypeVerbs(site *program.CallSite, arguments []reflect.Value, parameterTypes []reflect.Type) {
	parameterCount := len(parameterTypes)
	formatIndex := parameterCount - 2
	if formatIndex < 0 || parameterTypes[formatIndex].Kind() != reflect.String || formatIndex >= len(arguments) {
		return
	}
	format := arguments[formatIndex].String()
	if !strings.ContainsRune(format, 'T') {
		return
	}
	variadicStart := parameterCount - 1
	if site.IsEllipsisSpread {
		rewriteFmtTypeVerbsSpread(site, arguments, formatIndex, variadicStart, format)
		return
	}
	varArgs := make([]any, 0, len(arguments)-variadicStart)
	for _, argument := range arguments[variadicStart:] {
		varArgs = append(varArgs, interfaceOrNil(argument))
	}
	rewrittenFormat, rewrittenArgs, intercepted := interceptFmtFormat(site, variadicStart, format, varArgs)
	if !intercepted {
		return
	}
	arguments[formatIndex] = reflect.ValueOf(rewrittenFormat)
	for i, rewritten := range rewrittenArgs {
		if text, ok := rewritten.(string); ok {
			arguments[variadicStart+i] = reflect.ValueOf(text)
		}
	}
}

// rewriteFmtTypeVerbsSpread rewrites %T verbs for a call that spreads a slice into the
// variadic parameter (e.g. fmt.Sprintf(format, args...)).
//
// Elements come from the script's slice, so rewrites go into a fresh slice. No static
// type string describes an element, so every %T resolves from the element's runtime type.
//
// Takes site (*program.CallSite) which is the call site.
// Takes arguments ([]reflect.Value) which are the coerced call arguments, edited in
// place.
// Takes formatIndex (int) which is the format string's argument index.
// Takes sliceIndex (int) which is the spread slice's argument index.
// Takes format (string) which is the format string.
func rewriteFmtTypeVerbsSpread(site *program.CallSite, arguments []reflect.Value, formatIndex, sliceIndex int, format string) {
	if sliceIndex >= len(arguments) {
		return
	}
	slice := arguments[sliceIndex]
	if !slice.IsValid() || slice.Kind() != reflect.Slice {
		return
	}
	varArgs := make([]any, slice.Len())
	for i := range varArgs {
		varArgs[i] = interfaceOrNil(slice.Index(i))
	}
	rewrittenFormat, rewrittenArgs, intercepted := interceptFmtFormat(site, len(site.Arguments), format, varArgs)
	if !intercepted {
		return
	}
	arguments[formatIndex] = reflect.ValueOf(rewrittenFormat)
	fresh := reflect.MakeSlice(slice.Type(), len(rewrittenArgs), len(rewrittenArgs))
	for i, rewritten := range rewrittenArgs {
		if rewritten == nil {
			continue
		}
		fresh.Index(i).Set(reflect.ValueOf(rewritten))
	}
	arguments[sliceIndex] = fresh
}

// interfaceOrNil returns the Go value held by argument, or nil when the value cannot be
// read as an interface (invalid, or an unexported field).
//
// Takes argument (reflect.Value) which is the value to read.
//
// Returns any which is the value or nil.
func interfaceOrNil(argument reflect.Value) any {
	if !argument.IsValid() || !argument.CanInterface() {
		return nil
	}
	return argument.Interface()
}

// wrapFmtArgument routes a coerced `...any` argument of a fmt scanning call through
// wrapPipitSynthesisedFmtArg so fmt sees adapters for synthesised types. Nil interfaces
// are left untouched, and so is an adapter, which a Scan method needs.
//
// Takes vm (*VM) which provides the method registry for adapters.
// Takes argument (reflect.Value) which is the coerced argument.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns the argument, wrapped when its dynamic type is pipit-synthesised.
func wrapFmtArgument(vm *VM, argument reflect.Value, staticTypeName string) reflect.Value {
	return wrapFmtOperand(argument, func(operand any) any {
		return wrapPipitSynthesisedFmtArg(vm, operand, staticTypeName)
	})
}

// renderStringKindPrintOperands renders wrapped strings for unformatted fmt calls.
//
// It runs once the call has been allowed, so a String method of the script's runs only
// for a call that goes ahead, and in the order fmt would run it. A spread slice is
// rendered into a copy, never into the script's own slice.
//
// Takes function (reflect.Value) which is the native about to be called.
// Takes arguments ([]reflect.Value) which are its arguments, edited in place.
func renderStringKindPrintOperands(function reflect.Value, arguments []reflect.Value) {
	functionType := function.Type()
	parameterCount := functionType.NumIn()
	if !functionType.IsVariadic() || parameterCount == 0 ||
		(parameterCount >= fmtFormatParameterSpan && functionType.In(parameterCount-fmtFormatParameterSpan).Kind() == reflect.String) {
		return
	}
	if !isFmtVariadicAny(function, functionType) {
		return
	}
	if symbol := runtime.FuncForPC(function.Pointer()); symbol != nil && isFmtScanName(symbol.Name()) {
		return
	}
	variadicStart := parameterCount - 1
	for index := variadicStart; index < len(arguments); index++ {
		arguments[index] = renderOperandOrSpread(arguments[index])
	}
}

// renderOperandOrSpread renders one variadic operand, or every element of a spread slice.
//
// Takes argument (reflect.Value) which is an operand or the spread slice.
//
// Returns reflect.Value which is the argument, or a copy with rendered elements.
func renderOperandOrSpread(argument reflect.Value) reflect.Value {
	if !argument.IsValid() || argument.Kind() != reflect.Slice || argument.Type().Elem().Kind() != reflect.Interface {
		return keepStringKindForPrint(fmtOperandHeldValue(argument), argument)
	}
	rendered := argument
	for index := range argument.Len() {
		element := argument.Index(index)
		if element.IsNil() {
			continue
		}
		kept := keepStringKindForPrint(fmtOperandHeldValue(element.Elem()), element.Elem())
		if kept.Kind() != reflect.String || element.Elem().Kind() == reflect.String {
			continue
		}
		if rendered.Pointer() == argument.Pointer() {
			rendered = reflect.MakeSlice(argument.Type(), argument.Len(), argument.Len())
			reflect.Copy(rendered, argument)
		}
		rendered.Index(index).Set(kept)
	}
	return rendered
}

// fmtOperandHeldValue unwraps the script value represented by a fmt operand.
//
// Takes operand (reflect.Value) which is the operand handed to fmt.
//
// Returns reflect.Value which is the value it holds.
func fmtOperandHeldValue(operand reflect.Value) reflect.Value {
	if !operand.IsValid() || !operand.CanInterface() {
		return operand
	}
	switch held := operand.Interface().(type) {
	case pipitFmtValue:
		return held.underlying
	case adapterUnwrapper:
		return held.adapterUnderlying()
	default:
		return operand
	}
}

// keepStringKindForPrint renders a wrapped operand to the string fmt would print for it
// when the operand is of a string kind and its wrapper is not.
//
// Print, Sprint, Fprint and Append put a space between two operands when neither is of a
// string kind, so a value of a named string type that is handed to fmt inside a wrapper,
// for its methods to be found, would gain spaces Go does not print.
//
// Takes argument (reflect.Value) which is the operand before wrapping.
// Takes wrapped (reflect.Value) which is the operand after wrapping.
//
// Returns reflect.Value which is wrapped, or the string it prints as.
func keepStringKindForPrint(argument, wrapped reflect.Value) reflect.Value {
	if !argument.IsValid() || !wrapped.IsValid() || !wrapped.CanInterface() || wrapped.Kind() == reflect.String {
		return wrapped
	}
	if !argument.CanInterface() || isNilInterfaceValue(argument) {
		return wrapped
	}
	operand := reflect.ValueOf(unwrapFmtAdapter(argument.Interface()))
	if operand.Kind() != reflect.String {
		return wrapped
	}
	return reflect.ValueOf(fmt.Sprint(wrapped.Interface()))
}

// wrapFmtArgumentForVerb is wrapFmtArgument for an operand whose verb is known (see
// wrapPipitSynthesisedFmtArgForVerb).
//
// Takes vm (*VM) which resolves script method tables.
// Takes argument (reflect.Value) which is the operand.
// Takes verb (byte) which is the operand's verb.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns reflect.Value which is the operand to hand to fmt.
func wrapFmtArgumentForVerb(vm *VM, argument reflect.Value, verb byte, staticTypeName string) reflect.Value {
	return wrapFmtOperand(argument, func(operand any) any {
		return wrapPipitSynthesisedFmtArgForVerb(vm, operand, verb, staticTypeName)
	})
}

// wrapFmtOperand applies wrap to an operand that fmt can receive, keeping the operand
// when it is invalid, cannot be turned into an interface, is a nil interface, or wrap
// returns nil.
//
// Takes argument (reflect.Value) which is the operand.
// Takes wrap (func(any) any) which returns the operand's wrapper, or nil.
//
// Returns reflect.Value which is the operand to hand to fmt.
func wrapFmtOperand(argument reflect.Value, wrap func(operand any) any) reflect.Value {
	if !argument.IsValid() || !argument.CanInterface() || isNilInterfaceValue(argument) {
		return argument
	}
	wrapped := wrap(argument.Interface())
	if wrapped == nil {
		return argument
	}
	return reflect.ValueOf(wrapped)
}

// fmtVerbsForCall scans the format operand of a Printf-style native call (the parameter
// before the variadic one, when it is a string) and returns the verb of each variadic
// operand. It is nil when the call has no format string (Print, Println, Sprint).
//
// Takes registers (*Registers) which hold the operands.
// Takes site (*program.CallSite) which describes the call.
// Takes parameterTypes ([]reflect.Type) which are the callee's parameter types.
//
// Returns []byte which is nil or one verb per variadic operand.
func fmtVerbsForCall(registers *Registers, site *program.CallSite, parameterTypes []reflect.Type) []byte {
	format, ok := fmtFormatOperand(registers, site, parameterTypes)
	if !ok {
		return nil
	}
	variadicCount := len(site.Arguments) - (len(parameterTypes) - 1)
	if variadicCount <= 0 {
		return nil
	}
	return fmtArgumentVerbs(format, variadicCount)
}

// fmtFormatOperand reads a string-register format argument immediately before the
// variadic parameter of a Printf-style call.
//
// Takes registers (*Registers) which hold the operands.
// Takes site (*program.CallSite) which describes the call.
// Takes parameterTypes ([]reflect.Type) which are the callee's parameter types.
//
// Returns string which is the format string.
// Returns bool which is false when the call has no format operand.
func fmtFormatOperand(registers *Registers, site *program.CallSite, parameterTypes []reflect.Type) (string, bool) {
	formatIndex := len(parameterTypes) - 2
	if formatIndex < 0 || parameterTypes[formatIndex].Kind() != reflect.String || formatIndex >= len(site.Arguments) {
		return "", false
	}
	formatLocation := site.Arguments[formatIndex]
	if formatLocation.Kind != isa.RegisterString {
		return "", false
	}
	return registers.Strings[formatLocation.Register], true
}

// fmtVerbAt returns the verb recorded for variadic operand index, or 'v' when the call
// has no format string or the index is out of range.
//
// Takes verbs ([]byte) which may be nil.
// Takes index (int) which is the variadic operand position.
//
// Returns byte which is the verb.
func fmtVerbAt(verbs []byte, index int) byte {
	if index < 0 || index >= len(verbs) {
		return 'v'
	}
	return verbs[index]
}
