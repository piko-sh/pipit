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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	// AxisNetworkDial gates outbound network connections such as net.Dial and http.Get.
	AxisNetworkDial = "network.dial"

	// AxisNetworkListen gates inbound listeners such as net.Listen and http.ListenAndServe.
	AxisNetworkListen = "network.listen"

	// AxisFilesystemRead gates read-side file operations such as os.Open and os.ReadFile.
	AxisFilesystemRead = "filesystem.read"

	// AxisFilesystemWrite gates write-side file operations such as os.WriteFile and
	// os.Remove.
	AxisFilesystemWrite = "filesystem.write"

	// AxisExec gates process execution through os/exec.
	AxisExec = "exec"

	// AxisSubprocess gates non-exec process spawns.
	AxisSubprocess = "subprocess"

	// AxisEnvRead gates environment reads such as os.Getenv and os.LookupEnv.
	AxisEnvRead = "env.read"

	// AxisEnvWrite gates environment mutations such as os.Setenv and os.Unsetenv.
	AxisEnvWrite = "env.write"
)

// allAxes lists every capability axis the CLI can gate, in canonical order.
var allAxes = []string{
	AxisNetworkDial, AxisNetworkListen,
	AxisFilesystemRead, AxisFilesystemWrite,
	AxisExec, AxisSubprocess,
	AxisEnvRead, AxisEnvWrite,
}

// capabilityClaim is one axis and scope a native call exercises.
type capabilityClaim struct {
	// axis names the capability axis, such as AxisFilesystemWrite.
	axis string

	// scope narrows the claim within the axis, such as a path or an address.
	scope string
}

// gateClassifier maps a native call's arguments to the capability claims it exercises.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which lists each axis and scope the call needs, in the order
// they are consulted.
type gateClassifier func(args []reflect.Value) []capabilityClaim

// nativeGateRules maps a registered native symbol to the classifier that decides which
// capability claims the call exercises. A symbol absent from the map is ungated.
var nativeGateRules = map[string]gateClassifier{
	"net.Dial":                   dialClassifier,
	"net.DialTimeout":            dialClassifier,
	"crypto/tls.Dial":            dialClassifier,
	"net/http.Get":               urlClassifier(AxisNetworkDial),
	"net/http.Post":              urlClassifier(AxisNetworkDial),
	"net/http.Head":              urlClassifier(AxisNetworkDial),
	"net/http.PostForm":          urlClassifier(AxisNetworkDial),
	"net.Listen":                 listenClassifier,
	"net/http.ListenAndServe":    urlClassifier(AxisNetworkListen),
	"net/http.ListenAndServeTLS": urlClassifier(AxisNetworkListen),

	"os.Open":                    pathClassifier(AxisFilesystemRead, 0),
	"os.ReadFile":                pathClassifier(AxisFilesystemRead, 0),
	"os.Stat":                    pathClassifier(AxisFilesystemRead, 0),
	"os.Lstat":                   pathClassifier(AxisFilesystemRead, 0),
	"os.ReadDir":                 pathClassifier(AxisFilesystemRead, 0),
	"os.Readlink":                pathClassifier(AxisFilesystemRead, 0),
	"io/ioutil.ReadFile":         pathClassifier(AxisFilesystemRead, 0),
	"io/ioutil.ReadDir":          pathClassifier(AxisFilesystemRead, 0),
	"path/filepath.Walk":         pathClassifier(AxisFilesystemRead, 0),
	"path/filepath.WalkDir":      pathClassifier(AxisFilesystemRead, 0),
	"path/filepath.Glob":         pathClassifier(AxisFilesystemRead, 0),
	"path/filepath.EvalSymlinks": pathClassifier(AxisFilesystemRead, 0),
	"archive/zip.OpenReader":     pathClassifier(AxisFilesystemRead, 0),
	"crypto/tls.LoadX509KeyPair": pathsClassifier(AxisFilesystemRead, 0, 1),
	"debug/buildinfo.ReadFile":   pathClassifier(AxisFilesystemRead, 0),
	"debug/elf.Open":             pathClassifier(AxisFilesystemRead, 0),
	"debug/macho.Open":           pathClassifier(AxisFilesystemRead, 0),
	"debug/pe.Open":              pathClassifier(AxisFilesystemRead, 0),
	"go/build.ImportDir":         pathClassifier(AxisFilesystemRead, 0),
	"go/parser.ParseDir":         pathClassifier(AxisFilesystemRead, 1),
	"html/template.ParseGlob":    pathClassifier(AxisFilesystemRead, 0),
	"text/template.ParseGlob":    pathClassifier(AxisFilesystemRead, 0),
	"net/http.ServeFile":         pathClassifier(AxisFilesystemRead, 2),
	"os.OpenFile":                openFileClassifier,

	"os.Create":           pathClassifier(AxisFilesystemWrite, 0),
	"os.WriteFile":        pathClassifier(AxisFilesystemWrite, 0),
	"io/ioutil.WriteFile": pathClassifier(AxisFilesystemWrite, 0),
	"os.Mkdir":            pathClassifier(AxisFilesystemWrite, 0),
	"os.MkdirAll":         pathClassifier(AxisFilesystemWrite, 0),
	"os.Remove":           pathClassifier(AxisFilesystemWrite, 0),
	"os.RemoveAll":        pathClassifier(AxisFilesystemWrite, 0),
	"os.Truncate":         pathClassifier(AxisFilesystemWrite, 0),
	"os.Chmod":            pathClassifier(AxisFilesystemWrite, 0),
	"os.Chown":            pathClassifier(AxisFilesystemWrite, 0),
	"os.Lchown":           pathClassifier(AxisFilesystemWrite, 0),
	"os.Chtimes":          pathClassifier(AxisFilesystemWrite, 0),
	"os.CopyFS":           pathClassifier(AxisFilesystemWrite, 0),
	"os.CreateTemp":       temporaryDirectoryClassifier,
	"os.MkdirTemp":        temporaryDirectoryClassifier,
	"io/ioutil.TempFile":  temporaryDirectoryClassifier,
	"io/ioutil.TempDir":   temporaryDirectoryClassifier,
	"os.Rename":           pathsClassifier(AxisFilesystemWrite, 0, 1),
	"os.Link":             pathsClassifier(AxisFilesystemWrite, 0, 1),
	"os.Symlink":          symlinkClassifier,

	"os.Getenv":              nameClassifier(AxisEnvRead),
	"os.LookupEnv":           nameClassifier(AxisEnvRead),
	"os.Environ":             fixedClassifier(AxisEnvRead, "*"),
	"os.Setenv":              nameClassifier(AxisEnvWrite),
	"os.Unsetenv":            nameClassifier(AxisEnvWrite),
	"os.Clearenv":            fixedClassifier(AxisEnvWrite, "*"),
	"os/exec.Command":        nameClassifier(AxisExec),
	"os/exec.LookPath":       nameClassifier(AxisExec),
	"os/exec.CommandContext": commandContextClassifier,
}

