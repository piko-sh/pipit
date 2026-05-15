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

package policy

import (
	"context"
	"reflect"
)

var (
	// errorType is the reflect.Type of the error interface, used to detect functions that
	// surface failures through an error return.
	errorType = reflect.TypeFor[error]()
)

// CapabilityGate is the per-function policy that translates raw reflect arguments into a
// hook consultation. A non-nil return aborts the wrapped call.
type CapabilityGate func(ctx context.Context, hook CapabilityHook, modulePath string, args []reflect.Value) error

// StaticHookProvider implements hookProvider with constant values. Used in tests and for
// stdlib bindings that are wrapped once at Service construction time and don't change.
type StaticHookProvider struct {
	// hook holds the constant capability hook returned by Hook.
	hook CapabilityHook

	// ctx holds the constant execution context returned by Context.
	ctx context.Context

	// modulePath holds the constant module path returned by ModulePath.
	modulePath string
}

// NewStaticHookProvider builds a provider that returns the same values on every call.
//
// Takes hook (CapabilityHook) which every Hook call returns; nil is allowed.
// Takes modulePath (string) which every ModulePath call returns.
//
// Returns *StaticHookProvider which is ready to use.
func NewStaticHookProvider(hook CapabilityHook, modulePath string) *StaticHookProvider {
	return &StaticHookProvider{hook: hook, modulePath: modulePath, ctx: nil}
}

// WithContext returns a copy of the provider whose Context call returns ctx.
//
// Returns *StaticHookProvider which is a new provider; the receiver is unchanged.
func (s *StaticHookProvider) WithContext(ctx context.Context) *StaticHookProvider {
	clone := *s
	clone.ctx = ctx
	return &clone
}

// Hook returns the stored hook.
//
// Returns CapabilityHook which is the stored hook.
func (s *StaticHookProvider) Hook() CapabilityHook { return s.hook }

// ModulePath returns the stored module path.
//
// Returns string which is the stored module path.
func (s *StaticHookProvider) ModulePath() string { return s.modulePath }

// Context returns the stored context.
//
// Returns context.Context which is the stored context.
func (s *StaticHookProvider) Context() context.Context { return s.ctx }

// hookProvider is the minimal interface a gated-function wrapper needs from the live VM
// at call time. A nil provider means "no gating".
type hookProvider interface {
	// Hook returns the live capability hook.
	//
	// Returns CapabilityHook which is the hook configured on the provider, or nil when no
	// hook is set.
	Hook() CapabilityHook

	// ModulePath returns the active module path for scoping decisions.
	//
	// Returns string which is the canonical module path, or empty for main-program code.
	ModulePath() string

	// Context returns the live execution context.
	//
	// Returns context.Context which is the active context, never nil.
	Context() context.Context
}

// WrapNativeFunction wraps a stdlib function value with a CapabilityGate. On denial the
// wrapper returns the error through the last error result when the signature has one, or
// panics otherwise.
//
// Takes provider (hookProvider) which supplies the live hook and module path at call
// time, or nil to skip gating.
// Takes fn (reflect.Value) which is the stdlib function to wrap.
// Takes fnPath (string) which is the dotted identifier for CheckFunctionCall and error
// messages.
// Takes gate (CapabilityGate) which is the per-function policy, or nil for a generic
// CheckFunctionCall gate.
//
// Returns reflect.Value which is a function of the same type as fn.
func WrapNativeFunction(provider hookProvider, fn reflect.Value, fnPath string, gate CapabilityGate) reflect.Value {
	if !fn.IsValid() || fn.Kind() != reflect.Func {
		return fn
	}
	fnType := fn.Type()
	hasErrorReturn := fnType.NumOut() > 0 && fnType.Out(fnType.NumOut()-1) == errorType
	return reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		hook, modulePath, ctx := capabilityCallContext(provider)
		var gateErr error
		if hook != nil {
			if gate != nil {
				gateErr = gate(ctx, hook, modulePath, args)
			} else {
				gateErr = hook.CheckFunctionCall(ctx, modulePath, fnPath, args)
			}
		}
		if gateErr != nil {
			return handleGateDenial(fnType, hasErrorReturn, gateErr)
		}
		return fn.Call(args)
	})
}

// capabilityCallContext extracts the (hook, ModulePath, ctx) tuple from a provider, with
// nil-safe defaults.
//
// Takes provider (hookProvider) which may be nil.
//
// Returns CapabilityHook which is the live hook, nil when provider is nil or hook is
// unset.
// Returns string which is the module path.
// Returns context.Context which is the active context.
func capabilityCallContext(provider hookProvider) (CapabilityHook, string, context.Context) {
	if provider == nil {
		return nil, "", context.Background()
	}
	hook := provider.Hook()
	ctx := provider.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return hook, provider.ModulePath(), ctx
}

// handleGateDenial builds the return slice for a denied call, either a slice ending with
// the denial error or a panic.
//
// Takes fnType (reflect.Type) which describes the function signature.
// Takes hasErrorReturn (bool) which is true when the last return type is error.
// Takes denial (error) which is the gate's rejection.
//
// Returns []reflect.Value matching fnType's return list.
//
// Panics with denial when hasErrorReturn is false. The wrapper relies on reflect's normal
// panic semantics. Interpreted code that wraps the call can recover.
func handleGateDenial(fnType reflect.Type, hasErrorReturn bool, denial error) []reflect.Value {
	if !hasErrorReturn {
		panic(denial)
	}
	results := make([]reflect.Value, fnType.NumOut())
	for i := range fnType.NumOut() - 1 {
		results[i] = reflect.Zero(fnType.Out(i))
	}
	results[fnType.NumOut()-1] = reflect.ValueOf(&denial).Elem()
	return results
}
