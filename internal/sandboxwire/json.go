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

package sandboxwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// jsonScan counts tokens and nesting while rejecting duplicate object members.
type jsonScan struct {
	// decoder reads tokens from the bounded JSON input.
	decoder *json.Decoder

	// remaining tracks the token budget still available.
	remaining int

	// maxDepth is the maximum permitted container nesting.
	maxDepth int
}

// token reads one token only while the traversal budget permits it.
//
// Returns json.Token which is the next token.
// Returns error when the JSON is malformed or its token budget is exhausted.
func (scan *jsonScan) token() (json.Token, error) {
	if scan.remaining <= 0 {
		return nil, fmt.Errorf("%w: JSON token limit", ErrProtocol)
	}
	scan.remaining--
	token, err := scan.decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: JSON token: %w", ErrProtocol, err)
	}
	return token, nil
}

// value traverses a primitive value or recursively validates a bounded container.
//
// Takes depth (int) which is the nesting depth of the next value.
//
// Returns error when parsing, nesting or token validation fails.
func (scan *jsonScan) value(depth int) error {
	token, err := scan.token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if depth > scan.maxDepth {
		return fmt.Errorf("%w: JSON depth limit", ErrProtocol)
	}
	switch delimiter {
	case '{':
		return scan.object(depth)
	case '[':
		return scan.array(depth)
	default:
		return fmt.Errorf("%w: unexpected JSON delimiter", ErrProtocol)
	}
}

// object validates unique decoded keys and bounded values in an opened object.
//
// Takes depth (int) which is the current container depth.
//
// Returns error when a key repeats or any child fails validation.
func (scan *jsonScan) object(depth int) error {
	keys := make(map[string]struct{})
	for scan.decoder.More() {
		token, err := scan.token()
		if err != nil {
			return err
		}
		key, valid := token.(string)
		if !valid {
			return fmt.Errorf("%w: non-string JSON key", ErrProtocol)
		}
		if _, exists := keys[key]; exists {
			return fmt.Errorf("%w: duplicate JSON key", ErrProtocol)
		}
		keys[key] = struct{}{}
		if err := scan.value(depth + 1); err != nil {
			return err
		}
	}
	return scan.end('}')
}

// array validates every value in an opened array.
//
// Takes depth (int) which is the current container depth.
//
// Returns error when any child or the closing delimiter is invalid.
func (scan *jsonScan) array(depth int) error {
	for scan.decoder.More() {
		if err := scan.value(depth + 1); err != nil {
			return err
		}
	}
	return scan.end(']')
}

// end consumes the expected closing container delimiter.
//
// Takes expected (json.Delim) which is the required delimiter.
//
// Returns error when the delimiter is absent or the token budget is exhausted.
func (scan *jsonScan) end(expected json.Delim) error {
	token, err := scan.token()
	if err != nil {
		return err
	}
	if token != expected {
		return fmt.Errorf("%w: invalid closing JSON delimiter", ErrProtocol)
	}
	return nil
}

// validateJSON validates one bounded object without trusting encoding/json's
// duplicate-key acceptance or its case-insensitive struct field matching.
//
// Takes data ([]byte) which contains a complete encoded object.
// Takes limits (Limits) which have already been normalised.
//
// Returns error when size, syntax, token count, depth or uniqueness checks fail.
func validateJSON(data []byte, limits Limits) error {
	if len(data) == 0 || len(data) > limits.MaxFrameBytes || !utf8.Valid(data) {
		return fmt.Errorf("%w: invalid JSON size or UTF-8", ErrProtocol)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	scan := jsonScan{decoder: decoder, remaining: limits.MaxJSONValues, maxDepth: limits.MaxJSONDepth}
	opening, err := scan.token()
	if err != nil {
		return err
	}
	if opening != json.Delim('{') {
		return fmt.Errorf("%w: expected JSON object", ErrProtocol)
	}
	if err := scan.object(1); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON data", ErrProtocol)
	}
	return nil
}