// ParseGateSpec parses a --gate flag value into the list of axes to gate.
//
// Takes spec (string) which is the raw --gate flag value.
//
// Returns []string which are the resolved axes to gate, deduplicated in canonical order.
// Returns error when a token is not a recognised group or axis.
func ParseGateSpec(spec string) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	seen := make(map[string]bool)
	add := func(axes ...string) {
		for _, axis := range axes {
			seen[axis] = true
		}
	}

	for token := range strings.SplitSeq(spec, ",") {
		token = strings.TrimSpace(strings.ToLower(token))
		if token == "" {
			continue
		}
		switch token {
		case "all":
			add(allAxes...)
		case "network", "net":
			add(AxisNetworkDial, AxisNetworkListen)
		case "disk", "fs", "file", "filesystem":
			add(AxisFilesystemRead, AxisFilesystemWrite)
		case "exec", "process", "proc", "subprocess":
			add(AxisExec, AxisSubprocess)
		case "env", "environment":
			add(AxisEnvRead, AxisEnvWrite)
		default:
			return nil, fmt.Errorf("unknown gate %q (want: all, network, disk, exec, env)", token)
		}
	}

	resolved := make([]string, 0, len(seen))
	for _, axis := range allAxes {
		if seen[axis] {
			resolved = append(resolved, axis)
		}
	}
	return resolved, nil
}

// classifyNativeCall resolves the claims a native call exercises, if it is gated.
//
// Takes fnPath (string) which is the registered native symbol.
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which lists the claims, in the order they are consulted.
// Returns bool which is true when the call is a gated entry point.
func classifyNativeCall(fnPath string, args []reflect.Value) ([]capabilityClaim, bool) {
	rule, ok := nativeGateRules[fnPath]
	if !ok {
		return nil, false
	}
	return rule(args), true
}

// dialClassifier reads network and address from a net.Dial-shaped call.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which holds one AxisNetworkDial claim scoped to
// "network:address".
func dialClassifier(args []reflect.Value) []capabilityClaim {
	return []capabilityClaim{{axis: AxisNetworkDial, scope: joinScope(argString(args, 0), argString(args, 1))}}
}

// listenClassifier reads network and address from a net.Listen-shaped call.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which holds one AxisNetworkListen claim scoped to
// "network:address".
func listenClassifier(args []reflect.Value) []capabilityClaim {
	return []capabilityClaim{{axis: AxisNetworkListen, scope: joinScope(argString(args, 0), argString(args, 1))}}
}

// urlClassifier builds a classifier for calls whose first argument is a URL or address
// string.
//
// Takes axis (string) which is the axis to report.
//
// Returns gateClassifier which reads the first string argument as the scope.
func urlClassifier(axis string) gateClassifier {
	return func(args []reflect.Value) []capabilityClaim {
		return []capabilityClaim{{axis: axis, scope: argString(args, 0)}}
	}
}

