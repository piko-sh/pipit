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

package stdlib

import (
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

var valueOperandNatives = map[string]string{
	"cmp":                                 "orders built-in ordered values",
	"container/heap":                      "stores the value; heap.Interface is the script's own",
	"container/list":                      "stores the value",
	"context":                             "stores the value and compares keys with ==",
	"crypto":                              "asserts key types, failing with an error for a script type",
	"crypto/ecdh":                         "compares keys by asserting their type",
	"crypto/ecdsa":                        "compares keys by asserting their type",
	"crypto/ed25519":                      "compares keys by asserting their type",
	"crypto/mldsa":                        "compares keys by asserting their type",
	"crypto/rsa":                          "compares keys by asserting their type",
	"crypto/x509":                         "asserts key types, failing with an error for a script type",
	"encoding/asn1":                       "walks the value through reflect without looking methods up",
	"encoding/binary":                     "walks the value through reflect without looking methods up",
	"go/ast":                              "prints AST nodes through reflect",
	"go/constant":                         "switches on built-in types",
	"go/doc":                              "switches on option types",
	"go/format":                           "switches on AST node types",
	"go/parser":                           "switches on source types, failing with an error for others",
	"go/printer":                          "switches on AST node types",
	"hash/maphash":                        "hashes the value's memory, as compiled Go does",
	"iter":                                "calls the iterator function it is given",
	"maps":                                "copies and compares values",
	"reflect":                             "the interpreter answers reflect's method queries itself",
	"runtime":                             "keeps or pins the value; SetFinalizer is an intrinsic",
	"runtime/pprof":                       "records the value as a key",
	"slices":                              "copies and compares values",
	"sort":                                "swaps elements through reflect",
	"sync":                                "stores the value and compares keys with ==",
	"sync/atomic":                         "stores the value",
	"testing/quick":                       "calls the function it is given",
	"unique":                              "canonicalises the value by ==",
	"weak":                                "points at the value",
	"database/sql/driver":                 "checks driver value types; ConvertValue is classified",
	"encoding/json/jsontext":              "appends string or byte values",
	"errors.As":                           "an intrinsic that consults the program's method tables",
	"encoding/gob.Register":               "records the value's type for interface encoding",
	"encoding/gob.RegisterName":           "records the value's type for interface encoding",
	"encoding/xml.CopyToken":              "copies an xml.Token",
	"encoding/xml.(*Encoder).EncodeToken": "writes an xml.Token",
	"html/template.IsTrue":                "tests the value's truth through reflect",
	"text/template.IsTrue":                "tests the value's truth through reflect",
	"database/sql.(*NullBool).Scan":       "receives a driver value",
	"database/sql.(*NullByte).Scan":       "receives a driver value",
	"database/sql.(*NullFloat64).Scan":    "receives a driver value",
	"database/sql.(*NullInt16).Scan":      "receives a driver value",
	"database/sql.(*NullInt32).Scan":      "receives a driver value",
	"database/sql.(*NullInt64).Scan":      "receives a driver value",
	"database/sql.(*NullString).Scan":     "receives a driver value",
	"database/sql.(*NullTime).Scan":       "receives a driver value",
	"testing.(*F).Add":                    "records seed values for the fuzz function",
	"testing.(*F).Fuzz":                   "calls the fuzz function it is given",
}

func TestEveryNativeWithAnAnyParameterIsClassified(t *testing.T) {
	t.Parallel()

	var unclassified []string
	for packagePath, symbols := range Exports() {
		for name, value := range symbols {
			for _, symbol := range anyParameterSymbols(packagePath, name, value) {
				if anyOperandsAccountedFor(packagePath, symbol) {
					continue
				}
				unclassified = append(unclassified, symbol)
			}
		}
	}
	slices.Sort(unclassified)
	require.Empty(t, slices.Compact(unclassified),
		"a native that receives a script value through an `any` parameter must either use it as a value "+
			"(valueOperandNatives) or be classified by the engine, so no script method it would look up is skipped")
}

func anyParameterSymbols(packagePath, name string, value reflect.Value) []string {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Func {
		if !takesAnyParameter(value.Type(), 0) {
			return nil
		}
		if symbol := runtime.FuncForPC(value.Pointer()); symbol != nil && strings.HasPrefix(symbol.Name(), packagePath+".") {
			return []string{symbol.Name()}
		}
		return []string{packagePath + "." + name}
	}
	if value.Kind() != reflect.Pointer || !value.IsNil() {
		return nil
	}
	var symbols []string
	if value.Type().Elem().Kind() == reflect.Interface {
		return nil
	}
	for _, receiver := range []reflect.Type{value.Type(), value.Type().Elem()} {
		for method := range receiver.Methods() {
			if takesAnyParameter(method.Type, 1) {
				symbols = append(symbols, packagePath+".(*"+name+")."+method.Name)
			}
		}
	}
	return symbols
}

func anyOperandsAccountedFor(packagePath, symbol string) bool {
	valueForm := strings.Replace(symbol, ".(*", ".", 1)
	valueForm = strings.Replace(valueForm, ").", ".", 1)
	if engine.NativeAnySinkClassified(symbol) || engine.NativeAnySinkClassified(valueForm) {
		return true
	}
	if _, listed := valueOperandNatives[symbol]; listed {
		return true
	}
	if _, listed := valueOperandNatives[valueForm]; listed {
		return true
	}
	_, listed := valueOperandNatives[packagePath]
	return listed
}

func takesAnyParameter(signature reflect.Type, skip int) bool {
	for index := skip; index < signature.NumIn(); index++ {
		parameter := signature.In(index)
		if signature.IsVariadic() && index == signature.NumIn()-1 {
			parameter = parameter.Elem()
		}
		if parameter.Kind() == reflect.Interface && parameter.NumMethod() == 0 {
			return true
		}
	}
	return false
}
