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

package app

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

type recordingHook struct {
	denyFn func(call recordedHookCall) error
	calls  []recordedHookCall
	mu     sync.Mutex
}

type recordedHookCall struct {
	Fields     map[string]any
	Method     string
	ModulePath string
}

func (r *recordingHook) record(method, modulePath string, fields map[string]any) error {
	r.mu.Lock()
	call := recordedHookCall{Method: method, ModulePath: modulePath, Fields: fields}
	r.calls = append(r.calls, call)
	deny := r.denyFn
	r.mu.Unlock()
	if deny != nil {
		return deny(call)
	}
	return nil
}

func (r *recordingHook) CheckFunctionCall(_ context.Context, modulePath, fnPath string, args []reflect.Value) error {
	return r.record("CheckFunctionCall", modulePath, map[string]any{
		"fnPath":   fnPath,
		"argCount": len(args),
	})
}

func (r *recordingHook) CheckFileOpen(_ context.Context, modulePath, path string, flag int, mode os.FileMode) error {
	return r.record("CheckFileOpen", modulePath, map[string]any{
		"path": path,
		"flag": flag,
		"mode": mode,
	})
}

func (r *recordingHook) CheckFileWrite(_ context.Context, modulePath, path string) error {
	return r.record("CheckFileWrite", modulePath, map[string]any{"path": path})
}

func (r *recordingHook) CheckExec(_ context.Context, modulePath, name string, argv []string) error {
	return r.record("CheckExec", modulePath, map[string]any{"name": name, "argv": argv})
}

func (r *recordingHook) CheckNetDial(_ context.Context, modulePath, network, address string) error {
	return r.record("CheckNetDial", modulePath, map[string]any{"network": network, "address": address})
}

func (r *recordingHook) CheckNetListen(_ context.Context, modulePath, network, address string) error {
	return r.record("CheckNetListen", modulePath, map[string]any{"network": network, "address": address})
}

func (r *recordingHook) CheckGetenv(_ context.Context, modulePath, name string) error {
	return r.record("CheckGetenv", modulePath, map[string]any{"name": name})
}

func (r *recordingHook) CheckSetenv(_ context.Context, modulePath, name, value string) error {
	return r.record("CheckSetenv", modulePath, map[string]any{"name": name, "value": value})
}

func (r *recordingHook) CheckSubprocess(_ context.Context, modulePath, name string, argv []string) error {
	return r.record("CheckSubprocess", modulePath, map[string]any{"name": name, "argv": argv})
}

func (r *recordingHook) snapshot() []recordedHookCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]recordedHookCall, len(r.calls))
	copy(cp, r.calls)
	return cp
}

func TestCapabilityHookOption(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	service := NewService(WithCapabilityHook(hook))

	require.Same(t, hook, service.CapabilityHook())
	require.Same(t, hook, service.limits.CapabilityHook)
}

func TestCapabilityHookSetter(t *testing.T) {
	t.Parallel()
	service := NewService()
	require.Nil(t, service.CapabilityHook(), "a service has no hook by default")

	hook := &recordingHook{}
	service.SetCapabilityHook(hook)
	require.Same(t, hook, service.CapabilityHook())
	require.Same(t, hook, service.limits.CapabilityHook)

	service.SetCapabilityHook(nil)
	require.Nil(t, service.CapabilityHook(), "setting a nil hook clears it")
}

func TestPermissiveHookAllowsEverything(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	h := policy.PermissiveHook
	type checkFn func() error
	checks := []checkFn{
		func() error { return h.CheckFunctionCall(ctx, "", "fmt.Println", nil) },
		func() error { return h.CheckFileOpen(ctx, "", "/etc/passwd", os.O_RDONLY, 0) },
		func() error { return h.CheckFileWrite(ctx, "", "/tmp/x") },
		func() error { return h.CheckExec(ctx, "", "ls", []string{"ls", "-la"}) },
		func() error { return h.CheckNetDial(ctx, "", "tcp", "1.2.3.4:80") },
		func() error { return h.CheckNetListen(ctx, "", "tcp", ":8080") },
		func() error { return h.CheckGetenv(ctx, "", "PATH") },
		func() error { return h.CheckSetenv(ctx, "", "FOO", "bar") },
		func() error { return h.CheckSubprocess(ctx, "", "sh", []string{"sh", "-c", "echo"}) },
	}
	for i, check := range checks {
		assert.NoError(t, check(), "permissive hook check %d allows the call", i)
	}
}

func TestEffectiveCapabilityHook(t *testing.T) {
	t.Parallel()
	require.Equal(t, policy.PermissiveHook, policy.EffectiveCapabilityHook(nil), "a nil hook falls back to the permissive hook")
	hook := &recordingHook{}
	require.Same(t, hook, policy.EffectiveCapabilityHook(hook))
}

func hookTestSymbols() *symtab.SymbolRegistry {
	return symtab.NewSymbolRegistry(symtab.SymbolExports{
		"hooktest": {
			"Upper":   reflect.ValueOf(func(s string) string { return strings.ToUpper(s) }),
			"Sprintf": reflect.ValueOf(func(format, value string) string { return format + ":" + value }),
		},
	})
}

func TestCapabilityHookFiresOnNativeCall(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	service := NewService(WithCapabilityHook(hook), WithForceGoDispatch())
	service.UseSymbols(hookTestSymbols())

	result, err := service.Eval(t.Context(), `
		import "hooktest"
		hooktest.Upper("hello")
	`)
	require.NoError(t, err)
	require.Equal(t, "HELLO", result)

	calls := hook.snapshot()
	require.NotEmpty(t, calls, "the hook is consulted")
	var foundUpper bool
	for _, call := range calls {
		if call.Method != "CheckFunctionCall" {
			continue
		}
		fnPath, ok := call.Fields["fnPath"].(string)
		if !ok {
			continue
		}

		if fnPath != "" {
			foundUpper = true
		}
	}
	require.True(t, foundUpper, "the hook sees CheckFunctionCall with the resolved path (%d calls: %#v)", len(calls), calls)
}

func TestCapabilityHookDeniesNativeCall(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{
		denyFn: func(call recordedHookCall) error {
			if call.Method == "CheckFunctionCall" {
				return errors.New("call denied by policy")
			}
			return nil
		},
	}
	service := NewService(WithCapabilityHook(hook), WithForceGoDispatch())
	service.UseSymbols(hookTestSymbols())

	_, err := service.Eval(t.Context(), `
		import "hooktest"
		hooktest.Upper("hello")
	`)
	require.Error(t, err, "a denying hook fails the evaluation")
	require.Contains(t, err.Error(), "denied by policy", "the error carries the denial message")
}

func TestTheSealedEnvironmentIsGatedAsTheOsPackage(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	service := NewService(WithSealedEnv(map[string]string{"A": "b"}), WithCapabilityHook(hook))
	service.UseSymbols(newOSSymbols())

	result, err := service.Eval(t.Context(), "import \"os\"\nos.Setenv(\"A\", \"c\")\nos.Getenv(\"A\")")
	require.NoError(t, err)
	require.Equal(t, "c", result)

	methods := make([]string, 0, 2)
	for _, call := range hook.snapshot() {
		if call.Method == "CheckGetenv" || call.Method == "CheckSetenv" {
			methods = append(methods, call.Method)
		}
	}
	require.Equal(t, []string{"CheckSetenv", "CheckGetenv"}, methods,
		"the sealed view's accessors are gated as os.Setenv and os.Getenv")
}