// decodeStrict checks exact field names before decoding a host-selected schema.
//
// Takes data ([]byte) which must contain one bounded JSON object.
// Takes destination (any) which must be a non-nil pointer to a struct.
//
// Returns error when the payload, schema or decoded fields are invalid.
func decodeStrict(data []byte, destination any) error {
	limits := Limits{MaxFrameBytes: maximumFrameBytes, MaxJSONDepth: maximumJSONDepth, MaxJSONValues: maximumJSONValues}
	if err := validateJSON(data, limits); err != nil {
		return err
	}
	target := reflect.ValueOf(destination)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() || target.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("%w: payload destination must point to a struct", ErrProtocol)
	}
	if err := checkFieldNames(data, target.Elem().Type()); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: payload schema: %w", ErrProtocol, err)
	}
	return nil
}

// checkFieldNames rejects unknown or case-aliased keys throughout a typed schema. Payload
// schemas use explicit fields rather than anonymous embedded structs.
//
// Takes data ([]byte) which is a previously validated JSON value.
// Takes target (reflect.Type) which is the host-selected decoding type.
//
// Returns error when an object includes an unknown or ambiguously embedded field.
func checkFieldNames(data []byte, target reflect.Type) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if bytes.Equal(data, []byte("null")) || target == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	if target.Kind() == reflect.Slice && target.Elem().Kind() == reflect.Uint8 {
		return nil
	}
	switch target.Kind() {
	case reflect.Struct:
		return checkObjectFields(data, target)
	case reflect.Array, reflect.Slice:
		return checkArrayFields(data, target.Elem())
	case reflect.Map:
		return checkMapFields(data, target.Elem())
	}
	return nil
}

// checkObjectFields validates exact JSON keys against explicit exported fields.
//
// Takes data ([]byte) which is the encoded object.
// Takes target (reflect.Type) which is a struct type.
//
// Returns error when the object contains unknown or unsupported schema fields.
func checkObjectFields(data []byte, target reflect.Type) error {
	fields, err := schemaFields(target)
	if err != nil {
		return err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return fmt.Errorf("%w: object schema: %w", ErrProtocol, err)
	}
	for name, value := range members {
		field, exists := fields[name]
		if !exists {
			return fmt.Errorf("%w: unknown JSON field %q", ErrProtocol, name)
		}
		if err := checkFieldNames(value, field); err != nil {
			return err
		}
	}
	return nil
}

// schemaFields indexes unambiguous, explicit fields of a host-selected payload.
//
// Takes target (reflect.Type) which must be a struct.
//
// Returns map[string]reflect.Type which maps exact JSON keys to field types.
// Returns error when the schema has embedded fields or duplicated JSON names.
func schemaFields(target reflect.Type) (map[string]reflect.Type, error) {
	fields := make(map[string]reflect.Type)
	for field := range target.Fields() {
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if field.Anonymous {
			return nil, fmt.Errorf("%w: embedded payload schema field", ErrProtocol)
		}
		if name == "" {
			name = field.Name
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate payload schema field", ErrProtocol)
		}
		fields[name] = field.Type
	}
	return fields, nil
}

// checkArrayFields validates typed children of an array or slice.
//
// Takes data ([]byte) which is the encoded array.
// Takes element (reflect.Type) which is the destination element type.
//
// Returns error when a child violates its host-selected schema.
func checkArrayFields(data []byte, element reflect.Type) error {
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("%w: array schema: %w", ErrProtocol, err)
	}
	for _, value := range values {
		if err := checkFieldNames(value, element); err != nil {
			return err
		}
	}
	return nil
}

// checkMapFields validates typed map values without restricting their data keys.
//
// Takes data ([]byte) which is the encoded map.
// Takes element (reflect.Type) which is the destination element type.
//
// Returns error when a child violates its host-selected schema.
func checkMapFields(data []byte, element reflect.Type) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("%w: map schema: %w", ErrProtocol, err)
	}
	for _, value := range values {
		if err := checkFieldNames(value, element); err != nil {
			return err
		}
	}
	return nil
}
