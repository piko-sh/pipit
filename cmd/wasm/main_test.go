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

	"github.com/stretchr/testify/require"
)

func TestBrowserRestrictedPolicy(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantOutput string
		wantOK     bool
	}{
		{name: "print", source: "package main\nfunc main() { println(42) }", wantOutput: "42\n", wantOK: true},
		{name: "reviewed helper", source: "package main\nimport \"math\"\nfunc main() { println(int(math.Sqrt(49))) }", wantOutput: "7\n", wantOK: true},
		{name: "unreviewed import", source: "package main\nimport \"os\"\nfunc main() { os.Exit(0) }"},
		{name: "global registry", source: "package main\nimport \"fmt\"\nfunc main() { fmt.Println(42) }"},
		{name: "goroutine", source: "package main\nfunc main() { go println(42) }"},
		{name: "channel", source: "package main\nfunc main() { channel := make(chan int); _ = channel }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runSource(tt.source, "untrusted")
			require.Subset(t, result, map[string]any{resultKeyOK: tt.wantOK, "stdout": tt.wantOutput, "result": ""})
			if !tt.wantOK {
				require.NotEmpty(t, result[resultKeyError], "a denial carries an error")
			}
		})
	}
}

func TestBrowserModesFailClosed(t *testing.T) {
	t.Parallel()
	source := js.ValueOf("package main\nfunc main() {}")
	tests := []struct {
		name      string
		arguments []js.Value
	}{
		{name: "no arguments", arguments: nil},
		{name: "source is not a string", arguments: []js.Value{js.ValueOf(12)}},
		{name: "null options", arguments: []js.Value{source, js.Null()}},
		{name: "options are a string", arguments: []js.Value{source, js.ValueOf("untrusted")}},
		{name: "misspelt mode", arguments: []js.Value{source, js.ValueOf(map[string]any{"mode": "untrustd"})}},
		{name: "empty mode", arguments: []js.Value{source, js.ValueOf(map[string]any{"mode": ""})}},
		{name: "mode is not a string", arguments: []js.Value{source, js.ValueOf(map[string]any{"mode": false})}},
		{name: "extra argument", arguments: []js.Value{source, js.ValueOf(map[string]any{}), source}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseRunArgs(tt.arguments)
			require.Error(t, err)
		})
	}
}

func TestBrowserModesAcceptValidModes(t *testing.T) {
	t.Parallel()
	source := js.ValueOf("package main\nfunc main() {}")
	tests := []struct {
		name      string
		arguments []js.Value
		wantMode  string
	}{
		{name: "dev", arguments: []js.Value{source, js.ValueOf(map[string]any{"mode": "dev"})}, wantMode: "dev"},
		{name: "untrusted", arguments: []js.Value{source, js.ValueOf(map[string]any{"mode": "untrusted"})}, wantMode: "untrusted"},
		{name: "omitted mode means dev", arguments: []js.Value{source}, wantMode: "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, mode, err := parseRunArgs(tt.arguments)
			require.NoError(t, err)
			require.Equal(t, tt.wantMode, mode)
		})
	}
}

func TestBrowserRefusesUnknownMode(t *testing.T) {
	result := runSource("package main\nfunc main() { println(42) }", "unknown")
	require.Subset(t, result, map[string]any{resultKeyOK: false, "stdout": ""}, "an unknown mode runs nothing")
}

func TestBrowserSourceBounds(t *testing.T) {
	oversized := strings.Repeat(" ", browserSourceLimit+1)
	tests := []struct {
		name      string
		operation func(string) map[string]any
	}{
		{name: "dev run", operation: func(source string) map[string]any { return runSource(source, "dev") }},
		{name: "untrusted run", operation: func(source string) map[string]any { return runSource(source, "untrusted") }},
		{name: "format", operation: formatSource},
		{name: "disassemble", operation: disassembleSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.operation(oversized)
			require.Subset(t, result, map[string]any{resultKeyOK: false, resultKeyError: "browser source exceeds 1 MiB"})
		})
	}
}

func TestBrowserDisassemble(t *testing.T) {
	result := disassembleSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n")
	require.Subset(t, result, map[string]any{resultKeyOK: true, resultKeyError: "", "truncated": false})
	assembly, ok := result["assembly"].(string)
	require.True(t, ok, "the result holds the listing")
	for _, want := range []string{"; pkasm - pipit bytecode assembly", "; function main", "; main.go:6", "CALL_NATIVE"} {
		require.Contains(t, assembly, want)
	}

	failed := disassembleSource("package main\nfunc main() { undefined() }")
	require.Subset(t, failed, map[string]any{resultKeyOK: false, "assembly": ""})
	require.NotEmpty(t, failed[resultKeyError], "a compile error is reported")
}

func TestClipListing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		listing       string
		want          string
		limit         int
		wantTruncated bool
	}{
		{name: "fits the limit", listing: "a\nb\n", limit: 4, want: "a\nb\n"},
		{name: "cut at a line end", listing: "a\nbc\n", limit: 4, want: "a\n", wantTruncated: true},
		{name: "no complete line fits", listing: "abcdef", limit: 4, want: "", wantTruncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, truncated := clipListing(tt.listing, tt.limit)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantTruncated, truncated)
		})
	}
}

func TestBrowserRejectsConcurrentOperations(t *testing.T) {
	browserBusy.Store(true)
	t.Cleanup(func() { browserBusy.Store(false) })
	tests := []struct {
		name      string
		operation func(js.Value, []js.Value) any
	}{
		{name: "run", operation: jsRun},
		{name: "format", operation: jsFormat},
		{name: "disassemble", operation: jsDisassemble},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completed := make(chan js.Value, 1)
			callback := js.FuncOf(func(_ js.Value, arguments []js.Value) any {
				completed <- arguments[0]
				return nil
			})
			t.Cleanup(callback.Release)
			promise, ok := tt.operation(js.Undefined(), []js.Value{js.ValueOf("package main\nfunc main() { println(42) }")}).(js.Value)
			require.True(t, ok, "the operation returns a promise")
			promise.Call("then", callback)
			result := <-completed
			require.False(t, result.Get(resultKeyOK).Bool(), "a concurrent operation is refused")
			require.Equal(t, "browser interpreter is busy", result.Get(resultKeyError).String())
			require.True(t, browserBusy.Load(), "a refused operation keeps the other operation's admission")
		})
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
	require.Equal(t, "cde", string(stdout), "stdout honours the write offsets")
	require.Equal(t, "ab", string(stderr), "stderr gets what is left of the combined bound")
	require.True(t, truncated, "the combined bound truncates the capture")
	require.Equal(t, 3, acknowledged, "the asynchronous write acknowledges its bytes")
	require.True(t, fs.Get("write").Equal(original), "the capture restores the host writer")
}
