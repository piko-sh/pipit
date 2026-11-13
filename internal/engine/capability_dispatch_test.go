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

package engine

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/policy"
)

type recordingCapabilityHook struct {
	policy.PermissiveCapabilityHook
	gate   string
	first  string
	second string
	flag   int
	argv   []string
}

func (hook *recordingCapabilityHook) CheckFileOpen(_ context.Context, _, path string, flag int, _ os.FileMode) error {
	hook.gate, hook.first, hook.flag = "fileopen", path, flag
	return nil
}

func (hook *recordingCapabilityHook) CheckFileWrite(_ context.Context, _, path string) error {
	hook.gate, hook.first = "filewrite", path
	return nil
}

func (hook *recordingCapabilityHook) CheckNetDial(_ context.Context, _, network, address string) error {
	hook.gate, hook.first, hook.second = "netdial", network, address
	return nil
}

func (hook *recordingCapabilityHook) CheckNetListen(_ context.Context, _, network, address string) error {
	hook.gate, hook.first, hook.second = "netlisten", network, address
	return nil
}

func (hook *recordingCapabilityHook) CheckSubprocess(_ context.Context, _, name string, argv []string) error {
	hook.gate, hook.first, hook.argv = "subprocess", name, argv
	return nil
}

func TestConsultSpecialisedCapabilityGateRouting(t *testing.T) {
	t.Parallel()
	strv := func(values ...string) []reflect.Value {
		out := make([]reflect.Value, len(values))
		for index, value := range values {
			out[index] = reflect.ValueOf(value)
		}
		return out
	}
	tests := []struct {
		name   string
		path   string
		args   []reflect.Value
		gate   string
		first  string
		second string
	}{
		{name: "http get", path: "net/http.Get", args: strv("https://example.com/x"), gate: "netdial", first: "tcp", second: "example.com:443"},
		{name: "http get port", path: "net/http.Get", args: strv("http://example.com:8080/x"), gate: "netdial", first: "tcp", second: "example.com:8080"},
		{name: "http post", path: "net/http.Post", args: strv("http://host/x", "text/plain"), gate: "netdial", first: "tcp", second: "host:80"},
		{name: "http serve", path: "net/http.ListenAndServe", args: strv(":8080", ""), gate: "netlisten", first: "tcp", second: ":8080"},
		{name: "listen packet", path: "net.ListenPacket", args: strv("udp", ":9000"), gate: "netlisten", first: "udp", second: ":9000"},
		{name: "dirfs", path: "os.DirFS", args: strv("/data"), gate: "fileopen", first: "/data", second: ""},
		{name: "open root", path: "os.OpenRoot", args: strv("/data"), gate: "fileopen", first: "/data", second: ""},
		{name: "start process", path: "os.StartProcess", args: strv("/bin/true"), gate: "subprocess", first: "/bin/true", second: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hook := &recordingCapabilityHook{}
			require.NoError(t, consultSpecialisedCapabilityGate(t.Context(), hook, "mod", tt.path, tt.args))
			require.Equal(t, tt.gate, hook.gate)
			require.Equal(t, tt.first, hook.first)
			require.Equal(t, tt.second, hook.second)
		})
	}
}

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

func TestConsultSpecialisedCapabilityGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		fnPath     string
		args       []reflect.Value
		wantMethod string
		wantFields map[string]any
	}{
		{
			name:       "os.Open routes to CheckFileOpen read-only",
			fnPath:     "os.Open",
			args:       []reflect.Value{reflect.ValueOf("/etc/hosts")},
			wantMethod: "CheckFileOpen",
			wantFields: map[string]any{"path": "/etc/hosts", "flag": os.O_RDONLY, "mode": os.FileMode(0)},
		},
		{
			name:       "os.OpenFile routes flag and mode through",
			fnPath:     "os.OpenFile",
			args:       []reflect.Value{reflect.ValueOf("/tmp/x"), reflect.ValueOf(os.O_WRONLY), reflect.ValueOf(os.FileMode(0o644))},
			wantMethod: "CheckFileOpen",
			wantFields: map[string]any{"path": "/tmp/x", "flag": os.O_WRONLY, "mode": os.FileMode(0o644)},
		},
		{
			name:       "os.Remove routes to CheckFileWrite",
			fnPath:     "os.Remove",
			args:       []reflect.Value{reflect.ValueOf("/tmp/x")},
			wantMethod: "CheckFileWrite",
			wantFields: map[string]any{"path": "/tmp/x"},
		},
		{
			name:       "os.Getenv routes to CheckGetenv",
			fnPath:     "os.Getenv",
			args:       []reflect.Value{reflect.ValueOf("PATH")},
			wantMethod: "CheckGetenv",
			wantFields: map[string]any{"name": "PATH"},
		},
		{
			name:       "os.Setenv routes name and value",
			fnPath:     "os.Setenv",
			args:       []reflect.Value{reflect.ValueOf("K"), reflect.ValueOf("V")},
			wantMethod: "CheckSetenv",
			wantFields: map[string]any{"name": "K", "value": "V"},
		},
		{
			name:       "net.Dial routes network and address",
			fnPath:     "net.Dial",
			args:       []reflect.Value{reflect.ValueOf("tcp"), reflect.ValueOf("evil.example:443")},
			wantMethod: "CheckNetDial",
			wantFields: map[string]any{"network": "tcp", "address": "evil.example:443"},
		},
		{
			name:       "exec.Command routes name and argv",
			fnPath:     "os/exec.Command",
			args:       []reflect.Value{reflect.ValueOf("rm"), reflect.ValueOf("-rf"), reflect.ValueOf("/")},
			wantMethod: "CheckExec",
			wantFields: map[string]any{"name": "rm", "argv": []string{"rm", "-rf", "/"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hook := &recordingHook{}
			err := consultSpecialisedCapabilityGate(t.Context(), hook, "mod", tt.fnPath, tt.args)
			require.NoError(t, err)

			calls := hook.snapshot()
			require.Len(t, calls, 1)
			require.Equal(t, tt.wantMethod, calls[0].Method)
			for key, want := range tt.wantFields {
				require.Equal(t, want, calls[0].Fields[key], "field %q", key)
			}
		})
	}
}

func TestConsultSpecialisedCapabilityGateDenial(t *testing.T) {
	t.Parallel()
	denied := errors.New("file access denied")
	hook := &recordingHook{denyFn: func(call recordedHookCall) error {
		if call.Method == "CheckFileOpen" {
			return denied
		}
		return nil
	}}

	err := consultSpecialisedCapabilityGate(t.Context(), hook, "mod", "os.Open",
		[]reflect.Value{reflect.ValueOf("/etc/shadow")})
	require.ErrorIs(t, err, denied)
}

func TestConsultSpecialisedCapabilityGateUnmatched(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}

	require.NoError(t, consultSpecialisedCapabilityGate(t.Context(), hook, "mod",
		"strings.ToUpper", []reflect.Value{reflect.ValueOf("x")}))
	require.NoError(t, consultSpecialisedCapabilityGate(t.Context(), hook, "mod",
		"os.OpenFile", []reflect.Value{reflect.ValueOf("/tmp/x")}))

	require.Empty(t, hook.snapshot(), "no specialised gate fires for unmatched or malformed calls")
}

func capabilityStrings(values ...string) []reflect.Value {
	out := make([]reflect.Value, len(values))
	for index, value := range values {
		out[index] = reflect.ValueOf(value)
	}
	return out
}

