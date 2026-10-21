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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGateSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    string
		want    []string
		wantErr bool
	}{
		{name: "empty", spec: "", want: nil},
		{name: "network", spec: "network", want: []string{AxisNetworkDial, AxisNetworkListen}},
		{name: "disk", spec: "disk", want: []string{AxisFilesystemRead, AxisFilesystemWrite}},
		{name: "env", spec: "env", want: []string{AxisEnvRead, AxisEnvWrite}},
		{name: "exec", spec: "exec", want: []string{AxisExec, AxisSubprocess}},
		{name: "two groups", spec: "network,disk", want: []string{AxisNetworkDial, AxisNetworkListen, AxisFilesystemRead, AxisFilesystemWrite}},
		{name: "all", spec: "all", want: allAxes},
		{name: "a repeated group", spec: "network, network", want: []string{AxisNetworkDial, AxisNetworkListen}},
		{name: "unknown group", spec: "bogus", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGateSpec(tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
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

	require.Error(t, hook.CheckFunctionCall(t.Context(), "", "os.ReadFile", strArgs("/etc/hostname")), "frozen mode denies a gated os.ReadFile")

	require.NoError(t, hook.CheckFunctionCall(t.Context(), "", "os.Getenv", strArgs("HOME")), "an ungated os.Getenv is allowed")

	require.NoError(t, hook.CheckFunctionCall(t.Context(), "", "strings.ToUpper", strArgs("x")), "an unmapped strings.ToUpper is allowed")
}

func TestCheckFunctionCallHonoursApproval(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	store.Upsert(LockedModule{Path: "", ApprovedCapabilities: []string{"network.dial(tcp:example.com:443)"}})
	hook := NewHook(HookModeFrozen, store, nil)
	hook.Gate(AxisNetworkDial)

	require.NoError(t, hook.CheckFunctionCall(t.Context(), "", "net.Dial", strArgs("tcp", "example.com:443")), "a pre-approved dial is allowed")
	require.Error(t, hook.CheckFunctionCall(t.Context(), "", "net.Dial", strArgs("tcp", "evil.example:443")), "an unapproved dial is denied")
}

func TestCheckFunctionCallUngatedHookAllowsEverything(t *testing.T) {
	t.Parallel()
	hook := NewHook(HookModeFrozen, nil, nil)
	require.NoError(t, hook.CheckFunctionCall(t.Context(), "", "os.ReadFile", strArgs("/etc/hostname")), "a hook with no gated axes allows everything")
}

func TestOpenFileClassifierPicksWriteAxisFromFlags(t *testing.T) {
	t.Parallel()
	readArgs := []reflect.Value{reflect.ValueOf("/tmp/x"), reflect.ValueOf(0), reflect.ValueOf(0)}
	require.Equal(t, AxisFilesystemRead, openFileClassifier(readArgs)[0].axis, "an O_RDONLY open is a read")
	writeArgs := []reflect.Value{reflect.ValueOf("/tmp/x"), reflect.ValueOf(577), reflect.ValueOf(0)}
	require.Equal(t, AxisFilesystemWrite, openFileClassifier(writeArgs)[0].axis, "an open with a write flag is a write")
}

func TestClassifyNativeCallCoversPathTakingFunctions(t *testing.T) {
	t.Parallel()
	temporary := os.TempDir()
	tests := []struct {
		fnPath string
		args   []reflect.Value
		want   []capabilityClaim
	}{
		{fnPath: "os.CreateTemp", args: strArgs("/outside", "p-*"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside"}}},
		{fnPath: "os.CreateTemp", args: strArgs("", "p-*"), want: []capabilityClaim{{AxisFilesystemWrite, temporary}}},
		{fnPath: "os.MkdirTemp", args: strArgs("/outside", "p-*"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside"}}},
		{fnPath: "io/ioutil.TempFile", args: strArgs("/outside", "p-*"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside"}}},
		{fnPath: "io/ioutil.TempDir", args: strArgs("/outside", "p-*"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside"}}},
		{fnPath: "os.CopyFS", args: strArgs("/outside/copy"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside/copy"}}},
		{fnPath: "os.Chtimes", args: strArgs("/outside/secret"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside/secret"}}},
		{fnPath: "os.Chmod", args: strArgs("/outside/secret"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside/secret"}}},
		{fnPath: "os.Chown", args: strArgs("/outside/secret"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside/secret"}}},
		{fnPath: "os.Lchown", args: strArgs("/outside/secret"), want: []capabilityClaim{{AxisFilesystemWrite, "/outside/secret"}}},
		{fnPath: "os.Stat", args: strArgs("/outside/secret"), want: []capabilityClaim{{AxisFilesystemRead, "/outside/secret"}}},
		{fnPath: "os.Lstat", args: strArgs("/outside/secret"), want: []capabilityClaim{{AxisFilesystemRead, "/outside/secret"}}},
		{fnPath: "os.ReadDir", args: strArgs("/outside"), want: []capabilityClaim{{AxisFilesystemRead, "/outside"}}},
		{fnPath: "os.Readlink", args: strArgs("/outside/link"), want: []capabilityClaim{{AxisFilesystemRead, "/outside/link"}}},
		{fnPath: "io/ioutil.ReadDir", args: strArgs("/outside"), want: []capabilityClaim{{AxisFilesystemRead, "/outside"}}},
		{fnPath: "path/filepath.WalkDir", args: strArgs("/outside"), want: []capabilityClaim{{AxisFilesystemRead, "/outside"}}},
		{fnPath: "path/filepath.Walk", args: strArgs("/outside"), want: []capabilityClaim{{AxisFilesystemRead, "/outside"}}},
		{fnPath: "path/filepath.Glob", args: strArgs("/outside/*"), want: []capabilityClaim{{AxisFilesystemRead, "/outside/*"}}},
		{fnPath: "os.Rename", args: strArgs("/a", "/b"), want: []capabilityClaim{{AxisFilesystemWrite, "/a"}, {AxisFilesystemWrite, "/b"}}},
		{fnPath: "os.Link", args: strArgs("/a", "/b"), want: []capabilityClaim{{AxisFilesystemWrite, "/a"}, {AxisFilesystemWrite, "/b"}}},
		{fnPath: "os.Symlink", args: strArgs("/etc/hostname", "/b"), want: []capabilityClaim{{AxisFilesystemWrite, "/b"}, {AxisFilesystemRead, "/etc/hostname"}}},
		{fnPath: "crypto/tls.LoadX509KeyPair", args: strArgs("/c", "/k"), want: []capabilityClaim{{AxisFilesystemRead, "/c"}, {AxisFilesystemRead, "/k"}}},
	}
	for _, tt := range tests {
		t.Run(tt.fnPath+tt.want[0].scope, func(t *testing.T) {
			t.Parallel()
			claims, gated := classifyNativeCall(tt.fnPath, tt.args)
			require.True(t, gated, "%s is gated", tt.fnPath)
			require.Equal(t, tt.want, claims)
		})
	}
}
