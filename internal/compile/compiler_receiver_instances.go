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

package compile

import (
	"context"
	"go/types"
	"log/slog"
	"reflect"
	"slices"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"
)

// noteReceiverInstances queues every instantiation of a generic type with methods that t
// mentions, for compileReceiverInstances.
//
// Takes t (types.Type) which is a type the compiler is building a runtime type for, with
// the active substitutions applied.
func (c *Compiler) noteReceiverInstances(t types.Type) {
	if t == nil {
		return
	}
	if _, walked := c.walkedInstanceTypes[t]; walked {
		return
	}
	if c.walkedInstanceTypes == nil {
		c.walkedInstanceTypes = make(map[types.Type]struct{})
	}
	pending := []types.Type{t}
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if next == nil {
			continue
		}
		if _, walked := c.walkedInstanceTypes[next]; walked {
			continue
		}
		c.walkedInstanceTypes[next] = struct{}{}
		start := len(pending)
		pending = c.appendInstanceTypeParts(pending, next)
		slices.Reverse(pending[start:])
	}
}

// appendInstanceTypeParts queues t if it is an instantiated generic type with methods,
// and appends the types it is built from to pending.
//
// Takes pending ([]types.Type) which is the work list.
// Takes t (types.Type) which is the type being walked.
//
// Returns []types.Type which is the work list with t's parts appended.
func (c *Compiler) appendInstanceTypeParts(pending []types.Type, t types.Type) []types.Type {
	switch typed := t.(type) {
	case *types.Alias:
		return append(pending, types.Unalias(typed))
	case *types.Named:
		if typed.TypeArgs().Len() > 0 && typed.Origin().NumMethods() > 0 {
			c.pendingReceiverInstances = append(c.pendingReceiverInstances, typed)
		}
		pending = slices.AppendSeq(pending, typed.TypeArgs().Types())
		return append(pending, typed.Underlying())
	case *types.Pointer:
		return append(pending, typed.Elem())
	case *types.Slice:
		return append(pending, typed.Elem())
	case *types.Array:
		return append(pending, typed.Elem())
	case *types.Chan:
		return append(pending, typed.Elem())
	case *types.Map:
		return append(pending, typed.Key(), typed.Elem())
	case *types.Struct:
		for field := range typed.Fields() {
			pending = append(pending, field.Type())
		}
		return pending
	case *types.Signature:
		return append(pending, typed.Params(), typed.Results())
	case *types.Tuple:
		for variable := range typed.Variables() {
			pending = append(pending, variable.Type())
		}
		return pending
	default:
		return pending
	}
}

// compileReceiverInstances enters each queued instantiation's methods in the root's
// receiver method table, compiling a body for the instantiation's type arguments where
// they are concrete.
func (c *Compiler) compileReceiverInstances(ctx context.Context) {
	ctx, _ = logging.From(ctx)
	for len(c.pendingReceiverInstances) > 0 {
		named := c.pendingReceiverInstances[0]
		c.pendingReceiverInstances = c.pendingReceiverInstances[1:]
		c.enterReceiverInstance(ctx, named)
	}
}

// enterReceiverInstance enters the methods of one instantiation in the receiver method
// table.
//
// An instantiation whose type arguments still mention type parameters is the type of
// values built inside shared bodies, and its methods map to the shared bodies. Otherwise
// each method maps to the body compiled for the type arguments. A method no body could be
// compiled for gets no entry, so a call that reaches it fails rather than running the
// shared body.
//
// Takes named (*types.Named) which is the instantiation.
func (c *Compiler) enterReceiverInstance(ctx context.Context, named *types.Named) {
	ctx, logger := logging.From(ctx)
	receiverType := c.TypeToReflect(ctx, named)
	if receiverType == nil {
		return
	}
	if _, entered := c.receiverInstanceTypes[receiverType]; entered {
		return
	}
	if c.receiverInstanceTypes == nil {
		c.receiverInstanceTypes = make(map[reflect.Type]struct{})
	}
	c.receiverInstanceTypes[receiverType] = struct{}{}
	c.nameReceiverInstance(named, receiverType)

	typeArgs := slices.Collect(named.TypeArgs().Types())
	shared := slices.ContainsFunc(typeArgs, typemap.ContainsTypeParameter)
	for method := range named.Origin().Methods() {
		functionIndex, callee, ok := c.declaredGenericTypeMethod(method)
		if !ok {
			continue
		}
		target := functionIndex
		if !shared {
			specialised, compiled, err := c.specialiseForReceiverArgs(ctx, callee, typeArgs)
			if err != nil {
				logger.Warn("no body compiled for a generic type's method, so a call to it through an interface will fail",
					slog.String("method", callee.Name),
					slog.String("receiver", receiverType.String()),
					logging.ErrAttr(err))
				continue
			}
			if !compiled {
				continue
			}
			target = specialised
		}
		program.RegisterReceiverMethod(c.RootFunction, receiverType, functionIndex, target)
	}
}

// declaredGenericTypeMethod finds the shared body this compile registered for method, a
// method of a generic type.
//
// Takes method (*types.Func) which is a method of the generic type's origin.
//
// Returns uint16 which is the shared body's function index.
// Returns *program.CompiledFunction which is the shared body.
// Returns bool which is false when the method was declared in another compile, or
// declares type parameters of its own.
func (c *Compiler) declaredGenericTypeMethod(method *types.Func) (uint16, *program.CompiledFunction, bool) {
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return 0, nil, false
	}
	receiverType := signature.Recv().Type()
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	receiverNamed, ok := types.Unalias(receiverType).(*types.Named)
	if !ok {
		return 0, nil, false
	}
	functionIndex, ok := c.functionTable[receiverNamed.Obj().Name()+"."+method.Name()]
	if !ok {
		return 0, nil, false
	}
	callee, ok := c.genericTypeMethodCandidate(functionIndex)
	if !ok || c.Info.Defs[callee.GenericDeclaration.Name] != method {
		return 0, nil, false
	}
	return functionIndex, callee, true
}

// nameReceiverInstance records an instantiation's runtime type under its generic type's
// name, as registerMethodReceiver does for the type's declared form, so code that finds a
// type's methods by name (errors.As, the fmt and json adapters) knows the type.
//
// Takes named (*types.Named) which is the instantiation.
// Takes receiverType (reflect.Type) which is its runtime type.
func (c *Compiler) nameReceiverInstance(named *types.Named, receiverType reflect.Type) {
	root := c.RootFunction
	if root.TypeNames == nil {
		root.TypeNames = make(map[reflect.Type]string)
	}
	if _, exists := root.TypeNames[receiverType]; !exists {
		root.TypeNames[receiverType] = named.Obj().Name()
	}
}
