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

package fault

import "errors"

var (
	// ErrLiteralElementLimit is returned when a composite literal (slice, array, map) has
	// more elements than the configured maximum set via WithMaxLiteralElements.
	ErrLiteralElementLimit = errors.New("literal element count limit exceeded")

	// ErrSpillAreaExhausted is returned (via panic recovered by the compile-time recover)
	// when a per-bank spill area would exceed the uint16 slot-index limit. Surfacing the
	// failure prevents the uint16 cast from silently wrapping and aliasing two logical slots
	// onto the same runtime register.
	ErrSpillAreaExhausted = errors.New("spill area exhausted")

	// ErrSpillUnsupportedBank is returned when a typed-slice register bank would spill. Only
	// the seven scalar and general banks support spill and reload.
	ErrSpillUnsupportedBank = errors.New("register bank does not support spilling")

	// ErrCompileNamedScalarPoolExhausted is returned when a program declares more named
	// basic types of one kind than the process-wide named-scalar pool can give distinct
	// runtime identities to. Compilation fails rather than letting the type collapse to its
	// underlying type, where assertions and method dispatch would silently go wrong.
	ErrCompileNamedScalarPoolExhausted = errors.New("named-scalar pool exhausted: too many named basic types of one kind in this process")

	// ErrCompilePackageVariableNotSettable is returned when a program assigns to a
	// package-qualified symbol of a registered Go package that is not a settable variable (a
	// function, a constant, or a variable registered by value).
	ErrCompilePackageVariableNotSettable = errors.New("package symbol is not a settable variable")

	// ErrCompileEmbedUnsupported is returned when a source file carries a //go:embed
	// directive. Embedding is a build-time feature of the gc toolchain with no counterpart
	// in an interpreter; the message points at the run-time alternatives.
	ErrCompileEmbedUnsupported = errors.New("//go:embed is not supported; read files at run time with os.ReadFile(filepath.Join(os.Getenv(\"PIPIT_SCRIPT_DIR\"), name)) or pipit/fs")

	// ErrCompileTypeConversionArgCount is returned when a type conversion is invoked with
	// anything other than exactly one argument.
	ErrCompileTypeConversionArgCount = errors.New("type conversion requires exactly 1 argument")

	// ErrCompileDereferenceRequiresPointer is returned when the unary * operator is applied
	// to an operand whose register kind is not general (i.e. not a boxed pointer).
	ErrCompileDereferenceRequiresPointer = errors.New("dereference requires pointer in general register")

	// ErrCompileDereferenceAssignRequiresPointer is returned when an assignment through a
	// pointer dereference (*p = v) targets an expression whose register kind is not general.
	ErrCompileDereferenceAssignRequiresPointer = errors.New("dereference assignment requires pointer in general register")

	// ErrCompileMapLiteralExpectKeyValue is returned when a map literal element is not in
	// key-value form.
	ErrCompileMapLiteralExpectKeyValue = errors.New("expected key-value in map literal")

	// ErrCompileSliceIndexMustBeInteger is returned when a slice or array index expression
	// has a non-integer register kind after conversion.
	ErrCompileSliceIndexMustBeInteger = errors.New("slice index must be integer")

	// ErrCompileUnaryMinusUnsupported is returned when unary - is applied to a register kind
	// that has no negation handler.
	ErrCompileUnaryMinusUnsupported = errors.New("unary - not supported for this type")

	// ErrCompileUnaryXorRequiresInteger is returned when unary ^ is applied to an operand
	// whose register kind is not int or uint.
	ErrCompileUnaryXorRequiresInteger = errors.New("unary ^ requires integer operand")

	// ErrCompileChannelReceiveRequiresGeneral is returned when the channel receive operator
	// <- is applied to an operand whose register kind is not general.
	ErrCompileChannelReceiveRequiresGeneral = errors.New("channel receive requires general register operand")

	// ErrCompileBreakOutsideLoopOrSwitch is returned when a break statement is compiled
	// outside an enclosing loop or switch.
	ErrCompileBreakOutsideLoopOrSwitch = errors.New("break outside loop or switch")

	// ErrCompileContinueOutsideLoop is returned when a continue statement is compiled
	// outside an enclosing loop.
	ErrCompileContinueOutsideLoop = errors.New("continue outside loop")

	// ErrCompileFallthroughOutsideSwitch is returned when a fallthrough statement is
	// compiled outside an enclosing switch.
	ErrCompileFallthroughOutsideSwitch = errors.New("fallthrough outside switch")

	// ErrCompileTailCallTargetNotIdent is returned when a tail call's callee expression is
	// not a bare identifier.
	ErrCompileTailCallTargetNotIdent = errors.New("tail call target is not an identifier")

	// ErrCompileTypeSwitchAssignNotTypeAssert is returned when the right-hand side of a
	// type-switch assignment is not a type assertion expression.
	ErrCompileTypeSwitchAssignNotTypeAssert = errors.New("type switch assign RHS is not a type assertion")

	// ErrCompileTypeSwitchExprNotTypeAssert is returned when a type-switch expression
	// statement is not a type assertion.
	ErrCompileTypeSwitchExprNotTypeAssert = errors.New("type switch expression is not a type assertion")

	// ErrCompileMethodExprMissingReceiver is returned when a method expression call has no
	// receiver argument.
	ErrCompileMethodExprMissingReceiver = errors.New("method expression call missing receiver argument")

	// ErrCompileIncDecSelectorNumeric is returned when an inc/dec on a struct field selector
	// targets a non-numeric field kind.
	ErrCompileIncDecSelectorNumeric = errors.New("inc/dec on selector requires numeric field")

	// ErrCompileIncDecRequiresNumeric is returned when an inc/dec statement targets a
	// variable whose kind is not int, float, or uint.
	ErrCompileIncDecRequiresNumeric = errors.New("inc/dec requires numeric variable")

	// ErrCompileMapCommaOkValueNotIdent is returned when the value target of a map comma-ok
	// assignment is not a bare identifier.
	ErrCompileMapCommaOkValueNotIdent = errors.New("map comma-ok value target is not an identifier")

	// ErrCompileMapCommaOkOkNotIdent is returned when the ok target of a map comma-ok
	// assignment is not a bare identifier.
	ErrCompileMapCommaOkOkNotIdent = errors.New("map comma-ok ok target is not an identifier")

	// ErrCompileMapCommaOkSourceNotMap is returned when a `:=` map comma-ok is compiled but
	// the source expression is not a map type.
	ErrCompileMapCommaOkSourceNotMap = errors.New("map comma-ok source is not a map type")

	// ErrCompileChanRecvCommaOkSourceNotChan is returned when a channel-receive comma-ok is
	// compiled but the source expression is not a channel type.
	ErrCompileChanRecvCommaOkSourceNotChan = errors.New("channel receive comma-ok source is not a channel type")

	// ErrCompileChanRecvCommaOkValueNotIdent is returned when the value target of a
	// channel-receive comma-ok is not a bare identifier.
	ErrCompileChanRecvCommaOkValueNotIdent = errors.New("channel receive comma-ok value target is not an identifier")

	// ErrCompileChanRecvCommaOkOkNotIdent is returned when the ok target of a
	// channel-receive comma-ok is not a bare identifier.
	ErrCompileChanRecvCommaOkOkNotIdent = errors.New("channel receive comma-ok ok target is not an identifier")

	// ErrCompileTypeAssertCommaOkValueNotIdent is returned when the value target of a
	// type-assert comma-ok is not a bare identifier.
	ErrCompileTypeAssertCommaOkValueNotIdent = errors.New("type assert comma-ok value target is not an identifier")

	// ErrCompileTypeAssertCommaOkOkNotIdent is returned when the ok target of a type-assert
	// comma-ok is not a bare identifier.
	ErrCompileTypeAssertCommaOkOkNotIdent = errors.New("type assert comma-ok ok target is not an identifier")

	// ErrCompileArithFloatUnsupported is returned when an arithmetic opcode has no
	// float-bank counterpart.
	ErrCompileArithFloatUnsupported = errors.New("operation not supported for float")

	// ErrCompileArithStringUnsupported is returned when an arithmetic opcode has no
	// string-bank counterpart.
	ErrCompileArithStringUnsupported = errors.New("operation not supported for string")

	// ErrCompileArithGeneralUnsupported is returned when an arithmetic opcode has no
	// general-bank counterpart.
	ErrCompileArithGeneralUnsupported = errors.New("operation not supported for this type")

	// ErrCompileArithUintUnsupported is returned when an arithmetic or bitwise opcode has no
	// uint-bank counterpart.
	ErrCompileArithUintUnsupported = errors.New("operation not supported for uint")

	// ErrCompileArithComplexUnsupported is returned when an arithmetic opcode has no
	// complex-bank counterpart.
	ErrCompileArithComplexUnsupported = errors.New("operation not supported for complex")

	// ErrCompileCompareFloatUnsupported is returned when a comparison opcode has no
	// float-bank counterpart.
	ErrCompileCompareFloatUnsupported = errors.New("comparison not supported for float")

	// ErrCompileCompareStringUnsupported is returned when a comparison opcode has no
	// string-bank counterpart.
	ErrCompileCompareStringUnsupported = errors.New("comparison not supported for string")

	// ErrCompileCompareGeneralUnsupported is returned when a comparison opcode has no
	// general-bank counterpart.
	ErrCompileCompareGeneralUnsupported = errors.New("comparison not supported for this type")

	// ErrCompileCompareComplexOrdering is returned when an ordering comparison (<, <=, >,
	// >=) is applied to complex operands, which Go only allows for == and !=.
	ErrCompileCompareComplexOrdering = errors.New("comparison not supported for complex (only == and !=)")

	// ErrCompileBitwiseRequiresInteger is returned when a bitwise or shift operation
	// receives a left operand whose register kind is not int or uint.
	ErrCompileBitwiseRequiresInteger = errors.New("operation requires integer operands")

	// ErrCompileBuiltinLenArgCount is returned when len is called with anything other than
	// exactly one argument.
	ErrCompileBuiltinLenArgCount = errors.New("len requires exactly 1 argument")

	// ErrCompileBuiltinAppendArgCount is returned when append is called with no arguments.
	ErrCompileBuiltinAppendArgCount = errors.New("append requires at least 1 argument")

	// ErrCompileBuiltinDeleteArgCount is returned when delete is called with anything other
	// than exactly two arguments.
	ErrCompileBuiltinDeleteArgCount = errors.New("delete requires exactly 2 arguments")

	// ErrCompileBuiltinComplexArgCount is returned when complex is called with anything
	// other than exactly two arguments.
	ErrCompileBuiltinComplexArgCount = errors.New("complex requires exactly 2 arguments")

	// ErrCompileBuiltinComplexRequiresFloat is returned when complex is called with operands
	// whose register kind is not float.
	ErrCompileBuiltinComplexRequiresFloat = errors.New("complex requires float arguments")

	// ErrCompileBuiltinCapArgCount is returned when cap is called with anything other than
	// exactly one argument.
	ErrCompileBuiltinCapArgCount = errors.New("cap requires exactly 1 argument")

	// ErrCompileBuiltinCopyArgCount is returned when copy is called with anything other than
	// exactly two arguments.
	ErrCompileBuiltinCopyArgCount = errors.New("copy requires exactly 2 arguments")

	// ErrCompileBuiltinClearArgCount is returned when clear is called with anything other
	// than exactly one argument.
	ErrCompileBuiltinClearArgCount = errors.New("clear requires exactly 1 argument")

	// ErrCompileBuiltinMinMaxArgCount is returned when min or max is called with fewer than
	// two arguments.
	ErrCompileBuiltinMinMaxArgCount = errors.New("min/max requires at least 2 arguments")

	// ErrCompileBuiltinPanicArgCount is returned when panic is called with anything other
	// than exactly one argument.
	ErrCompileBuiltinPanicArgCount = errors.New("panic requires exactly 1 argument")

	// ErrCompileBuiltinCloseArgCount is returned when close is called with anything other
	// than exactly one argument.
	ErrCompileBuiltinCloseArgCount = errors.New("close expects 1 argument")

	// ErrCompileUnsafeStringDataArgCount is returned when unsafe.StringData is called with
	// anything other than exactly one argument.
	ErrCompileUnsafeStringDataArgCount = errors.New("unsafe.StringData requires 1 argument")

	// ErrCompileUnsafeSliceDataArgCount is returned when unsafe.SliceData is called with
	// anything other than exactly one argument.
	ErrCompileUnsafeSliceDataArgCount = errors.New("unsafe.SliceData requires 1 argument")

	// ErrCompileGenericMethodNotSpecialisable reports a Go 1.27 generic method whose type
	// arguments could not be resolved to concrete types. A generic method has no correct
	// erased lowering, so this is an error rather than a fallback.
	ErrCompileGenericMethodNotSpecialisable = errors.New("generic method cannot be specialised")

	// ErrCompileGenericMethodSpecialisationLimit reports a generic method that exceeded a
	// specialisation cap. Diagnosable ceiling rather than silent erasure.
	ErrCompileGenericMethodSpecialisationLimit = errors.New("generic method exceeded the specialisation limit")

	// ErrCompileGenericMethodTypeArgLimit reports a generic method whose receiver type
	// parameters and own type parameters together exceed the specialisation key width.
	ErrCompileGenericMethodTypeArgLimit = errors.New("generic method has too many type arguments to specialise")

	// ErrCompileGenericCallerRequiresSpecialisation reports a generic function that had to
	// be monomorphised because its body calls a generic method with the caller's own type
	// parameters, but whose call site could not be specialised.
	ErrCompileGenericCallerRequiresSpecialisation = errors.New("generic function calling a generic method must be specialised")

	// ErrCompileGenericValueNotSpecialisable reports an instantiated generic used as a value
	// that could not be monomorphised. Unlike a call, a value has no concrete arguments to
	// fall back on.
	ErrCompileGenericValueNotSpecialisable = errors.New("generic value cannot be specialised")

	// ErrCompileUninstantiatedGeneric reports a multi-parameter generic instantiation used
	// as a value that go/types recorded no instantiation for. An IndexListExpr has no
	// non-generic reading, so there is nothing to fall back to.
	ErrCompileUninstantiatedGeneric = errors.New("generic instantiation has no recorded type arguments")

	// ErrCompileInvalidStructLiteralKey reports a struct-literal key that is not a plain
	// identifier. Go 1.27 widened keys to name promoted fields, but the key is still an
	// identifier resolved as a field selector, so a dotted or computed key is invalid.
	ErrCompileInvalidStructLiteralKey = errors.New("invalid field name in struct literal")

	// ErrLinkedCallNoInstance reports that go/types has no instantiation recorded for a
	// //piko:link call site. Usually the source is invalid or the expression was not a
	// generic call.
	ErrLinkedCallNoInstance = errors.New("//piko:link call target has no type instantiation")

	// ErrLinkedCallArityMismatch reports a type-argument count that disagrees with the
	// LinkedFunction's declared TypeArgCount.
	ErrLinkedCallArityMismatch = errors.New("//piko:link call target type argument count mismatch")

	// ErrLinkedCallTypeArgUnresolvable reports a type argument that cannot be converted from
	// go/types to reflect.Type.
	ErrLinkedCallTypeArgUnresolvable = errors.New("//piko:link call target cannot resolve type argument")

	// ErrLinkedCallTooManyTypeArgs reports a LinkedFunction sentinel whose declared
	// TypeArgCount exceeds symtab.MaxLinkedTypeArgCount. Guards against a malformed or
	// hostile registration driving unbounded allocation through resolveLinkedTypeArgs.
	ErrLinkedCallTooManyTypeArgs = errors.New("//piko:link call target declares too many type arguments")
)
