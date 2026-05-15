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
	"net"
	"net/url"
	"os"
	"reflect"
	"runtime"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// defaultFileCreateMode is the permission bitset os.Create passes to the underlying
	// OpenFile, surfaced to the capability gate so a host sees the same mode the stdlib
	// would apply.
	defaultFileCreateMode os.FileMode = 0o666
)

// consultSpecialisedCapabilityGate routes a resolved native symbol to its typed gate.
//
// Dispatches to the matching typed capability gate (CheckFileOpen, CheckExec,
// CheckNetDial, ...) so a host can apply structured policy to filesystem, process,
// network, and environment operations rather than only the flat CheckFunctionCall path.
// The catch-all CheckFunctionCall still runs afterwards.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes functionPath (string) which is the resolved dotted native symbol.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns the first denial from a matched gate, or nil when no specialised gate applies.
func consultSpecialisedCapabilityGate(ctx context.Context, hook policy.CapabilityHook, modulePath, functionPath string, arguments []reflect.Value) error {
	if matched, err := gateFilesystemCapability(ctx, hook, modulePath, functionPath, arguments); matched {
		return err
	}
	if matched, err := gateEnvironmentCapability(ctx, hook, modulePath, functionPath, arguments); matched {
		return err
	}
	if matched, err := gateProcessNetworkCapability(ctx, hook, modulePath, functionPath, arguments); matched {
		return err
	}
	if matched, err := gateHTTPCapability(ctx, hook, modulePath, functionPath, arguments); matched {
		return err
	}
	return nil
}

// consultCapabilityHookForNativeCall gates a native call through the hook. No-op when no
// hook is installed.
//
// Takes vm (*VM) which carries the live hook and execution context.
// Takes site (*CallSite) which holds the cached symbol path.
// Takes reflectedFunction (reflect.Value) which is the function about to be dispatched.
// Takes arguments ([]reflect.Value) which is the prepared argument slice.
//
// Returns error from the hook's denial, or nil to allow the call to proceed.
func consultCapabilityHookForNativeCall(vm *VM, site *program.CallSite, reflectedFunction reflect.Value, arguments []reflect.Value) error {
	hook := vm.Limits.CapabilityHook
	if hook == nil {
		return nil
	}
	if site.NativeFunctionPath == "" {
		site.NativeFunctionPath = resolveNativeFunctionPath(reflectedFunction)
	}
	ctx := capabilityHookContext(vm)
	if denial := consultSpecialisedCapabilityGate(ctx, hook, vm.modulePath, site.NativeFunctionPath, arguments); denial != nil {
		return denial
	}
	return hook.CheckFunctionCall(ctx, vm.modulePath, site.NativeFunctionPath, arguments)
}

// gateFilesystemCapability routes the filesystem stdlib symbols to CheckFileOpen or
// CheckFileWrite. It reports matched=true when functionPath names a filesystem primitive
// (even if the arguments cannot be classified, in which case the specialised gate is
// skipped and err is nil), so the caller stops probing further categories.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes functionPath (string) which is the resolved dotted native symbol.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns (matched, err): matched reports whether functionPath is a filesystem symbol;
// err is the gate's denial or nil.
func gateFilesystemCapability(ctx context.Context, hook policy.CapabilityHook, modulePath, functionPath string, arguments []reflect.Value) (bool, error) {
	switch functionPath {
	case "os.Open", "os.ReadFile", "os.DirFS", "os.OpenRoot":
		if path, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckFileOpen(ctx, modulePath, path, os.O_RDONLY, 0)
		}
		return true, nil
	case "os.Create":
		if path, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckFileOpen(ctx, modulePath, path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, defaultFileCreateMode)
		}
		return true, nil
	case "os.OpenFile":
		path, pathOK := capabilityStringArg(arguments, 0)
		flag, flagOK := capabilityIntArg(arguments, 1)
		mode, modeOK := capabilityFileModeArg(arguments, 2)
		if pathOK && flagOK && modeOK {
			return true, hook.CheckFileOpen(ctx, modulePath, path, flag, mode)
		}
		return true, nil
	case "os.WriteFile", "os.Remove", "os.RemoveAll", "os.Mkdir", "os.MkdirAll", "os.Rename", "os.Truncate", "os.Symlink", "os.Link", "os.Chmod":
		if path, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckFileWrite(ctx, modulePath, path)
		}
		return true, nil
	case "os.OpenInRoot":
		if path, ok := capabilityStringArg(arguments, 1); ok {
			return true, hook.CheckFileOpen(ctx, modulePath, path, os.O_RDONLY, 0)
		}
		return true, nil
	}
	return false, nil
}

