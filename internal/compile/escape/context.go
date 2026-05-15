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

// Package escape decides which values a function body lets outlive their frame.
//
// The passes here answer three related questions: which locals are captured or
// address-taken and so must be heap-promoted, which typed-slice locals stay on their
// typed register bank, and which function parameters escape through a call or a closure.
// Their answers drive register allocation and arena placement, so a wrong "no" here is a
// use-after-free rather than a slow program.
//
// Every analysis reads the compilation through Context and writes nothing back. That is
// what lets an analysis be driven from a test with a hand-built Context, instead of only
// through a whole compiler.
package escape

import (
	"go/types"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// Context is the read-only view of an in-progress compilation that the escape and
// typed-slice analyses need.
type Context struct {
	// Info carries the go/types results for the package being analysed.
	Info *types.Info

	// Function is the function whose body is under analysis.
	Function *program.CompiledFunction

	// RootFunction owns the function table callees are resolved through.
	RootFunction *program.CompiledFunction

	// FunctionTable maps a function name to its index in RootFunction's table.
	FunctionTable map[string]uint16

	// Substitutions maps type parameters to their concrete instantiation, empty outside a
	// specialised body.
	Substitutions map[*types.TypeParam]types.Type

	// SubstitutionCache memoises Substitutions lookups for the lifetime of one
	// specialisation. A nil cache is valid and skips memoisation.
	SubstitutionCache map[types.Type]types.Type
}

// kindFor reports which register bank holds a value of type t.
//
// Any active generic substitution is applied first, so a type parameter resolves to the
// bank of the type it was instantiated with rather than to the general bank.
//
// Takes t (types.Type) which is the type to classify.
//
// Returns isa.RegisterKind naming the bank.
func (x *Context) kindFor(t types.Type) isa.RegisterKind {
	if x == nil {
		return typemap.KindForType(t)
	}

	return typemap.KindForType(typemap.SubstituteType(t, x.Substitutions, x.SubstitutionCache))
}