func TestFilesystemGateChecksEveryPathArgument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fnPath string
		args   []reflect.Value
		want   []recordedHookCall
	}{
		{
			name:   "rename checks source and destination",
			fnPath: "os.Rename",
			args:   capabilityStrings("/allowed/a", "/outside/b"),
			want: []recordedHookCall{
				{Method: "CheckFileWrite", Fields: map[string]any{"path": "/allowed/a"}},
				{Method: "CheckFileWrite", Fields: map[string]any{"path": "/outside/b"}},
			},
		},
		{
			name:   "link checks the existing name and the new name",
			fnPath: "os.Link",
			args:   capabilityStrings("/outside/secret", "/allowed/link"),
			want: []recordedHookCall{
				{Method: "CheckFileWrite", Fields: map[string]any{"path": "/outside/secret"}},
				{Method: "CheckFileWrite", Fields: map[string]any{"path": "/allowed/link"}},
			},
		},
		{
			name:   "symlink checks the new name and the absolute target",
			fnPath: "os.Symlink",
			args:   capabilityStrings("/etc/hostname", "/outside/planted"),
			want: []recordedHookCall{
				{Method: "CheckFileWrite", Fields: map[string]any{"path": "/outside/planted"}},
				{Method: "CheckFileOpen", Fields: map[string]any{"path": "/etc/hostname", "flag": os.O_RDONLY}},
			},
		},
		{
			name:   "symlink resolves a relative target against the new name's directory",
			fnPath: "os.Symlink",
			args:   capabilityStrings("../outside/secret", "/allowed/link"),
			want: []recordedHookCall{
				{Method: "CheckFileWrite", Fields: map[string]any{"path": "/allowed/link"}},
				{Method: "CheckFileOpen", Fields: map[string]any{"path": "/outside/secret", "flag": os.O_RDONLY}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hook := &recordingHook{}
			require.NoError(t, consultSpecialisedCapabilityGate(t.Context(), hook, "mod", tt.fnPath, tt.args))
			calls := hook.snapshot()
			require.Len(t, calls, len(tt.want))
			for index, want := range tt.want {
				require.Equal(t, want.Method, calls[index].Method)
				for key, value := range want.Fields {
					require.Equal(t, value, calls[index].Fields[key], "call %d field %q", index, key)
				}
			}
		})
	}
}

func TestFilesystemGateDeniesRenameToAnOutsideDestination(t *testing.T) {
	t.Parallel()
	denied := errors.New("outside")
	hook := &recordingHook{denyFn: func(call recordedHookCall) error {
		if call.Fields["path"] == "/outside/b" {
			return denied
		}
		return nil
	}}
	err := consultSpecialisedCapabilityGate(t.Context(), hook, "mod", "os.Rename", capabilityStrings("/allowed/a", "/outside/b"))
	require.ErrorIs(t, err, denied)
}

