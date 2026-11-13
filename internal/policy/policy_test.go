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

package policy_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/policy"
)

var errDenied = errors.New("denied by test hook")

type recordedCall struct {
	ctx        context.Context
	modulePath string
	fnPath     string
	argCount   int
}

type fakeHook struct {
	deny  map[string]error
	calls []recordedCall
}

func (h *fakeHook) CheckFunctionCall(ctx context.Context, modulePath, fnPath string, args []reflect.Value) error {
	h.calls = append(h.calls, recordedCall{ctx: ctx, modulePath: modulePath, fnPath: fnPath, argCount: len(args)})
	return h.deny[fnPath]
}

func (*fakeHook) CheckFileOpen(context.Context, string, string, int, os.FileMode) error { return nil }
func (*fakeHook) CheckFileWrite(context.Context, string, string) error                  { return nil }
func (*fakeHook) CheckExec(context.Context, string, string, []string) error             { return nil }
func (*fakeHook) CheckNetDial(context.Context, string, string, string) error            { return nil }
func (*fakeHook) CheckNetListen(context.Context, string, string, string) error          { return nil }
func (*fakeHook) CheckGetenv(context.Context, string, string) error                     { return nil }
func (*fakeHook) CheckSetenv(context.Context, string, string, string) error             { return nil }
func (*fakeHook) CheckSubprocess(context.Context, string, string, []string) error       { return nil }

func TestInterpFeatureHas(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		set     policy.InterpFeature
		feature policy.InterpFeature
		want    bool
	}{
		{name: "all has for loops", set: policy.InterpFeaturesAll, feature: policy.InterpFeatureForLoops, want: true},
		{name: "all has panic/recover", set: policy.InterpFeaturesAll, feature: policy.InterpFeaturePanicRecover, want: true},
		{name: "all has composite", set: policy.InterpFeaturesAll, feature: policy.InterpFeatureGoroutines | policy.InterpFeatureChannels, want: true},
		{name: "restricted has closures", set: policy.InterpFeaturesRestricted, feature: policy.InterpFeatureClosures, want: true},
		{name: "restricted lacks goroutines", set: policy.InterpFeaturesRestricted, feature: policy.InterpFeatureGoroutines, want: false},
		{name: "restricted lacks unsafe", set: policy.InterpFeaturesRestricted, feature: policy.InterpFeatureUnsafeOps, want: false},
		{name: "restricted lacks goto", set: policy.InterpFeaturesRestricted, feature: policy.InterpFeatureGoto, want: false},
		{name: "restricted lacks panic/recover", set: policy.InterpFeaturesRestricted, feature: policy.InterpFeaturePanicRecover, want: false},
		{name: "restricted lacks composite with goroutines", set: policy.InterpFeaturesRestricted, feature: policy.InterpFeatureForLoops | policy.InterpFeatureGoroutines, want: false},
		{name: "none lacks for loops", set: policy.InterpFeaturesNone, feature: policy.InterpFeatureForLoops, want: false},
		{name: "minimal lacks recursion", set: policy.InterpFeaturesMinimal, feature: policy.InterpFeatureRecursion, want: false},
		{name: "any set has the empty feature", set: policy.InterpFeaturesNone, feature: policy.InterpFeaturesNone, want: true},
		{name: "single feature has itself", set: policy.InterpFeatureDefer, feature: policy.InterpFeatureDefer, want: true},
		{name: "single feature lacks another", set: policy.InterpFeatureDefer, feature: policy.InterpFeatureRangeLoops, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.set.Has(tc.feature))
		})
	}
}

func TestInterpFeatureString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		feature policy.InterpFeature
		want    string
	}{
		{name: "for loops", feature: policy.InterpFeatureForLoops, want: "for loops"},
		{name: "range loops", feature: policy.InterpFeatureRangeLoops, want: "range loops"},
		{name: "recursion", feature: policy.InterpFeatureRecursion, want: "recursion"},
		{name: "goroutines", feature: policy.InterpFeatureGoroutines, want: "goroutines"},
		{name: "channels", feature: policy.InterpFeatureChannels, want: "channels"},
		{name: "defer", feature: policy.InterpFeatureDefer, want: "defer"},
		{name: "goto", feature: policy.InterpFeatureGoto, want: "goto"},
		{name: "closures", feature: policy.InterpFeatureClosures, want: "closures"},
		{name: "unsafe", feature: policy.InterpFeatureUnsafeOps, want: "unsafe operations"},
		{name: "panic/recover", feature: policy.InterpFeaturePanicRecover, want: "panic/recover"},
		{name: "none is not a member", feature: policy.InterpFeaturesNone, want: "InterpFeature(0)"},
		{name: "composite is not a member", feature: policy.InterpFeatureForLoops | policy.InterpFeatureRangeLoops, want: "InterpFeature(3)"},
		{name: "all is not a member", feature: policy.InterpFeaturesAll, want: "InterpFeature(1023)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.feature.String())
		})
	}
}

func TestInterpFeatureSetsAreConsistent(t *testing.T) {
	t.Parallel()
	members := []policy.InterpFeature{
		policy.InterpFeatureForLoops, policy.InterpFeatureRangeLoops, policy.InterpFeatureRecursion,
		policy.InterpFeatureGoroutines, policy.InterpFeatureChannels, policy.InterpFeatureDefer,
		policy.InterpFeatureGoto, policy.InterpFeatureClosures, policy.InterpFeatureUnsafeOps,
		policy.InterpFeaturePanicRecover,
	}
	var union policy.InterpFeature
	for _, m := range members {
		require.Equal(t, 1, popCount(uint32(m)), "%s must be a single bit", m)
		union |= m
	}
	require.Equal(t, policy.InterpFeaturesAll, union)
	require.Equal(t, policy.InterpFeaturesNone, policy.InterpFeaturesMinimal)
	require.Equal(t, policy.InterpFeaturesNone, policy.InterpFeature(0))
	require.True(t, policy.InterpFeaturesAll.Has(policy.InterpFeaturesRestricted))
	require.False(t, policy.InterpFeaturesRestricted.Has(policy.InterpFeaturesAll))
}

