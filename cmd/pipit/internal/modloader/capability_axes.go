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

// gateClassifier maps a native call's arguments to the capability axis and scope it
// exercises.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns string which is the capability axis.
// Returns string which is the scope within it.
type gateClassifier func(args []reflect.Value) (axis, scope string)

// nativeGateRules maps a dotted native symbol path to the classifier that decides which
// capability axis and scope the call exercises. A symbol absent from the map is ungated.
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
	"os.Open":                    pathClassifier(AxisFilesystemRead),
	"os.ReadFile":                pathClassifier(AxisFilesystemRead),
	"io/ioutil.ReadFile":         pathClassifier(AxisFilesystemRead),
	"os.OpenFile":                openFileClassifier,
	"os.Create":                  pathClassifier(AxisFilesystemWrite),
	"os.WriteFile":               pathClassifier(AxisFilesystemWrite),
	"io/ioutil.WriteFile":        pathClassifier(AxisFilesystemWrite),
	"os.Mkdir":                   pathClassifier(AxisFilesystemWrite),
	"os.MkdirAll":                pathClassifier(AxisFilesystemWrite),
	"os.Remove":                  pathClassifier(AxisFilesystemWrite),
	"os.RemoveAll":               pathClassifier(AxisFilesystemWrite),
	"os.Truncate":                pathClassifier(AxisFilesystemWrite),
	"os.Rename":                  renameClassifier,
	"os.Symlink":                 renameClassifier,
	"os.Link":                    renameClassifier,
	"os.Getenv":                  nameClassifier(AxisEnvRead),
	"os.LookupEnv":               nameClassifier(AxisEnvRead),
	"os.Environ":                 func(_ []reflect.Value) (string, string) { return AxisEnvRead, "*" },
	"os.Setenv":                  nameClassifier(AxisEnvWrite),
	"os.Unsetenv":                nameClassifier(AxisEnvWrite),
	"os.Clearenv":                func(_ []reflect.Value) (string, string) { return AxisEnvWrite, "*" },
	"os/exec.Command":            nameClassifier(AxisExec),
	"os/exec.LookPath":           nameClassifier(AxisExec),
	"os/exec.CommandContext":     commandContextClassifier,
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

// classifyNativeCall resolves the axis and scope for a native call, if it is gated.
//
// Takes fnPath (string) which is the dotted native symbol path.
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns axis (string) which is the capability axis.
// Returns scope (string) which narrows the axis.
// Returns gated (bool) which is true when the call is a gated entry point.
func classifyNativeCall(fnPath string, args []reflect.Value) (axis, scope string, gated bool) {
	rule, ok := nativeGateRules[fnPath]
	if !ok {
		return "", "", false
	}
	axis, scope = rule(args)
	return axis, scope, true
}

// dialClassifier reads network and address from a net.Dial-shaped call.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns axis (string) which is AxisNetworkDial.
// Returns scope (string) which is the "network:address" pair.
func dialClassifier(args []reflect.Value) (axis, scope string) {
	network := argString(args, 0)
	address := argString(args, 1)
	return AxisNetworkDial, joinScope(network, address)
}

// listenClassifier reads network and address from a net.Listen-shaped call.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns axis (string) which is AxisNetworkListen.
// Returns scope (string) which is the "network:address" pair.
func listenClassifier(args []reflect.Value) (axis, scope string) {
	network := argString(args, 0)
	address := argString(args, 1)
	return AxisNetworkListen, joinScope(network, address)
}

// urlClassifier builds a classifier for calls whose first argument is a URL or address
// string.
//
// Takes axis (string) which is the axis to report.
//
// Returns gateClassifier which reads the first string argument as the scope.
func urlClassifier(axis string) gateClassifier {
	return func(args []reflect.Value) (string, string) {
		return axis, argString(args, 0)
	}
}

// pathClassifier builds a classifier for calls whose first argument is a filesystem path.
//
// Takes axis (string) which is the axis to report.
//
// Returns gateClassifier which reads the first string argument as the scope.
func pathClassifier(axis string) gateClassifier {
	return func(args []reflect.Value) (string, string) {
		return axis, argString(args, 0)
	}
}

// nameClassifier builds a classifier for calls whose first argument names a variable or
// command.
//
// Takes axis (string) which is the axis to report.
//
// Returns gateClassifier which reads the first string argument as the scope.
func nameClassifier(axis string) gateClassifier {
	return func(args []reflect.Value) (string, string) {
		return axis, argString(args, 0)
	}
}

// renameClassifier reads the source and destination paths from a two-path write call.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns axis (string) which is AxisFilesystemWrite.
// Returns scope (string) which is the "old -> new" pair.
func renameClassifier(args []reflect.Value) (axis, scope string) {
	from := argString(args, 0)
	to := argString(args, 1)
	if to == "" {
		return AxisFilesystemWrite, from
	}
	return AxisFilesystemWrite, from + " -> " + to
}

// openFileClassifier maps os.OpenFile to a read or write axis by inspecting its flag
// argument.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns axis (string) which is AxisFilesystemRead or AxisFilesystemWrite.
// Returns scope (string) which is the opened path.
func openFileClassifier(args []reflect.Value) (axis, scope string) {
	axis = AxisFilesystemRead
	flag := argInt(args, 1)
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		axis = AxisFilesystemWrite
	}
	return axis, argString(args, 0)
}

// commandContextClassifier maps os/exec.CommandContext, whose command name is the second
// argument after the context.
//
// Takes args ([]reflect.Value) which are the reflected call arguments.
//
// Returns axis (string) which is AxisExec.
// Returns scope (string) which is the command name.
func commandContextClassifier(args []reflect.Value) (axis, scope string) {
	return AxisExec, argString(args, 1)
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
