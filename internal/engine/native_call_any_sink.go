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
	"errors"
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"

	"pipit.sh/pipit/internal/fault"
)

const (
	// anySinkValue is a native that receives a script value passed as `any` as the value
	// itself, with its own runtime type, exactly as compiled Go would pass it.
	anySinkValue nativeAnySink = iota

	// anySinkPrint is a printing function of package fmt or log. Its operands are wrapped so
	// formatting reaches script String, Error, Format and GoString methods.
	anySinkPrint

	// anySinkScan is a scanning function of package fmt. An operand whose type has a script
	// Scan method reaches it through the fmt.Scanner adapter.
	anySinkScan

	// anySinkJSONEncode is an encoding/json encoder. Nested script values with a MarshalJSON
	// or MarshalText method reach it through the marshal shadow.
	anySinkJSONEncode

	// anySinkJSONDecode is an encoding/json decoder.
	anySinkJSONDecode

	// anySinkXMLEncode is an encoding/xml encoder. Nested script values with a MarshalText
	// method reach it through the marshal shadow.
	anySinkXMLEncode

	// anySinkXMLDecode is an encoding/xml decoder. Targets are decoded into directly.
	anySinkXMLDecode

	// anySinkGobEncode is an encoding/gob encoder. Values are encoded directly.
	anySinkGobEncode

	// anySinkGobDecode is an encoding/gob decoder. Targets are decoded into directly.
	anySinkGobDecode

	// anySinkJSONv2Encode is an encoding/json/v2 encoder. Nested script values with a
	// MarshalJSON or MarshalText method reach it through the marshal shadow, as for
	// encoding/json.
	anySinkJSONv2Encode

	// anySinkJSONv2Decode is an encoding/json/v2 decoder. A target whose type has a script
	// UnmarshalJSON or UnmarshalText method reaches it through the adapter.
	anySinkJSONv2Decode

	// anySinkSlog is a log/slog function or method.
	anySinkSlog

	// anySinkMethodsByName is a native that looks up exported methods by name through
	// reflect (net/rpc receivers), which no adapter can serve.
	anySinkMethodsByName

	// anySinkTemplate is a text/template or html/template execution.
	anySinkTemplate

	// anySinkSQL is a database/sql function or method. A script value whose type declares
	// Value (driver.Valuer) or Scan (sql.Scanner), which no adapter serves, is refused.
	anySinkSQL

	// anySinkUnknownMethodValue is a method value bound by reflect outside the VM, whose
	// receiver and method cannot be told from the function. A script value whose type
	// declares a method some classified native looks up is refused, since the method might
	// look it up too.
	anySinkUnknownMethodValue
)

const (
	// anySinkAnyExportedMethod stands in anySinkRefusedMethods for every exported method a
	// script type declares.
	anySinkAnyExportedMethod = "*"

	// methodValueSinkRecordMax bounds a VM's record of the method values it bound on native
	// receivers.
	methodValueSinkRecordMax = 1024

	// nativeMethodSymbolBuffer sizes the stack buffer nativeMethodAnySink builds a method's
	// symbol in, so the lookup allocates nothing for the usual symbol lengths.
	nativeMethodSymbolBuffer = 128

	// unknownMethodValueDescription names a method value bound outside the VM in errors.
	unknownMethodValueDescription = "a method value bound outside the interpreter"

	// interfaceHeldWalkNodeCap bounds the parts one walk of a decoder's target visits.
	interfaceHeldWalkNodeCap = 1 << 20

	// interfaceHoldingTypeMax bounds interfaceHoldingTypes.
	interfaceHoldingTypeMax = 4096

	// interfaceHeldWalkCheckInterval is how many parts a walk of a decoder's target visits
	// between charging the guest's cost budget and checking for cancellation.
	interfaceHeldWalkCheckInterval = 256
)

// errInterfaceHeldWalkTooLarge stops a walk of a decoder's target that reaches
// interfaceHeldWalkNodeCap parts. checkAnySinkArguments reports it as a refusal.
var errInterfaceHeldWalkTooLarge = errors.New("decoder target has too many parts to check")