// gateEnvironmentCapability routes the environment stdlib symbols to CheckGetenv or
// CheckSetenv. See gateFilesystemCapability for the matched/err contract.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes functionPath (string) which is the resolved dotted native symbol.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns (matched, err): matched reports whether functionPath is an environment symbol;
// err is the gate's denial or nil.
func gateEnvironmentCapability(ctx context.Context, hook policy.CapabilityHook, modulePath, functionPath string, arguments []reflect.Value) (bool, error) {
	switch functionPath {
	case "os.Getenv", "os.LookupEnv":
		if name, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckGetenv(ctx, modulePath, name)
		}
		return true, nil
	case "os.Setenv":
		name, nameOK := capabilityStringArg(arguments, 0)
		value, valueOK := capabilityStringArg(arguments, 1)
		if nameOK && valueOK {
			return true, hook.CheckSetenv(ctx, modulePath, name, value)
		}
		return true, nil
	case "os.Unsetenv":
		if name, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckSetenv(ctx, modulePath, name, "")
		}
		return true, nil
	}
	return false, nil
}

// gateProcessNetworkCapability routes the subprocess and network stdlib symbols to
// CheckExec, CheckNetDial, or CheckNetListen. See gateFilesystemCapability for the
// matched/err contract.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes functionPath (string) which is the resolved dotted native symbol.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns (matched, err): matched reports whether functionPath is a process or network
// symbol; err is the gate's denial or nil.
func gateProcessNetworkCapability(ctx context.Context, hook policy.CapabilityHook, modulePath, functionPath string, arguments []reflect.Value) (bool, error) {
	switch functionPath {
	case "os/exec.Command":
		if name, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckExec(ctx, modulePath, name, capabilityCommandArgv(name, arguments, 1))
		}
		return true, nil
	case "os/exec.CommandContext":
		if name, ok := capabilityStringArg(arguments, 1); ok {
			return true, hook.CheckExec(ctx, modulePath, name, capabilityCommandArgv(name, arguments, 2))
		}
		return true, nil
	case "net.Dial", "net.DialTimeout":
		network, networkOK := capabilityStringArg(arguments, 0)
		address, addressOK := capabilityStringArg(arguments, 1)
		if networkOK && addressOK {
			return true, hook.CheckNetDial(ctx, modulePath, network, address)
		}
		return true, nil
	case "net.Listen", "net.ListenPacket":
		network, networkOK := capabilityStringArg(arguments, 0)
		address, addressOK := capabilityStringArg(arguments, 1)
		if networkOK && addressOK {
			return true, hook.CheckNetListen(ctx, modulePath, network, address)
		}
		return true, nil
	case "os.StartProcess":
		if name, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckSubprocess(ctx, modulePath, name, capabilityStringSliceArg(arguments, 1))
		}
		return true, nil
	case "os/exec.(*Cmd).Start", "os/exec.(*Cmd).Run", "os/exec.(*Cmd).Output", "os/exec.(*Cmd).CombinedOutput":

		return true, nil
	}
	return false, nil
}

// gateHTTPCapability routes the net/http convenience functions to CheckNetDial for
// outbound requests and CheckNetListen for servers, deriving the host from the target URL
// or listen address. See gateFilesystemCapability for the matched/err contract.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes functionPath (string) which is the resolved dotted native symbol.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns (matched, err) with the same contract as the other gates.
func gateHTTPCapability(ctx context.Context, hook policy.CapabilityHook, modulePath, functionPath string, arguments []reflect.Value) (bool, error) {
	switch functionPath {
	case "net/http.Get", "net/http.Head", "net/http.Post", "net/http.PostForm":
		if target, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckNetDial(ctx, modulePath, "tcp", httpDialAddress(target))
		}
		return true, nil
	case "net/http.ListenAndServe", "net/http.ListenAndServeTLS":
		if address, ok := capabilityStringArg(arguments, 0); ok {
			return true, hook.CheckNetListen(ctx, modulePath, "tcp", address)
		}
		return true, nil
	}
	return false, nil
}

// httpDialAddress derives a host:port dial address from an HTTP request URL, defaulting
// the port from the scheme so the network gate sees the endpoint the client would reach.
//
// Takes target (string) which is a request URL.
//
// Returns string which is host:port, or the raw target when it cannot be parsed.
func httpDialAddress(target string) string {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return target
	}
	if parsed.Port() != "" {
		return parsed.Host
	}
	port := "80"
	if parsed.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(parsed.Hostname(), port)
}