func popCount(v uint32) int {
	n := 0
	for ; v != 0; v &= v - 1 {
		n++
	}
	return n
}

func TestLimitsDefaults(t *testing.T) {
	t.Parallel()
	require.Equal(t, "unsafe", policy.PkgUnsafe)
	require.Equal(t, int32(10_000), policy.DefaultMaxGoroutines)
}

func TestDefaultRestrictedSurface(t *testing.T) {
	t.Parallel()
	surface := policy.DefaultRestrictedSurface()

	for _, pkg := range []string{policy.PkgUnsafe, "runtime/debug", "runtime/pprof", "runtime/trace", "net/rpc"} {
		require.Contains(t, surface.FullyDenied, pkg)
	}
	require.Len(t, surface.FullyDenied, 5)

	require.Len(t, surface.Allowed, 1)
	reflectAllowed, ok := surface.Allowed["reflect"]
	require.True(t, ok)
	for _, sym := range []string{"TypeOf", "DeepEqual", "Indirect", "Kind", "Type", "Value", "Zero", "PtrTo"} {
		require.Contains(t, reflectAllowed, sym)
	}
	for _, sym := range []string{"MakeFunc", "MakeMap", "MakeSlice", "StructOf", "ValueOf", "New", "NewAt", "Call"} {
		require.NotContains(t, reflectAllowed, sym, "mutating reflect must stay off the allowlist")
	}

	again := policy.DefaultRestrictedSurface()
	again.FullyDenied["extra"] = struct{}{}
	require.NotContains(t, policy.DefaultRestrictedSurface().FullyDenied, "extra", "each call must build a fresh set")
}

func TestDescribeRestrictedSurface(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		surface     policy.RestrictedPackageSet
		wantExact   string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "empty surface prints only the header",
			surface:     policy.RestrictedPackageSet{Allowed: nil, FullyDenied: nil},
			wantExact:   "Restricted package surface:\n",
			wantContain: nil,
			wantAbsent:  nil,
		},
		{
			name:        "denied only",
			surface:     policy.RestrictedPackageSet{Allowed: nil, FullyDenied: map[string]struct{}{"unsafe": {}}},
			wantExact:   "Restricted package surface:\n  Fully denied packages:\n    - unsafe\n",
			wantContain: nil,
			wantAbsent:  []string{"Symbol-allowlisted"},
		},
		{
			name:        "allowlist only",
			surface:     policy.RestrictedPackageSet{Allowed: map[string]map[string]struct{}{"reflect": {"TypeOf": {}, "DeepEqual": {}}}, FullyDenied: nil},
			wantExact:   "Restricted package surface:\n  Symbol-allowlisted packages:\n    - reflect (2 symbols)\n",
			wantContain: nil,
			wantAbsent:  []string{"Fully denied"},
		},
		{
			name:        "default surface",
			surface:     policy.DefaultRestrictedSurface(),
			wantExact:   "",
			wantContain: []string{"Restricted package surface:\n", "  Fully denied packages:\n", "    - unsafe\n", "    - net/rpc\n", "  Symbol-allowlisted packages:\n", "    - reflect (8 symbols)\n"},
			wantAbsent:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := policy.DescribeRestrictedSurface(tc.surface)
			if tc.wantExact != "" {
				require.Equal(t, tc.wantExact, got)
			}
			for _, s := range tc.wantContain {
				require.Contains(t, got, s)
			}
			for _, s := range tc.wantAbsent {
				require.NotContains(t, got, s)
			}
		})
	}
}

func TestEffectiveCapabilityHook(t *testing.T) {
	t.Parallel()
	hook := &fakeHook{deny: nil, calls: nil}
	cases := []struct {
		name  string
		input policy.CapabilityHook
		want  policy.CapabilityHook
	}{
		{name: "nil falls back to permissive", input: nil, want: policy.PermissiveHook},
		{name: "permissive passes through", input: policy.PermissiveHook, want: policy.PermissiveHook},
		{name: "custom passes through", input: hook, want: hook},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, policy.EffectiveCapabilityHook(tc.input))
		})
	}
}

func TestPermissiveHookAllowsEverything(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	hook := policy.EffectiveCapabilityHook(nil)
	cases := []struct {
		name string
		call func() error
	}{
		{name: "function call", call: func() error { return hook.CheckFunctionCall(ctx, "mod", "os.Exit", nil) }},
		{name: "file open", call: func() error { return hook.CheckFileOpen(ctx, "mod", "/etc/passwd", os.O_RDONLY, 0) }},
		{name: "file write", call: func() error { return hook.CheckFileWrite(ctx, "mod", "/tmp/x") }},
		{name: "exec", call: func() error { return hook.CheckExec(ctx, "mod", "sh", []string{"sh", "-c", "true"}) }},
		{name: "net dial", call: func() error { return hook.CheckNetDial(ctx, "mod", "tcp", "example.com:443") }},
		{name: "net listen", call: func() error { return hook.CheckNetListen(ctx, "mod", "tcp", ":0") }},
		{name: "getenv", call: func() error { return hook.CheckGetenv(ctx, "mod", "HOME") }},
		{name: "setenv", call: func() error { return hook.CheckSetenv(ctx, "mod", "HOME", "/") }},
		{name: "subprocess", call: func() error { return hook.CheckSubprocess(ctx, "mod", "sh", nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, tc.call())
		})
	}
}