func TestFilesystemGateCoversPathTakingFunctions(t *testing.T) {
	t.Parallel()
	temporary := os.TempDir()
	tests := []struct {
		fnPath     string
		args       []reflect.Value
		wantMethod string
		wantPath   string
	}{
		{fnPath: "io/ioutil.WriteFile", args: capabilityStrings("/outside/x"), wantMethod: "CheckFileWrite", wantPath: "/outside/x"},
		{fnPath: "io/ioutil.ReadFile", args: capabilityStrings("/outside/x"), wantMethod: "CheckFileOpen", wantPath: "/outside/x"},
		{fnPath: "io/ioutil.ReadDir", args: capabilityStrings("/outside"), wantMethod: "CheckFileOpen", wantPath: "/outside"},
		{fnPath: "io/ioutil.TempFile", args: capabilityStrings("/outside", "p-*"), wantMethod: "CheckFileWrite", wantPath: "/outside"},
		{fnPath: "io/ioutil.TempDir", args: capabilityStrings("", "p-*"), wantMethod: "CheckFileWrite", wantPath: temporary},
		{fnPath: "os.CreateTemp", args: capabilityStrings("/outside", "p-*"), wantMethod: "CheckFileWrite", wantPath: "/outside"},
		{fnPath: "os.CreateTemp", args: capabilityStrings("", "p-*"), wantMethod: "CheckFileWrite", wantPath: temporary},
		{fnPath: "os.MkdirTemp", args: capabilityStrings("/outside", "p-*"), wantMethod: "CheckFileWrite", wantPath: "/outside"},
		{fnPath: "os.CopyFS", args: []reflect.Value{reflect.ValueOf("/outside/copy"), reflect.ValueOf(os.DirFS("/"))}, wantMethod: "CheckFileWrite", wantPath: "/outside/copy"},
		{fnPath: "os.Chtimes", args: capabilityStrings("/outside/secret"), wantMethod: "CheckFileWrite", wantPath: "/outside/secret"},
		{fnPath: "os.Chown", args: capabilityStrings("/outside/secret"), wantMethod: "CheckFileWrite", wantPath: "/outside/secret"},
		{fnPath: "os.Lchown", args: capabilityStrings("/outside/secret"), wantMethod: "CheckFileWrite", wantPath: "/outside/secret"},
		{fnPath: "os.Chdir", args: capabilityStrings("/outside"), wantMethod: "CheckFileOpen", wantPath: "/outside"},
		{fnPath: "os.Stat", args: capabilityStrings("/outside/secret"), wantMethod: "CheckFileOpen", wantPath: "/outside/secret"},
		{fnPath: "os.Lstat", args: capabilityStrings("/outside/secret"), wantMethod: "CheckFileOpen", wantPath: "/outside/secret"},
		{fnPath: "os.ReadDir", args: capabilityStrings("/outside"), wantMethod: "CheckFileOpen", wantPath: "/outside"},
		{fnPath: "os.Readlink", args: capabilityStrings("/outside/link"), wantMethod: "CheckFileOpen", wantPath: "/outside/link"},
		{fnPath: "path/filepath.Walk", args: capabilityStrings("/outside"), wantMethod: "CheckFileOpen", wantPath: "/outside"},
		{fnPath: "path/filepath.WalkDir", args: capabilityStrings("/outside"), wantMethod: "CheckFileOpen", wantPath: "/outside"},
		{fnPath: "path/filepath.Glob", args: capabilityStrings("/outside/*"), wantMethod: "CheckFileOpen", wantPath: "/outside/*"},
		{fnPath: "path/filepath.EvalSymlinks", args: capabilityStrings("/outside/link"), wantMethod: "CheckFileOpen", wantPath: "/outside/link"},
		{fnPath: "archive/zip.OpenReader", args: capabilityStrings("/outside/a.zip"), wantMethod: "CheckFileOpen", wantPath: "/outside/a.zip"},
		{fnPath: "debug/elf.Open", args: capabilityStrings("/outside/bin"), wantMethod: "CheckFileOpen", wantPath: "/outside/bin"},
		{fnPath: "debug/pe.Open", args: capabilityStrings("/outside/bin"), wantMethod: "CheckFileOpen", wantPath: "/outside/bin"},
		{fnPath: "debug/macho.Open", args: capabilityStrings("/outside/bin"), wantMethod: "CheckFileOpen", wantPath: "/outside/bin"},
		{fnPath: "debug/buildinfo.ReadFile", args: capabilityStrings("/outside/bin"), wantMethod: "CheckFileOpen", wantPath: "/outside/bin"},
		{fnPath: "go/build.ImportDir", args: capabilityStrings("/outside/pkg"), wantMethod: "CheckFileOpen", wantPath: "/outside/pkg"},
		{fnPath: "text/template.ParseGlob", args: capabilityStrings("/outside/*.tmpl"), wantMethod: "CheckFileOpen", wantPath: "/outside/*.tmpl"},
		{fnPath: "html/template.ParseGlob", args: capabilityStrings("/outside/*.tmpl"), wantMethod: "CheckFileOpen", wantPath: "/outside/*.tmpl"},
	}
	for _, tt := range tests {
		t.Run(tt.fnPath+"/"+tt.wantPath, func(t *testing.T) {
			t.Parallel()
			hook := &recordingHook{}
			require.NoError(t, consultSpecialisedCapabilityGate(t.Context(), hook, "mod", tt.fnPath, tt.args))
			calls := hook.snapshot()
			require.NotEmpty(t, calls, "%s reached no typed gate", tt.fnPath)
			require.Equal(t, tt.wantMethod, calls[0].Method)
			require.Equal(t, tt.wantPath, calls[0].Fields["path"])
		})
	}
}

