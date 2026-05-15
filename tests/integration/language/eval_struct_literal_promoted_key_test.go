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

//go:build integration

package language_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func TestStructLiteralPromotedKeyRejectsInvalidForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		code   string
		expect string
	}{
		{
			name: "promoted key overlaps an explicit embedded key",
			code: `
type Object struct{ name, color string }
type Line struct {
	Object
	length int
}
func run() string {
	object := Object{name: "a", color: "b"}
	l := Line{Object: object, name: "diagonal"}
	return l.name
}
run()`,
			expect: "cannot specify promoted field name and enclosing embedded field Object",
		},
		{
			name: "promoted key traverses an embedded pointer",
			code: `
type Object struct{ name, color string }
type Line struct {
	*Object
	length int
}
func run() string {
	l := Line{name: "diagonal"}
	return l.name
}
run()`,
			expect: "invalid implicit pointer indirection to reach name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			_, err := service.Eval(context.Background(), test.code)
			require.Error(t, err, "expected the type checker to reject this literal")
			require.ErrorContains(t, err, test.expect)
		})
	}
}