var (
	// methodValueClassSequence numbers method value records in the order they are made, so a
	// full record can drop its older entries.
	methodValueClassSequence atomic.Uint64

	// interfaceHoldingTypes memoises typeCanHoldInterface by type, for at most
	// interfaceHoldingTypeMax types.
	interfaceHoldingTypes typedCache[reflect.Type, bool]

	// interfaceHoldingTypeCount counts interfaceHoldingTypes' entries.
	interfaceHoldingTypeCount atomic.Int64

	// nativeAnySinksByName classifies the natives whose `any` parameters need more than the
	// value itself, keyed by the symbol name runtime.FuncForPC reports (and the matching
	// name of a method called on a native receiver).
	nativeAnySinksByName = map[string]nativeAnySink{
		"encoding/json.Marshal":                     anySinkJSONEncode,
		"encoding/json.MarshalIndent":               anySinkJSONEncode,
		"encoding/json.(*Encoder).Encode":           anySinkJSONEncode,
		"encoding/json.Unmarshal":                   anySinkJSONDecode,
		"encoding/json.(*Decoder).Decode":           anySinkJSONDecode,
		"encoding/xml.Marshal":                      anySinkXMLEncode,
		"encoding/xml.MarshalIndent":                anySinkXMLEncode,
		"encoding/xml.(*Encoder).Encode":            anySinkXMLEncode,
		"encoding/xml.(*Encoder).EncodeElement":     anySinkXMLEncode,
		"encoding/xml.Unmarshal":                    anySinkXMLDecode,
		"encoding/xml.(*Decoder).Decode":            anySinkXMLDecode,
		"encoding/xml.(*Decoder).DecodeElement":     anySinkXMLDecode,
		"encoding/gob.(*Encoder).Encode":            anySinkGobEncode,
		"encoding/gob.(*Decoder).Decode":            anySinkGobDecode,
		"log.Print":                                 anySinkPrint,
		"log.Printf":                                anySinkPrint,
		"log.Println":                               anySinkPrint,
		"log.Fatal":                                 anySinkPrint,
		"log.Fatalf":                                anySinkPrint,
		"log.Fatalln":                               anySinkPrint,
		"log.Panic":                                 anySinkPrint,
		"log.Panicf":                                anySinkPrint,
		"log.Panicln":                               anySinkPrint,
		"log.(*Logger).Print":                       anySinkPrint,
		"log.(*Logger).Printf":                      anySinkPrint,
		"log.(*Logger).Println":                     anySinkPrint,
		"log.(*Logger).Fatal":                       anySinkPrint,
		"log.(*Logger).Fatalf":                      anySinkPrint,
		"log.(*Logger).Fatalln":                     anySinkPrint,
		"log.(*Logger).Panic":                       anySinkPrint,
		"log.(*Logger).Panicf":                      anySinkPrint,
		"log.(*Logger).Panicln":                     anySinkPrint,
		"encoding/json.(*Encoder).Encode-fm":        anySinkJSONEncode,
		"encoding/json.(*Decoder).Decode-fm":        anySinkJSONDecode,
		"encoding/xml.(*Encoder).Encode-fm":         anySinkXMLEncode,
		"encoding/xml.(*Encoder).EncodeElement-fm":  anySinkXMLEncode,
		"encoding/xml.(*Decoder).Decode-fm":         anySinkXMLDecode,
		"encoding/xml.(*Decoder).DecodeElement-fm":  anySinkXMLDecode,
		"encoding/gob.(*Encoder).Encode-fm":         anySinkGobEncode,
		"encoding/gob.(*Decoder).Decode-fm":         anySinkGobDecode,
		"encoding/json/v2.Marshal":                  anySinkJSONv2Encode,
		"encoding/json/v2.MarshalWrite":             anySinkJSONv2Encode,
		"encoding/json/v2.MarshalEncode":            anySinkJSONv2Encode,
		"encoding/json/v2.Unmarshal":                anySinkJSONv2Decode,
		"encoding/json/v2.UnmarshalRead":            anySinkJSONv2Decode,
		"encoding/json/v2.UnmarshalDecode":          anySinkJSONv2Decode,
		"log/slog.Any":                              anySinkSlog,
		"log/slog.AnyValue":                         anySinkSlog,
		"log/slog.Group":                            anySinkSlog,
		"log/slog.With":                             anySinkSlog,
		"log/slog.Debug":                            anySinkSlog,
		"log/slog.DebugContext":                     anySinkSlog,
		"log/slog.Info":                             anySinkSlog,
		"log/slog.InfoContext":                      anySinkSlog,
		"log/slog.Warn":                             anySinkSlog,
		"log/slog.WarnContext":                      anySinkSlog,
		"log/slog.Error":                            anySinkSlog,
		"log/slog.ErrorContext":                     anySinkSlog,
		"log/slog.Log":                              anySinkSlog,
		"log/slog.(*Logger).With":                   anySinkSlog,
		"log/slog.(*Logger).Debug":                  anySinkSlog,
		"log/slog.(*Logger).DebugContext":           anySinkSlog,
		"log/slog.(*Logger).Info":                   anySinkSlog,
		"log/slog.(*Logger).InfoContext":            anySinkSlog,
		"log/slog.(*Logger).Warn":                   anySinkSlog,
		"log/slog.(*Logger).WarnContext":            anySinkSlog,
		"log/slog.(*Logger).Error":                  anySinkSlog,
		"log/slog.(*Logger).ErrorContext":           anySinkSlog,
		"log/slog.(*Logger).Log":                    anySinkSlog,
		"log/slog.(*Record).Add":                    anySinkSlog,
		"log/slog.(*Logger).With-fm":                anySinkSlog,
		"log/slog.(*Logger).Debug-fm":               anySinkSlog,
		"log/slog.(*Logger).DebugContext-fm":        anySinkSlog,
		"log/slog.(*Logger).Info-fm":                anySinkSlog,
		"log/slog.(*Logger).InfoContext-fm":         anySinkSlog,
		"log/slog.(*Logger).Warn-fm":                anySinkSlog,
		"log/slog.(*Logger).WarnContext-fm":         anySinkSlog,
		"log/slog.(*Logger).Error-fm":               anySinkSlog,
		"log/slog.(*Logger).ErrorContext-fm":        anySinkSlog,
		"log/slog.(*Logger).Log-fm":                 anySinkSlog,
		"testing.(*T).Log":                          anySinkPrint,
		"testing.(*T).Logf":                         anySinkPrint,
		"testing.(*T).Error":                        anySinkPrint,
		"testing.(*T).Errorf":                       anySinkPrint,
		"testing.(*T).Fatal":                        anySinkPrint,
		"testing.(*T).Fatalf":                       anySinkPrint,
		"testing.(*T).Skip":                         anySinkPrint,
		"testing.(*T).Skipf":                        anySinkPrint,
		"testing.(*B).Log":                          anySinkPrint,
		"testing.(*B).Logf":                         anySinkPrint,
		"testing.(*B).Error":                        anySinkPrint,
		"testing.(*B).Errorf":                       anySinkPrint,
		"testing.(*B).Fatal":                        anySinkPrint,
		"testing.(*B).Fatalf":                       anySinkPrint,
		"testing.(*B).Skip":                         anySinkPrint,
		"testing.(*B).Skipf":                        anySinkPrint,
		"testing.(*F).Log":                          anySinkPrint,
		"testing.(*F).Logf":                         anySinkPrint,
		"testing.(*F).Error":                        anySinkPrint,
		"testing.(*F).Errorf":                       anySinkPrint,
		"testing.(*F).Fatal":                        anySinkPrint,
		"testing.(*F).Fatalf":                       anySinkPrint,
		"testing.(*F).Skip":                         anySinkPrint,
		"testing.(*F).Skipf":                        anySinkPrint,
		"net/textproto.(*Conn).Cmd":                 anySinkPrint,
		"net/textproto.(*Conn).PrintfLine":          anySinkPrint,
		"net/textproto.(*Writer).PrintfLine":        anySinkPrint,
		"html/template.HTMLEscaper":                 anySinkPrint,
		"html/template.JSEscaper":                   anySinkPrint,
		"html/template.URLQueryEscaper":             anySinkPrint,
		"text/template.HTMLEscaper":                 anySinkPrint,
		"text/template.JSEscaper":                   anySinkPrint,
		"text/template.URLQueryEscaper":             anySinkPrint,
		"text/template.(*Template).Execute":         anySinkTemplate,
		"text/template.(*Template).ExecuteTemplate": anySinkTemplate,
		"html/template.(*Template).Execute":         anySinkTemplate,
		"html/template.(*Template).ExecuteTemplate": anySinkTemplate,
		"net/rpc.Register":                          anySinkMethodsByName,
		"net/rpc.RegisterName":                      anySinkMethodsByName,
		"net/rpc.(*Server).Register":                anySinkMethodsByName,
		"net/rpc.(*Server).RegisterName":            anySinkMethodsByName,
		"net/rpc.(*Client).Call":                    anySinkGobEncode,
		"net/rpc.(*Client).Go":                      anySinkGobEncode,
		"database/sql.ConvertAssign":                anySinkSQL,
		"database/sql.Named":                        anySinkSQL,
		"database/sql.(*DB).Exec":                   anySinkSQL,
		"database/sql.(*DB).ExecContext":            anySinkSQL,
		"database/sql.(*DB).Query":                  anySinkSQL,
		"database/sql.(*DB).QueryContext":           anySinkSQL,
		"database/sql.(*DB).QueryRow":               anySinkSQL,
		"database/sql.(*DB).QueryRowContext":        anySinkSQL,
		"database/sql.(*Conn).ExecContext":          anySinkSQL,
		"database/sql.(*Conn).QueryContext":         anySinkSQL,
		"database/sql.(*Conn).QueryRowContext":      anySinkSQL,
		"database/sql.(*Tx).Exec":                   anySinkSQL,
		"database/sql.(*Tx).ExecContext":            anySinkSQL,
		"database/sql.(*Tx).Query":                  anySinkSQL,
		"database/sql.(*Tx).QueryContext":           anySinkSQL,
		"database/sql.(*Tx).QueryRow":               anySinkSQL,
		"database/sql.(*Tx).QueryRowContext":        anySinkSQL,
		"database/sql.(*Stmt).Exec":                 anySinkSQL,
		"database/sql.(*Stmt).ExecContext":          anySinkSQL,
		"database/sql.(*Stmt).Query":                anySinkSQL,
		"database/sql.(*Stmt).QueryContext":         anySinkSQL,
		"database/sql.(*Stmt).QueryRow":             anySinkSQL,
		"database/sql.(*Stmt).QueryRowContext":      anySinkSQL,
		"database/sql.(*Row).Scan":                  anySinkSQL,
		"database/sql.(*Rows).Scan":                 anySinkSQL,
		"database/sql/driver.Null.ConvertValue":     anySinkSQL,
		"database/sql/driver.NotNull.ConvertValue":  anySinkSQL,
	}

	// anySinkRefusedMethods lists, per sink, the script methods the native would look up on
	// a value it reaches that no adapter can carry to it.
	anySinkRefusedMethods = map[nativeAnySink][]string{
		anySinkJSONDecode:    {"UnmarshalJSON", "UnmarshalText"},
		anySinkJSONv2Encode:  {"MarshalJSONTo", "AppendText"},
		anySinkJSONv2Decode:  {"UnmarshalJSONFrom", "UnmarshalJSON", "UnmarshalText"},
		anySinkSlog:          {"AppendText"},
		anySinkMethodsByName: {anySinkAnyExportedMethod},
		anySinkTemplate:      {"String", "Error", "Format", "GoString"},
		anySinkSQL:           {"Value", "Scan"},
		anySinkUnknownMethodValue: {"String", "Error", "Format", "GoString", "MarshalJSON", "MarshalText", "AppendText", "MarshalJSONTo",
			"UnmarshalJSON", "UnmarshalText", "UnmarshalJSONFrom", "MarshalXML", "UnmarshalXML", "GobEncode", "GobDecode",
			"MarshalBinary", "UnmarshalBinary", "Scan", "Value", "LogValue"},
		anySinkXMLEncode: {"MarshalXML", "MarshalXMLAttr"},
		anySinkXMLDecode: {"UnmarshalXML", "UnmarshalXMLAttr", "UnmarshalText"},
		anySinkGobEncode: {"GobEncode", "MarshalBinary", "MarshalText"},
		anySinkGobDecode: {"GobDecode", "UnmarshalBinary", "UnmarshalText"},
	}
)