// fixedClassifier builds a classifier whose claim does not depend on the arguments.
//
// Takes axis (string) which is the axis to report.
// Takes scope (string) which is the scope to report.
//
// Returns gateClassifier which always reports the one claim.
func fixedClassifier(axis, scope string) gateClassifier {
	return func(_ []reflect.Value) []capabilityClaim {
		return []capabilityClaim{{axis: axis, scope: scope}}
	}
}

// pathClassifier builds a classifier for calls with one filesystem path argument.
//
// Takes axis (string) which is the axis to report.
// Takes index (int) which is the position of the path argument.
//
// Returns gateClassifier which reads that argument as the scope.
func pathClassifier(axis string, index int) gateClassifier {
	return pathsClassifier(axis, index)
}

// pathsClassifier builds a classifier for calls with several filesystem path arguments,
// such as os.Rename, and reports one claim per path.
//
// Takes axis (string) which is the axis to report.
// Takes indices (...int) which are the positions of the path arguments.
//
// Returns gateClassifier which reports one claim per path, in argument order.
func pathsClassifier(axis string, indices ...int) gateClassifier {
	return func(args []reflect.Value) []capabilityClaim {
		claims := make([]capabilityClaim, 0, len(indices))
		for _, index := range indices {
			claims = append(claims, capabilityClaim{axis: axis, scope: argString(args, index)})
		}
		return claims
	}
}

// nameClassifier builds a classifier for calls whose first argument names a variable or
// command.
//
// Takes axis (string) which is the axis to report.
//
// Returns gateClassifier which reads the first string argument as the scope.
func nameClassifier(axis string) gateClassifier {
	return func(args []reflect.Value) []capabilityClaim {
		return []capabilityClaim{{axis: axis, scope: argString(args, 0)}}
	}
}

// symlinkClassifier reads os.Symlink(target, name).
//
// The new name is a write. The target is a read, resolved against the new name's
// directory when it is relative, because that is where the link will point.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which holds the write claim and then the read claim.
func symlinkClassifier(args []reflect.Value) []capabilityClaim {
	target := argString(args, 0)
	name := argString(args, 1)
	if !filepath.IsAbs(target) && name != "" {
		target = filepath.Join(filepath.Dir(name), target)
	}
	return []capabilityClaim{
		{axis: AxisFilesystemWrite, scope: name},
		{axis: AxisFilesystemRead, scope: target},
	}
}

// temporaryDirectoryClassifier reads the directory a temporary file or directory is
// created in, which is os.TempDir() when the script passes "".
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which holds one AxisFilesystemWrite claim on the directory.
func temporaryDirectoryClassifier(args []reflect.Value) []capabilityClaim {
	directory := argString(args, 0)
	if directory == "" {
		directory = os.TempDir()
	}
	return []capabilityClaim{{axis: AxisFilesystemWrite, scope: directory}}
}

// openFileClassifier maps os.OpenFile to a read or write axis by inspecting its flag
// argument.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which holds one claim on the opened path.
func openFileClassifier(args []reflect.Value) []capabilityClaim {
	axis := AxisFilesystemRead
	flag := argInt(args, 1)
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		axis = AxisFilesystemWrite
	}
	return []capabilityClaim{{axis: axis, scope: argString(args, 0)}}
}

// commandContextClassifier maps os/exec.CommandContext, whose command name is the second
// argument after the context.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns []capabilityClaim which holds one AxisExec claim on the command name.
func commandContextClassifier(args []reflect.Value) []capabilityClaim {
	return []capabilityClaim{{axis: AxisExec, scope: argString(args, 1)}}
}

// joinScope formats a network and address into the canonical "network:address" scope.
//
// Takes network (string) which is the transport.
// Takes address (string) which is the host and port.
//
// Returns string which is the combined scope, or just one part when the other is empty.
func joinScope(network, address string) string {
	switch {
	case network == "" && address == "":
		return ""
	case network == "":
		return address
	case address == "":
		return network
	default:
		return network + ":" + address
	}
}

// argString returns the i-th argument as a string when it is a string value.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
// Takes index (int) which is the argument position.
//
// Returns string which is the argument value, or empty when absent or not a string.
func argString(args []reflect.Value, index int) string {
	if index < 0 || index >= len(args) {
		return ""
	}
	value := args[index]
	if !value.IsValid() || value.Kind() != reflect.String {
		return ""
	}
	return value.String()
}

// argInt returns the i-th argument as an int when it is an integer value.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
// Takes index (int) which is the argument position.
//
// Returns int which is the argument value, or zero when absent or not an integer.
func argInt(args []reflect.Value, index int) int {
	if index < 0 || index >= len(args) {
		return 0
	}
	value := args[index]
	if !value.IsValid() {
		return 0
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(value.Int())
	default:
		return 0
	}
}
