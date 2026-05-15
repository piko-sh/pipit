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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

func TestGenericMethodFailurePolicy(t *testing.T) {
	t.Parallel()

	t.Run("combined type argument arity beyond the key width", func(t *testing.T) {
		t.Parallel()
		source := `
type Wide[A, B, C, D, E any] struct{}

func (w Wide[A, B, C, D, E]) n[F, G, H, I any]() int {
	var a A
	var i I
	_, _ = a, i
	return 1
}

func run() int {
	return Wide[int8, int16, int32, int64, string]{}.n[bool, float32, float64, uint8]()
}
run()`
		_, err := app.NewService().Eval(context.Background(), source)
		require.ErrorIs(t, err, fault.ErrCompileGenericMethodTypeArgLimit,
			"nine type arguments exceed MaxSpecialisationTypeArgs of %d",
			program.MaxSpecialisationTypeArgs)
	})

	t.Run("per callee specialisation cap", func(t *testing.T) {
		t.Parallel()
		var builder strings.Builder
		builder.WriteString("type Holder[A any] struct{}\n\n")
		builder.WriteString("func (h Holder[A]) n[C any]() int { var a A; var c C; _, _ = a, c; return 1 }\n\n")
		builder.WriteString("func run() int {\n\ttotal := 0\n")
		for index := range program.MaxSpecialisationsPerFunction + 2 {
			_, _ = fmt.Fprintf(&builder, "\ttype N%d int\n", index)
			_, _ = fmt.Fprintf(&builder, "\ttotal += Holder[N%d]{}.n[int]()\n", index)
		}
		builder.WriteString("\treturn total\n}\nrun()")

		_, err := app.NewService().Eval(context.Background(), builder.String())
		require.ErrorIs(t, err, fault.ErrCompileGenericMethodSpecialisationLimit,
			"more than MaxSpecialisationsPerFunction (%d) instantiations must be refused, not erased",
			program.MaxSpecialisationsPerFunction)
	})

	t.Run("caller that must itself be specialised", func(t *testing.T) {
		t.Parallel()
		var builder strings.Builder
		builder.WriteString("type Holder[A any] struct{}\n\n")
		builder.WriteString("func (h Holder[A]) n[C any]() int { var a A; var c C; _, _ = a, c; return 1 }\n\n")
		builder.WriteString("func call[A, C any]() int { return Holder[A]{}.n[C]() }\n\n")
		builder.WriteString("func run() int {\n\ttotal := 0\n")
		for index := range program.MaxSpecialisationsPerFunction + 2 {
			_, _ = fmt.Fprintf(&builder, "\ttype M%d int\n", index)
			_, _ = fmt.Fprintf(&builder, "\ttotal += call[M%d, int]()\n", index)
		}
		builder.WriteString("\treturn total\n}\nrun()")

		_, err := app.NewService().Eval(context.Background(), builder.String())
		require.ErrorIs(t, err, fault.ErrCompileGenericCallerRequiresSpecialisation,
			"a generic caller whose erased body holds a generic-method call must not fall back")
	})
}