// nativeAnySink classifies what a native does with the script values it receives through
// `any` parameters.
//
// The runtime types of script types carry no methods, so the natives below, which look
// methods up on the values they receive, get the value prepared for that lookup instead.
// See docs/compatibility.md for the natives that are not listed.
type nativeAnySink uint8

// methodValueClass is the classification of a native method value. It keeps the receiver
// type and method name rather than the label built from them, so recording a binding
// allocates nothing.
type methodValueClass struct {
	// receiverType is the runtime type of the receiver the method was bound on.
	receiverType reflect.Type

	// methodName is the method's name.
	methodName string

	// sequence orders the record, from methodValueClassSequence.
	sequence uint64

	// sink is the method's classification.
	sink nativeAnySink
}

// anySinkRefusalKey keys the memo of anySinkRefusal walks.
type anySinkRefusalKey struct {
	// typ is the walked type.
	typ reflect.Type

	// sink is the sink whose refused methods the walk looks for.
	sink nativeAnySink

	// skipTop is true when the walk ignores t's own methods.
	skipTop bool
}

// anySinkRefusal is the memoised result of one anySinkRefusal walk.
type anySinkRefusal struct {
	// typeName is the script type that declares the method, or "".
	typeName string

	// method is the refused method's name.
	method string
}

// nativeCalleeName names a native callee in a refusal. It holds what the name is built
// from rather than the name, since a call checks its arguments every time and almost
// never needs it.
type nativeCalleeName struct {
	// receiverType is the runtime type of the receiver a method value was bound on, or nil.
	receiverType reflect.Type

	// methodName is the method a method value was bound for, or "".
	methodName string

	// description names the callee as it is, or "".
	description string
}