func TestFilesystemGateChecksEveryFileOfAMultiPathCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fnPath string
		args   []reflect.Value
		want   []string
	}{
		{fnPath: "text/template.ParseFiles", args: capabilityStrings("/a.tmpl", "/b.tmpl"), want: []string{"/a.tmpl", "/b.tmpl"}},
		{fnPath: "html/template.ParseFiles", args: []reflect.Value{reflect.ValueOf([]string{"/a.tmpl", "/b.tmpl"})}, want: []string{"/a.tmpl", "/b.tmpl"}},
		{fnPath: "crypto/tls.LoadX509KeyPair", args: capabilityStrings("/cert.pem", "/key.pem"), want: []string{"/cert.pem", "/key.pem"}},
		{fnPath: "go/parser.ParseDir", args: []reflect.Value{reflect.ValueOf(0), reflect.ValueOf("/outside/pkg")}, want: []string{"/outside/pkg"}},
		{fnPath: "go/parser.ParseFile", args: []reflect.Value{reflect.ValueOf(0), reflect.ValueOf("/outside/x.go")}, want: []string{"/outside/x.go"}},
		{fnPath: "net/http.ServeFile", args: []reflect.Value{reflect.ValueOf(0), reflect.ValueOf(0), reflect.ValueOf("/outside/x")}, want: []string{"/outside/x"}},
	}
	for _, tt := range tests {
		t.Run(tt.fnPath, func(t *testing.T) {
			t.Parallel()
			hook := &recordingHook{}
			require.NoError(t, consultSpecialisedCapabilityGate(t.Context(), hook, "mod", tt.fnPath, tt.args))
			calls := hook.snapshot()
			paths := make([]string, 0, len(calls))
			for _, call := range calls {
				require.Equal(t, "CheckFileOpen", call.Method)
				paths = append(paths, call.Fields["path"].(string))
			}
			require.Equal(t, tt.want, paths)
		})
	}
}

func TestNativeCallGateResolvesTheFunctionOnEveryCall(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	vm := &VM{}
	vm.Limits.CapabilityHook = hook
	site := &program.CallSite{}

	require.NoError(t, consultCapabilityHookForNativeCall(vm, site, reflect.ValueOf(strings.ToUpper), capabilityStrings("x")))
	require.NoError(t, consultCapabilityHookForNativeCall(vm, site, reflect.ValueOf(os.Remove), capabilityStrings("/outside/x")))

	calls := hook.snapshot()
	require.Len(t, calls, 3)
	require.Equal(t, "CheckFunctionCall", calls[0].Method)
	require.Equal(t, "strings.ToUpper", calls[0].Fields["fnPath"])
	require.Equal(t, "CheckFileWrite", calls[1].Method, "a second function through the same call site is gated as itself")
	require.Equal(t, "/outside/x", calls[1].Fields["path"])
	require.Equal(t, "os.Remove", calls[2].Fields["fnPath"])
}

func TestNativeCallGateKeysOnTheRegisteredIdentity(t *testing.T) {
	t.Parallel()
	removeThroughWrapper := func(name string) error { return os.Remove(name) }
	policy.RecordNativeSymbols("os", map[string]reflect.Value{"Remove": reflect.ValueOf(removeThroughWrapper)})

	hook := &recordingHook{}
	vm := &VM{}
	vm.Limits.CapabilityHook = hook
	require.NoError(t, consultCapabilityHookForNativeCall(vm, &program.CallSite{}, reflect.ValueOf(removeThroughWrapper), capabilityStrings("/outside/x")))

	calls := hook.snapshot()
	require.Len(t, calls, 2)
	require.Equal(t, "CheckFileWrite", calls[0].Method, "a wrapper registered as os.Remove is gated as os.Remove")
	require.Equal(t, "os.Remove", calls[1].Fields["fnPath"])
}

func TestGoStatementRunsTheTypedGates(t *testing.T) {
	t.Parallel()
	denied := errors.New("outside")
	hook := &recordingHook{denyFn: func(call recordedHookCall) error {
		if call.Method == "CheckFileWrite" {
			return denied
		}
		return nil
	}}
	vm := &VM{}
	vm.Limits.CapabilityHook = hook

	err := consultCapabilityHookForGoStatement(vm, reflect.ValueOf(os.WriteFile), capabilityStrings("/outside/x"))
	require.ErrorIs(t, err, denied)

	hook.denyFn = nil
	require.NoError(t, consultCapabilityHookForGoStatement(vm, reflect.ValueOf(strings.ToUpper), capabilityStrings("x")))
	calls := hook.snapshot()
	require.Equal(t, "go strings.ToUpper", calls[len(calls)-1].Fields["fnPath"])

	require.NoError(t, consultCapabilityHookForGoStatement(&VM{}, reflect.ValueOf(os.WriteFile), nil), "no hook means no gate")
}
