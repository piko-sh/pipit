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

package modloader

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseGateSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spec string
		want []string
		err  bool
	}{
		{"", nil, false},
		{"network", []string{AxisNetworkDial, AxisNetworkListen}, false},
		{"disk", []string{AxisFilesystemRead, AxisFilesystemWrite}, false},
		{"env", []string{AxisEnvRead, AxisEnvWrite}, false},
		{"exec", []string{AxisExec, AxisSubprocess}, false},
		{"network,disk", []string{AxisNetworkDial, AxisNetworkListen, AxisFilesystemRead, AxisFilesystemWrite}, false},
		{"all", allAxes, false},
		{"network, network", []string{AxisNetworkDial, AxisNetworkListen}, false},
		{"bogus", nil, true},
	}
	for _, tc := range cases {
		got, err := ParseGateSpec(tc.spec)
		if tc.err {
			if err == nil {
				t.Fatalf("ParseGateSpec(%q) expected error", tc.spec)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseGateSpec(%q) unexpected error: %v", tc.spec, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("ParseGateSpec(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}

func strArgs(values ...string) []reflect.Value {
	args := make([]reflect.Value, len(values))
	for i, v := range values {
		args[i] = reflect.ValueOf(v)
	}
	return args
}

func TestCheckFunctionCallGatesSelectedAxisOnly(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	hook := NewHook(HookModeFrozen, store, nil)
	hook.Gate(AxisFilesystemRead, AxisFilesystemWrite)

	if err := hook.CheckFunctionCall(context.Background(), "", "os.ReadFile", strArgs("/etc/hostname")); err == nil {
		t.Fatalf("expected gated os.ReadFile to be denied in frozen mode")
	}

	if err := hook.CheckFunctionCall(context.Background(), "", "os.Getenv", strArgs("HOME")); err != nil {
		t.Fatalf("un-gated os.Getenv should be allowed, got %v", err)
	}

	if err := hook.CheckFunctionCall(context.Background(), "", "strings.ToUpper", strArgs("x")); err != nil {
		t.Fatalf("unmapped strings.ToUpper should be allowed, got %v", err)
	}
}

func TestCheckFunctionCallHonoursApproval(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	store.Upsert(LockedModule{Path: "", ApprovedCapabilities: []string{"network.dial(tcp:example.com:443)"}})
	hook := NewHook(HookModeFrozen, store, nil)
	hook.Gate(AxisNetworkDial)

	if err := hook.CheckFunctionCall(context.Background(), "", "net.Dial", strArgs("tcp", "example.com:443")); err != nil {
		t.Fatalf("pre-approved dial should pass, got %v", err)
	}
	if err := hook.CheckFunctionCall(context.Background(), "", "net.Dial", strArgs("tcp", "evil.example:443")); err == nil {
		t.Fatalf("unapproved dial should be denied")
	}
}

func TestCheckFunctionCallUngatedHookAllowsEverything(t *testing.T) {
	t.Parallel()
	hook := NewHook(HookModeFrozen, nil, nil)
	if err := hook.CheckFunctionCall(context.Background(), "", "os.ReadFile", strArgs("/etc/hostname")); err != nil {
		t.Fatalf("hook with no gated axes should allow all, got %v", err)
	}
}

func TestOpenFileClassifierPicksWriteAxisFromFlags(t *testing.T) {
	t.Parallel()
	readArgs := []reflect.Value{reflect.ValueOf("/tmp/x"), reflect.ValueOf(0), reflect.ValueOf(0)}
	axis, _ := openFileClassifier(readArgs)
	if axis != AxisFilesystemRead {
		t.Fatalf("O_RDONLY open should classify as read, got %q", axis)
	}
	writeArgs := []reflect.Value{reflect.ValueOf("/tmp/x"), reflect.ValueOf(577), reflect.ValueOf(0)}
	axis, _ = openFileClassifier(writeArgs)
	if axis != AxisFilesystemWrite {
		t.Fatalf("write-flag open should classify as write, got %q", axis)
	}
}