// refusalPart is one part of a type a refusal walk follows.
type refusalPart struct {
	// typ is the part's type.
	typ reflect.Type

	// skipTop is true when the part's own methods are ignored.
	skipTop bool
}

// interfaceHeldVisit identifies the storage a pointer, map or slice in a decoder's target
// refers to, so a walk visits shared and cyclic parts once.
type interfaceHeldVisit struct {
	// typ is the part's type.
	typ reflect.Type

	// address is the address of the storage the part refers to.
	address uintptr

	// length is a slice part's length, and zero for any other part.
	length int
}

// interfaceHeldWalk is one walk of a decoder's target for an interface holding a pointer
// to a script type that declares a refused method.
type interfaceHeldWalk struct {
	// vm owns the method tables and the cost budget the walk is charged to.
	vm *VM

	// visited holds the storage already walked, allocated on first use.
	visited map[interfaceHeldVisit]struct{}

	// err is the error that stopped the walk, or nil.
	err error

	// nodes counts the parts visited.
	nodes int

	// sink is the decoder whose refused methods the walk looks for.
	sink nativeAnySink
}

// encodes reports whether the sink is an encoder whose arguments get the marshal shadow.
//
// Returns bool which is true for the json and xml encoders.
func (sink nativeAnySink) encodes() bool {
	return sink == anySinkJSONEncode || sink == anySinkXMLEncode || sink == anySinkJSONv2Encode
}

// encodesJSON reports whether the sink is a JSON encoder, whose marshal shadow leaves are
// script types with MarshalJSON or MarshalText.
//
// Returns bool which is true for the encoding/json and encoding/json/v2 encoders.
func (sink nativeAnySink) encodesJSON() bool {
	return sink == anySinkJSONEncode || sink == anySinkJSONv2Encode
}

// nativeFunctionAnySink classifies a native function by its symbol name.
//
// Takes function (reflect.Value) which is the native function.
//
// Returns nativeAnySink which is anySinkValue for every unlisted native.
func nativeFunctionAnySink(function reflect.Value) nativeAnySink {
	if !function.IsValid() || function.Kind() != reflect.Func || function.IsNil() {
		return anySinkValue
	}
	if function.Pointer() == methodValueCallPC {
		return anySinkValue
	}
	symbol := runtime.FuncForPC(function.Pointer())
	if symbol == nil {
		return anySinkValue
	}
	name := symbol.Name()
	if sink, listed := nativeAnySinksByName[name]; listed {
		return sink
	}
	if !strings.HasPrefix(name, "fmt.") {
		return anySinkValue
	}
	if isFmtScanName(name) {
		return anySinkScan
	}
	if isFmtVariadicAny(function, function.Type()) {
		return anySinkPrint
	}
	return anySinkValue
}

// NativeAnySinkClassified reports whether the engine prepares or refuses the script
// values a native receives through its `any` parameters, rather than passing them as they
// are. Those are the natives that look methods up on the values.
//
// Takes symbol (string) which names the native as runtime.FuncForPC does, or a method on
// a native receiver as "pkg.(*T).Method" or "pkg.T.Method".
//
// Returns bool which is true for a classified native.
func NativeAnySinkClassified(symbol string) bool {
	if _, listed := nativeAnySinksByName[symbol]; listed {
		return true
	}
	return strings.HasPrefix(symbol, "fmt.")
}

// calleeAnySink classifies a native callee. A function is classified by its symbol, and a
// method value, which reports reflect's shared trampoline as its symbol, by the receiver
// and method it was bound for.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns nativeAnySink which is the classification.
// Returns nativeCalleeName which names the callee for errors.
func (vm *VM) calleeAnySink(function reflect.Value) (nativeAnySink, nativeCalleeName) {
	if function.IsValid() && function.Kind() == reflect.Func && !function.IsNil() && function.Pointer() == methodValueCallPC {
		class, ok := vm.methodValueClasses[function]
		if !ok {
			return anySinkUnknownMethodValue, nativeCalleeName{receiverType: nil, methodName: "", description: unknownMethodValueDescription}
		}
		return class.sink, nativeCalleeName{receiverType: class.receiverType, methodName: class.methodName, description: ""}
	}
	return nativeFunctionAnySink(function), nativeCalleeName{receiverType: nil, methodName: "", description: ""}
}

// label builds the name, which is the description, the method on its receiver, or the
// native function's own path.
//
// Takes function (reflect.Value) which is the native callee.
//
// Returns string which is the name.
func (name nativeCalleeName) label(function reflect.Value) string {
	switch {
	case name.description != "":
		return name.description
	case name.methodName != "":
		return nativeMethodLabel(name.receiverType, name.methodName)
	default:
		return resolveNativeFunctionPath(function)
	}
}

// noteMethodValueClass records the classification of a method value bound on a native
// receiver, anySinkValue included, so a later call through it is told apart from one
// through a method value bound outside the VM.
//
// Takes bound (reflect.Value) which is the method value.
// Takes receiverType (reflect.Type) which is the receiver's runtime type.
// Takes methodName (string) which is the method's name.
func (vm *VM) noteMethodValueClass(bound reflect.Value, receiverType reflect.Type, methodName string) {
	if !bound.IsValid() || bound.Kind() != reflect.Func || bound.Pointer() != methodValueCallPC {
		return
	}
	if recorded, ok := vm.methodValueClasses[bound]; ok && recorded.receiverType == receiverType && recorded.methodName == methodName {
		return
	}
	sink := nativeMethodAnySink(receiverType, methodName)
	switch {
	case vm.methodValueClasses == nil:
		vm.methodValueClasses = make(map[reflect.Value]methodValueClass)
	case len(vm.methodValueClasses) >= methodValueSinkRecordMax:
		dropOlderMethodValueClasses(vm.methodValueClasses)
	}
	vm.methodValueClasses[bound] = methodValueClass{
		receiverType: receiverType,
		methodName:   methodName,
		sequence:     methodValueClassSequence.Add(1),
		sink:         sink,
	}
}

