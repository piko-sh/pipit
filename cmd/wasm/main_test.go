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

//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"
)

func TestBrowserRestrictedPolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		ok     bool
		output string
		value  string
	}{
		{name: "print", source: "package main\nfunc main() { println(42) }", ok: true, output: "42\n"},
		{name: "reviewed helper", source: "package main\nimport \"math\"\nfunc main() { println(int(math.Sqrt(49))) }", ok: true, output: "7\n"},
		{name: "unreviewed import", source: "package main\nimport \"os\"\nfunc main() { os.Exit(0) }"},
		{name: "global registry", source: "package main\nimport \"fmt\"\nfunc main() { fmt.Println(42) }"},
		{name: "goroutine", source: "package main\nfunc main() { go println(42) }"},
		{name: "channel", source: "package main\nfunc main() { channel := make(chan int); _ = channel }"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runSource(test.source, "untrusted")
			if result[resultKeyOK] != test.ok || result["stdout"] != test.output || result["result"] != test.value {
				t.Fatalf("unexpected result: %+v", result)
			}
			if !test.ok && result[resultKeyError] == "" {
				t.Fatal("denial has no error")
			}
		})
	}
}

func TestBrowserModesFailClosed(t *testing.T) {
	source := js.ValueOf("package main\nfunc main() {}")
	for _, arguments := range [][]js.Value{
		nil,
		{js.ValueOf(12)},
		{source, js.Null()},
		{source, js.ValueOf("untrusted")},
		{source, js.ValueOf(map[string]any{"mode": "untrustd"})},
		{source, js.ValueOf(map[string]any{"mode": ""})},
		{source, js.ValueOf(map[string]any{"mode": false})},
		{source, js.ValueOf(map[string]any{}), source},
	} {
		if _, _, err := parseRunArgs(arguments); err == nil {
			t.Fatalf("accepted invalid arguments: %v", arguments)
		}
	}
	for _, mode := range []string{"dev", "untrusted"} {
		if _, parsed, err := parseRunArgs([]js.Value{source, js.ValueOf(map[string]any{"mode": mode})}); err != nil || parsed != mode {
			t.Fatalf("valid mode rejected: mode=%q error=%v", parsed, err)
		}
	}
	if _, mode, err := parseRunArgs([]js.Value{source}); err != nil || mode != "dev" {
		t.Fatalf("omitted mode rejected: mode=%q error=%v", mode, err)
	}
	if result := runSource("package main\nfunc main() { println(42) }", "unknown"); result[resultKeyOK] != false || result["stdout"] != "" {
		t.Fatalf("unknown mode executed: %+v", result)
	}
}

func TestBrowserSourceBounds(t *testing.T) {
	source := strings.Repeat(" ", browserSourceLimit+1)
	for _, mode := range []string{"dev", "untrusted"} {
		if result := runSource(source, mode); result[resultKeyOK] != false || result[resultKeyError] != "browser source exceeds 1 MiB" {
			t.Fatalf("oversized source accepted: %+v", result)
		}
	}
	if result := formatSource(source); result[resultKeyOK] != false || result[resultKeyError] != "browser source exceeds 1 MiB" {
		t.Fatalf("oversized formatting accepted: %+v", result)
	}
}

func TestBrowserRejectsConcurrentOperations(t *testing.T) {
	browserBusy.Store(true)
	defer browserBusy.Store(false)
	for _, operation := range []func(js.Value, []js.Value) any{jsRun, jsFormat} {
		completed := make(chan js.Value, 1)
		callback := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
			completed <- arguments[0]
			return nil
		})
		promise, ok := operation(js.Undefined(), []js.Value{js.ValueOf("package main\nfunc main() { println(42) }")}).(js.Value)
		if !ok {
			callback.Release()
			t.Fatal("operation did not return a promise")
		}
		promise.Call("then", callback)
		result := <-completed
		callback.Release()
		if result.Get(resultKeyOK).Bool() || result.Get(resultKeyError).String() != "browser interpreter is busy" || !browserBusy.Load() {
			t.Fatal("concurrent operation executed or released another operation's admission")
		}
	}
}

func TestBrowserCaptureCombinedBound(t *testing.T) {
	buffer := js.Global().Get("Uint8Array").New(1024)
	js.CopyBytesToJS(buffer, []byte("abcdefgh"))
	fs := js.Global().Get("fs")
	original := fs.Get("write")
	var acknowledged int
	callback := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
		acknowledged = arguments[1].Int()
		return nil
	})
	defer callback.Release()
	stdout, stderr, truncated := captureOutput(5, func() {
		fs.Call("write", 1, buffer, 2, 3, js.Null(), callback)
		fs.Call("writeSync", 2, buffer)
		fs.Call("writeSync", 1, buffer)
	})
	if string(stdout) != "cde" || string(stderr) != "ab" || !truncated || acknowledged != 3 {
		t.Fatalf("capture escaped combined bound or ignored offsets: stdout=%q stderr=%q truncated=%v acknowledged=%d", stdout, stderr, truncated, acknowledged)
	}
	if !fs.Get("write").Equal(original) {
		t.Fatal("capture did not restore the host writer")
	}
}
