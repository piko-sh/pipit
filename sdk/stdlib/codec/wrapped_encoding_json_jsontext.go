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

package codec

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
)

// jsontextSourceBytes narrows an interpreted argument to the ~[]byte | ~string constraint
// jsontext's Append helpers declare.
//
// The constraint is a union with approximation, so a named type whose underlying type is
// []byte or string satisfies it too; those arrive as the named type and are converted
// here rather than rejected.
//
// Takes function (string) which names the caller, for the panic message.
// Takes source (any) which is the argument to narrow.
//
// Returns the source as a []byte.
//
// Panics when source is neither a byte slice nor a string, mirroring the compile error
// real Go would have produced.
func jsontextSourceBytes(function, source any) []byte {
	switch typed := source.(type) {
	case []byte:
		return typed
	case string:
		return []byte(typed)
	}
	value := reflect.ValueOf(source)
	if value.IsValid() {
		switch value.Kind() {
		case reflect.String:
			return []byte(value.String())
		case reflect.Slice:
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return value.Bytes()
			}
		default:
		}
	}
	panic(fmt.Sprintf("encoding/json/jsontext.%v: argument of type %T does not satisfy ~[]byte | ~string",
		function, source))
}

// wrappedJsontextAppendQuote bridges the generic jsontext.AppendQuote for interpreted
// code.
//
// Takes destination ([]byte) which is the buffer the quoted form is appended to.
// Takes source (any) which is the value to quote.
//
// Returns the extended buffer and any error jsontext reported.
func wrappedJsontextAppendQuote(destination []byte, source any) ([]byte, error) {
	return jsontext.AppendQuote(destination, jsontextSourceBytes("AppendQuote", source))
}

// wrappedJsontextAppendUnquote bridges the generic jsontext.AppendUnquote for interpreted
// code.
//
// Takes destination ([]byte) which is the buffer the unquoted form is appended to.
// Takes source (any) which is the quoted value to unquote.
//
// Returns the extended buffer and any error jsontext reported.
func wrappedJsontextAppendUnquote(destination []byte, source any) ([]byte, error) {
	return jsontext.AppendUnquote(destination, jsontextSourceBytes("AppendUnquote", source))
}

// wrappedJsontextAppendFormat bridges the generic jsontext.AppendFormat for interpreted
// code.
//
// Takes destination ([]byte) which is the buffer the formatted JSON is appended to.
// Takes source (any) which is the JSON value to reformat.
// Takes options (...jsontext.Options) which are the formatting options.
//
// Returns the extended buffer and any error jsontext reported.
func wrappedJsontextAppendFormat(destination []byte, source any, options ...jsontext.Options) ([]byte, error) {
	return jsontext.AppendFormat(destination, jsontextSourceBytes("AppendFormat", source), options...)
}

func init() {
	const packagePath = "encoding/json/jsontext"
	registered, ok := Symbols[packagePath]
	if !ok {
		return
	}
	registered["AppendQuote"] = reflect.ValueOf(wrappedJsontextAppendQuote)
	registered["AppendUnquote"] = reflect.ValueOf(wrappedJsontextAppendUnquote)
	registered["AppendFormat"] = reflect.ValueOf(wrappedJsontextAppendFormat)
}