// dropOlderMethodValueClasses empties the older half of a full method value record.
//
// The half is counted, not taken from the range of sequence numbers. Every VM draws its
// numbers from one counter, so other VMs leave gaps in a record's sequence, and a cut at
// the middle of the range could drop nearly the whole record.
//
// Takes record (map[reflect.Value]methodValueClass) which is the full record.
func dropOlderMethodValueClasses(record map[reflect.Value]methodValueClass) {
	sequences := make([]uint64, 0, len(record))
	for _, class := range record {
		sequences = append(sequences, class.sequence)
	}
	slices.Sort(sequences)
	middle := sequences[(len(sequences)-1)/2]
	maps.DeleteFunc(record, func(_ reflect.Value, class methodValueClass) bool {
		return class.sequence <= middle
	})
}

// nativeMethodLabel names a method called on a native receiver as Go prints it, such as
// "(*encoding/gob.Encoder).Encode".
//
// Takes receiverType (reflect.Type) which is the receiver's runtime type.
// Takes methodName (string) which is the method's name.
//
// Returns string which is the label.
func nativeMethodLabel(receiverType reflect.Type, methodName string) string {
	if receiverType == nil {
		return methodName
	}
	if receiverType.Kind() == reflect.Pointer {
		return "(*" + receiverType.Elem().PkgPath() + "." + receiverType.Elem().Name() + ")." + methodName
	}
	return receiverType.PkgPath() + "." + receiverType.Name() + "." + methodName
}

// nativeMethodAnySink classifies a method called on a native receiver.
//
// Takes receiverType (reflect.Type) which is the receiver's runtime type.
// Takes methodName (string) which is the method's name.
//
// Returns nativeAnySink which is anySinkValue for every unlisted method.
func nativeMethodAnySink(receiverType reflect.Type, methodName string) nativeAnySink {
	if receiverType == nil {
		return anySinkValue
	}
	var buffer [nativeMethodSymbolBuffer]byte
	symbol := buffer[:0]
	if receiverType.Kind() == reflect.Pointer {
		element := receiverType.Elem()
		symbol = append(symbol, element.PkgPath()...)
		symbol = append(symbol, ".(*"...)
		symbol = append(symbol, element.Name()...)
		symbol = append(symbol, ")."...)
	} else {
		symbol = append(symbol, receiverType.PkgPath()...)
		symbol = append(symbol, '.')
		symbol = append(symbol, receiverType.Name()...)
		symbol = append(symbol, '.')
	}
	symbol = append(symbol, methodName...)
	return nativeAnySinksByName[string(symbol)]
}

// prepareAnySinkArgument prepares an operand bound for an `any` parameter of a native
// classified as sink.
//
// Takes vm (*VM) which owns the method tables.
// Takes sink (nativeAnySink) which classifies the native.
// Takes argument (reflect.Value) which is the coerced operand.
// Takes typeCtx (argumentTypeContext) which carries the call site's static type name.
//
// Returns reflect.Value which is the operand to pass.
func prepareAnySinkArgument(vm *VM, sink nativeAnySink, argument reflect.Value, typeCtx argumentTypeContext) reflect.Value {
	if !argument.IsValid() || typeCtx.skipInterfaceAdapter || sink == anySinkValue {
		return argument
	}
	if argument.Kind() == reflect.Interface {
		if argument.IsNil() {
			return argument
		}
		argument = argument.Elem()
	}
	switch sink {
	case anySinkJSONEncode, anySinkXMLEncode, anySinkJSONv2Encode:
		return shadowForMarshal(vm, argument, sink)
	case anySinkScan:
		return targetAdapter(vm, argument, typeCtx, buildScannerAdapterIfRegistered)
	case anySinkJSONDecode, anySinkJSONv2Decode:
		return targetAdapter(vm, argument, typeCtx, buildUnmarshalerAdapterIfRegistered, buildTextUnmarshalerAdapterIfRegistered)
	case anySinkSlog:
		return slogOperandFor(vm, argument, typeCtx)
	default:
		return argument
	}
}

// targetAdapter returns the first adapter the builders make for argument's script type,
// or argument itself when none applies.
//
// Takes vm (*VM) which owns the method tables.
// Takes argument (reflect.Value) which is the operand.
// Takes typeCtx (argumentTypeContext) which carries the call site's static type name.
// Takes builders (...func(*VM, reflect.Value, string) reflect.Value) which are tried in
// order.
//
// Returns reflect.Value which is the adapter or argument.
func targetAdapter(vm *VM, argument reflect.Value, typeCtx argumentTypeContext, builders ...func(*VM, reflect.Value, string) reflect.Value) reflect.Value {
	typeName := resolveAdapterTypeName(vm, argument, typeCtx)
	if typeName == "" {
		return argument
	}
	for _, build := range builders {
		if adapter := build(vm, argument, typeName); adapter.IsValid() {
			return adapter
		}
	}
	return argument
}