// capabilityStringSliceArg returns the []string at index i, or nil when absent or of a
// different type.
//
// Takes arguments ([]reflect.Value) which is the argument slice to read.
// Takes index (int) which is the position to read.
//
// Returns []string which is the argument's value, or nil.
func capabilityStringSliceArg(arguments []reflect.Value, index int) []string {
	if index < 0 || index >= len(arguments) {
		return nil
	}
	value := arguments[index]
	if !value.IsValid() || value.Kind() != reflect.Slice || value.Type().Elem().Kind() != reflect.String {
		return nil
	}
	result := make([]string, value.Len())
	for element := range result {
		result[element] = value.Index(element).String()
	}
	return result
}

// capabilityStringArg returns the string at index i when present and of string kind.
//
// Takes arguments ([]reflect.Value) which is the argument slice to read.
// Takes index (int) which is the position to read.
//
// Returns string which is the argument's string value.
// Returns bool which reports whether a string was present at that index.
func capabilityStringArg(arguments []reflect.Value, index int) (string, bool) {
	if index < 0 || index >= len(arguments) {
		return "", false
	}
	value := arguments[index]
	if !value.IsValid() || value.Kind() != reflect.String {
		return "", false
	}
	return value.String(), true
}

// capabilityIntArg returns the int at index i for any signed-integer kind.
//
// Takes arguments ([]reflect.Value) which is the argument slice to read.
// Takes index (int) which is the position to read.
//
// Returns int which is the argument's integer value.
// Returns bool which reports whether a signed integer was present at that index.
func capabilityIntArg(arguments []reflect.Value, index int) (int, bool) {
	if index < 0 || index >= len(arguments) {
		return 0, false
	}
	value := arguments[index]
	if !value.IsValid() {
		return 0, false
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(value.Int()), true
	default:
		return 0, false
	}
}

// capabilityFileModeArg returns the os.FileMode at index i for any unsigned-integer kind.
//
// Takes arguments ([]reflect.Value) which is the argument slice to read.
// Takes index (int) which is the position to read.
//
// Returns os.FileMode which is the argument's mode value.
// Returns bool which reports whether an unsigned integer was present at that index.
func capabilityFileModeArg(arguments []reflect.Value, index int) (os.FileMode, bool) {
	if index < 0 || index >= len(arguments) {
		return 0, false
	}
	value := arguments[index]
	if !value.IsValid() {
		return 0, false
	}
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return os.FileMode(safeconv.Uint64ToUint32(value.Uint())), true
	default:
		return 0, false
	}
}

// capabilityCommandArgv builds the argv for an exec gate: name in argv[0] followed by the
// trailing variadic string arguments (individually, or expanded from a single []string
// when the caller spread a slice).
//
// Takes name (string) which becomes argv[0].
// Takes arguments ([]reflect.Value) which hold the trailing variadic strings.
// Takes start (int) which is the index of the first variadic argument.
//
// Returns []string which is the assembled argv.
func capabilityCommandArgv(name string, arguments []reflect.Value, start int) []string {
	argv := []string{name}
	for index := start; index < len(arguments); index++ {
		value := arguments[index]
		if !value.IsValid() {
			continue
		}
		switch value.Kind() {
		case reflect.String:
			argv = append(argv, value.String())
		case reflect.Slice:
			if value.Type().Elem().Kind() == reflect.String {
				for element := range value.Len() {
					argv = append(argv, value.Index(element).String())
				}
			}
		default:
		}
	}
	return argv
}

// resolveNativeFunctionPath returns the dotted symbol identifier.
//
// Uses runtime.FuncForPC, which is suitable for cold-path use only. The result is cached
// on the call site so subsequent dispatches skip the lookup.
//
// Takes reflectedFunction (reflect.Value) which holds the function.
//
// Returns string which is the resolved symbol path, or "" when resolution fails.
func resolveNativeFunctionPath(reflectedFunction reflect.Value) string {
	if !reflectedFunction.IsValid() || reflectedFunction.Kind() != reflect.Func {
		return ""
	}
	fn := runtime.FuncForPC(reflectedFunction.Pointer())
	if fn == nil {
		return ""
	}
	return fn.Name()
}

// capabilityHookContext returns the context associated with the VM for hook consultation.
// Falls back to context.Background() so hook implementations can assume a non-nil
// context.
//
// Takes vm (*VM) which may carry a per-execution context.
//
// Returns context.Context that's safe to pass to hook methods.
func capabilityHookContext(vm *VM) context.Context {
	if vm == nil || vm.ctx == nil {
		return context.Background()
	}
	return vm.ctx
}
