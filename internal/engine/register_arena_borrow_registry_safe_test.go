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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBorrowRegistryClassesEveryMethodValueAsUnknown(t *testing.T) {
	registry := aliasAwareBorrowRegistry()
	builder := &strings.Builder{}
	require.Equal(t, BorrowUnknown, registry.classify(reflect.ValueOf(builder).MethodByName("WriteString")),
		"a method value's method cannot be read without unsafe, so even a listed one revokes")
	require.Equal(t, BorrowBorrows, registry.classify(reflect.ValueOf((*strings.Builder).WriteString)),
		"a method expression is a plain function and keeps its class")
}