// prepareLooseNativeArguments prepares the arguments of a native called outside the
// ordinary call path (deferred, started as a goroutine) for the native's sink, as
// buildReflectArgs does for an ordinary call.
//
// Takes vm (*VM) which owns the method tables.
// Takes sink (nativeAnySink) which classifies the native.
// Takes functionType (reflect.Type) which is the native's signature.
// Takes arguments ([]reflect.Value) which are the coerced arguments, edited in place.
// Takes spread (bool) which is true when the last argument is the variadic slice.
func prepareLooseNativeArguments(vm *VM, sink nativeAnySink, functionType reflect.Type, arguments []reflect.Value, spread bool) {
	if sink == anySinkValue {
		return
	}
	var verbs []byte
	if sink == anySinkPrint {
		verbs = looseFmtVerbs(functionType, arguments, spread)
	}
	variadicStart := functionType.NumIn() - 1
	for index, argument := range arguments {
		parameterType, ok := NativeParameterType(functionType, index, spread)
		if !ok || !isEmptyInterfaceType(parameterType) {
			continue
		}
		if sink == anySinkPrint {
			arguments[index] = wrapFmtArgumentForVerb(vm, argument, fmtVerbAt(verbs, index-variadicStart), "")
			continue
		}
		arguments[index] = prepareAnySinkArgument(vm, sink, argument, argumentTypeContext{})
	}
}

// looseFmtVerbs returns the verb of each variadic operand of a Printf-style call whose
// arguments are already built, or nil when the call has no format string.
//
// Takes functionType (reflect.Type) which is the native's signature.
// Takes arguments ([]reflect.Value) which are the call's arguments.
// Takes spread (bool) which is true when the variadic operands arrive as one slice.
//
// Returns []byte which holds one verb per variadic operand, or nil.
func looseFmtVerbs(functionType reflect.Type, arguments []reflect.Value, spread bool) []byte {
	if spread || !functionType.IsVariadic() {
		return nil
	}
	formatIndex := functionType.NumIn() - 2
	if formatIndex < 0 || formatIndex >= len(arguments) || functionType.In(formatIndex).Kind() != reflect.String ||
		!arguments[formatIndex].IsValid() || arguments[formatIndex].Kind() != reflect.String {
		return nil
	}
	count := len(arguments) - (functionType.NumIn() - 1)
	if count <= 0 {
		return nil
	}
	return fmtArgumentVerbs(arguments[formatIndex].String(), count)
}

// checkAnySinkArguments refuses a call that would hand an encoder or decoder a script
// value whose type, or a type nested in it, declares a method the native would look up
// and no adapter carries, rather than let the native encode or decode it without the
// method.
//
// Takes vm (*VM) which owns the method tables.
// Takes sink (nativeAnySink) which classifies the native.
// Takes function (reflect.Value) which is the native.
// Takes callee (nativeCalleeName) which names the native in the error.
// Takes arguments ([]reflect.Value) which are the prepared arguments.
// Takes spread (bool) which is true when the last argument is the variadic slice.
//
// Returns error which wraps fault.ErrUnsupportedInterfaceArgument, or the cost or
// cancellation error that stopped the check, or nil.
func checkAnySinkArguments(vm *VM, sink nativeAnySink, function reflect.Value, callee nativeCalleeName, arguments []reflect.Value, spread bool) error {
	if _, refuses := anySinkRefusedMethods[sink]; !refuses || vm == nil || vm.rootFunction == nil {
		return nil
	}
	functionType := function.Type()
	for index, argument := range arguments {
		parameterType, ok := NativeParameterType(functionType, index, spread)
		if !ok || !isEmptyInterfaceType(parameterType) {
			continue
		}
		refusal, err := vm.argumentRefusal(sink, argument)
		if errors.Is(err, errInterfaceHeldWalkTooLarge) {
			return fmt.Errorf("%w: argument %d of %s holds more than %d parts to check for interpreted types",
				fault.ErrUnsupportedInterfaceArgument, index, callee.label(function), interfaceHeldWalkNodeCap)
		}
		if err != nil {
			return err
		}
		if refusal.typeName == "" {
			continue
		}
		return fmt.Errorf("%w: argument %d of %s reaches interpreted type %s, whose %s method the native would not call",
			fault.ErrUnsupportedInterfaceArgument, index, callee.label(function), refusal.typeName, refusal.method)
	}
	return nil
}

// argumentRefusal returns the refusal for one `any` argument, which is a script type in
// it that declares a method sink refuses. The argument's own type is ignored when an
// adapter carries it.
//
// Takes sink (nativeAnySink) which classifies the native.
// Takes argument (reflect.Value) which is the prepared argument.
//
// Returns anySinkRefusal which is empty when nothing is refused.
// Returns error which is errInterfaceHeldWalkTooLarge, or the cost or cancellation error,
// when a decoder's target could not be walked to the end.
func (vm *VM) argumentRefusal(sink nativeAnySink, argument reflect.Value) (anySinkRefusal, error) {
	if argument.Kind() == reflect.Interface && !argument.IsNil() {
		argument = argument.Elem()
	}
	if !argument.IsValid() || argument.Kind() == reflect.Interface {
		return anySinkRefusal{}, nil
	}
	if _, adapted := reflect.TypeAssert[adapterUnwrapper](argument); adapted {
		return vm.anySinkRefusal(unwrapAdapterUnderlying(argument).Type(), sink, true), nil
	}
	if refusal := vm.anySinkRefusal(argument.Type(), sink, false); refusal.typeName != "" {
		return refusal, nil
	}
	if !sink.decodes() {
		return anySinkRefusal{}, nil
	}
	walk := interfaceHeldWalk{vm: vm, visited: nil, err: nil, nodes: 0, sink: sink}
	refusal := walk.refusal(argument, 0)
	return refusal, walk.err
}

// decodes reports whether the sink is a decoder, which decodes into what an interface
// inside its target already points at.
//
// Returns bool which is true for the json, json/v2, xml and gob decoders.
func (sink nativeAnySink) decodes() bool {
	switch sink {
	case anySinkJSONDecode, anySinkJSONv2Decode, anySinkXMLDecode, anySinkGobDecode:
		return true
	default:
		return false
	}
}

// refusal finds, in a decoder's target, an interface holding a pointer to a script type
// that declares one of the sink's refused methods.
//
// Takes value (reflect.Value) which is the part visited.
// Takes depth (int) which bounds the walk.
//
// Returns anySinkRefusal which names the first type and method found, or is empty when
// none is found or the walk stopped with an error in w.err.
func (w *interfaceHeldWalk) refusal(value reflect.Value, depth int) anySinkRefusal {
	if w.err != nil || !value.IsValid() || depth > marshalShadowDepthCap || !typeCanHoldInterface(value.Type()) {
		return anySinkRefusal{}
	}
	if !w.enter(value) {
		return anySinkRefusal{}
	}
	switch value.Kind() {
	case reflect.Interface:
		return w.heldInterfaceRefusal(value, depth)
	case reflect.Pointer:
		if value.IsNil() {
			return anySinkRefusal{}
		}
		return w.refusal(value.Elem(), depth+1)
	case reflect.Struct:
		return w.structRefusal(value, depth)
	case reflect.Slice, reflect.Array:
		return w.elementsRefusal(value, depth)
	case reflect.Map:
		return w.mapRefusal(value, depth)
	default:
		return anySinkRefusal{}
	}
}

// elementsRefusal is refusal for each element of a slice or an array, stopping at the
// first refusal or error.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which is the container's depth.
//
// Returns anySinkRefusal which is the first found, or empty.
func (w *interfaceHeldWalk) elementsRefusal(value reflect.Value, depth int) anySinkRefusal {
	for index := range value.Len() {
		if refusal := w.refusal(value.Index(index), depth+1); refusal.typeName != "" || w.err != nil {
			return refusal
		}
	}
	return anySinkRefusal{}
}

// mapRefusal is refusal for each value of a map, stopping at the first refusal or error.
//
// Takes value (reflect.Value) which is the map.
// Takes depth (int) which is the map's depth.
//
// Returns anySinkRefusal which is the first found, or empty.
func (w *interfaceHeldWalk) mapRefusal(value reflect.Value, depth int) anySinkRefusal {
	for iterator := value.MapRange(); iterator.Next(); {
		if refusal := w.refusal(iterator.Value(), depth+1); refusal.typeName != "" || w.err != nil {
			return refusal
		}
	}
	return anySinkRefusal{}
}

// enter counts a visited part against the walk's budget and records the storage a
// pointer, map or slice refers to, so shared and cyclic data is walked once. Every
// interfaceHeldWalkCheckInterval parts it charges the guest's cost budget and checks for
// cancellation.
//
// Takes value (reflect.Value) which is the part about to be visited.
//
// Returns bool which is false when the part was seen before or the walk must stop, with
// w.err set in the latter case.
func (w *interfaceHeldWalk) enter(value reflect.Value) bool {
	w.nodes++
	if w.nodes > interfaceHeldWalkNodeCap {
		w.err = errInterfaceHeldWalkTooLarge
		return false
	}
	if w.nodes%interfaceHeldWalkCheckInterval == 0 {
		if err := w.vm.chargeHostWalk(interfaceHeldWalkCheckInterval); err != nil {
			w.err = err
			return false
		}
	}
	key := interfaceHeldVisit{typ: value.Type(), address: 0, length: 0}
	switch value.Kind() {
	case reflect.Pointer, reflect.Map:
		if value.IsNil() {
			return true
		}
		key.address = value.Pointer()
	case reflect.Slice:
		if value.IsNil() {
			return true
		}
		key.address = value.Pointer()
		key.length = value.Len()
	default:
		return true
	}
	if w.visited == nil {
		w.visited = make(map[interfaceHeldVisit]struct{})
	}
	if _, seen := w.visited[key]; seen {
		return false
	}
	w.visited[key] = struct{}{}
	return true
}

// heldInterfaceRefusal is refusal for an interface. A pointer it holds is checked for a
// script type declaring one of the refused methods, and what it holds is walked on.
//
// Takes value (reflect.Value) which is the interface.
// Takes depth (int) which bounds the walk.
//
// Returns anySinkRefusal which names the first type and method found, or is empty.
func (w *interfaceHeldWalk) heldInterfaceRefusal(value reflect.Value, depth int) anySinkRefusal {
	if value.IsNil() {
		return anySinkRefusal{}
	}
	held := value.Elem()
	if held.Kind() == reflect.Pointer && !held.IsNil() {
		if refusal := w.vm.anySinkRefusal(held.Type(), w.sink, false); refusal.typeName != "" {
			return refusal
		}
	}
	return w.refusal(held, depth+1)
}

// structRefusal is refusal for a struct. Its exported and renamed embedded fields, the
// parts a decoder reaches, are walked.
//
// Takes value (reflect.Value) which is the struct.
// Takes depth (int) which bounds the walk.
//
// Returns anySinkRefusal which names the first type and method found, or is empty.
func (w *interfaceHeldWalk) structRefusal(value reflect.Value, depth int) anySinkRefusal {
	structType := value.Type()
	for index := range value.NumField() {
		field := structType.Field(index)
		if !field.IsExported() && !isRenamedEmbeddedField(field) {
			continue
		}
		if refusal := w.refusal(value.Field(index), depth+1); refusal.typeName != "" || w.err != nil {
			return refusal
		}
	}
	return anySinkRefusal{}
}

// chargeHostWalk charges work the host does on the guest's behalf to the guest's cost
// budget and checks for cancellation, so a walk over guest data is paid for and can be
// stopped. While the VM unwinds after an abort it charges nothing, since the unwind's
// deferred calls must still run.
//
// Takes units (int64) which is the cost to charge.
//
// Returns error which is the cancellation or cost-budget error, or nil.
func (vm *VM) chargeHostWalk(units int64) error {
	if vm.abortUnwinding {
		return nil
	}
	if vm.Cancelled.Load() != 0 || vm.pollContext() {
		return vm.stopError()
	}
	if vm.CostRemaining < 0 {
		return nil
	}
	vm.CostRemaining -= units
	if vm.CostRemaining > 0 {
		return nil
	}
	if exhausted, err := vm.RefillCostChunk(); exhausted {
		return err
	}
	return nil
}

// typeCanHoldInterface reports whether a value of t can contain an interface, through
// pointers, elements, map values and struct fields.
//
// Takes t (reflect.Type) which is the type.
//
// Returns bool which is true when it can.
func typeCanHoldInterface(t reflect.Type) bool {
	if result, ok := interfaceHoldingTypes.load(t); ok {
		return result
	}
	result := typeCanHoldInterfaceWalk(t, make(map[reflect.Type]bool))
	if interfaceHoldingTypeCount.Load() < interfaceHoldingTypeMax {
		if _, loaded := interfaceHoldingTypes.loadOrStore(t, result); !loaded {
			interfaceHoldingTypeCount.Add(1)
		}
	}
	return result
}

// typeCanHoldInterfaceWalk is the uncached walk of typeCanHoldInterface.
//
// Takes t (reflect.Type) which is the type.
// Takes visiting (map[reflect.Type]bool) which holds the types on the path.
//
// Returns bool which is true when a value of t can contain an interface.
func typeCanHoldInterfaceWalk(t reflect.Type, visiting map[reflect.Type]bool) bool {
	if visiting[t] {
		return false
	}
	visiting[t] = true
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return typeCanHoldInterfaceWalk(t.Elem(), visiting)
	case reflect.Struct:
		for field := range t.Fields() {
			if typeCanHoldInterfaceWalk(field.Type, visiting) {
				return true
			}
		}
	}
	return false
}

// anySinkRefusal finds a script type in t that declares a method sink refuses, memoised
// on the program root.
//
// Takes t (reflect.Type) which is the argument's type.
// Takes sink (nativeAnySink) which classifies the native.
// Takes skipTop (bool) which is true when t itself is carried by an adapter.
//
// Returns anySinkRefusal which names the type and method, or is empty.
func (vm *VM) anySinkRefusal(t reflect.Type, sink nativeAnySink, skipTop bool) anySinkRefusal {
	key := anySinkRefusalKey{typ: t, sink: sink, skipTop: skipTop}
	root := vm.rootFunction
	if cached, ok := root.MarshalShadowMemo.Load(key); ok {
		if refusal, isRefusal := cached.(anySinkRefusal); isRefusal {
			return refusal
		}
	}
	refusal := vm.findAnySinkRefusal(t, anySinkRefusedMethods[sink], skipTop, 0, map[reflect.Type]struct{}{})
	root.StoreMarshalShadow(key, refusal)
	return refusal
}

// findAnySinkRefusal is the uncached walk of anySinkRefusal. It follows pointers,
// elements, map keys and values and exported struct fields, the parts of a value an
// encoder or decoder reaches.
//
// Takes t (reflect.Type) which is the type to inspect.
// Takes methods ([]string) which are the refused method names.
// Takes skipTop (bool) which is true when t's own methods are ignored.
// Takes depth (int) which bounds the walk.
// Takes inProgress (map[reflect.Type]struct{}) which breaks type cycles.
//
// Returns anySinkRefusal which names the first type and method found, or is empty.
func (vm *VM) findAnySinkRefusal(t reflect.Type, methods []string, skipTop bool, depth int, inProgress map[reflect.Type]struct{}) anySinkRefusal {
	if t == nil || depth > marshalShadowDepthCap {
		return anySinkRefusal{}
	}
	if _, cycling := inProgress[t]; cycling {
		return anySinkRefusal{}
	}
	inProgress[t] = struct{}{}
	defer delete(inProgress, t)
	if !skipTop {
		if refusal := vm.declaredRefusal(t, methods); refusal.typeName != "" {
			return refusal
		}
	}
	for _, part := range refusalParts(t, skipTop) {
		if refusal := vm.findAnySinkRefusal(part.typ, methods, part.skipTop, depth+1, inProgress); refusal.typeName != "" {
			return refusal
		}
	}
	return anySinkRefusal{}
}

// declaredRefusal returns the refusal for a script type that declares one of methods.
//
// Takes t (reflect.Type) which is the type.
// Takes methods ([]string) which are the refused method names.
//
// Returns anySinkRefusal which is empty when t is no script type or declares none.
func (vm *VM) declaredRefusal(t reflect.Type, methods []string) anySinkRefusal {
	typeName := marshalLeafTypeName(t)
	if typeName == "" {
		return anySinkRefusal{}
	}
	if slices.Contains(methods, anySinkAnyExportedMethod) {
		pointer := t.Kind() == reflect.Pointer
		if set := pipitMethodSet(vm, typeName, true); len(set) > 0 {
			for _, entry := range set {
				if pointer || !entry.callee.IsPointerReceiver {
					return anySinkRefusal{typeName: typeName, method: entry.name}
				}
			}
			return anySinkRefusal{typeName: typeName, method: set[0].name}
		}
	}
	for _, method := range methods {
		if _, _, ok := lookupAdapterMethod(vm, typeName+"."+method); ok {
			return anySinkRefusal{typeName: typeName, method: method}
		}
	}
	return anySinkRefusal{}
}

// refusalParts returns the parts of t an encoder or decoder reaches.
//
// Takes t (reflect.Type) which is the type.
// Takes skipTop (bool) which is true when an adapter carries t.
//
// Returns []refusalPart which are the parts to follow.
func refusalParts(t reflect.Type, skipTop bool) []refusalPart {
	switch t.Kind() {
	case reflect.Pointer:
		return []refusalPart{{typ: t.Elem(), skipTop: skipTop}}
	case reflect.Slice, reflect.Array:
		return []refusalPart{{typ: t.Elem(), skipTop: false}}
	case reflect.Map:
		return []refusalPart{{typ: t.Key(), skipTop: false}, {typ: t.Elem(), skipTop: false}}
	case reflect.Struct:
		if skipTop {
			return nil
		}
		parts := make([]refusalPart, 0, t.NumField())
		for field := range t.Fields() {
			if field.IsExported() || isRenamedEmbeddedField(field) {
				parts = append(parts, refusalPart{typ: field.Type, skipTop: false})
			}
		}
		return parts
	default:
		return nil
	}
}
