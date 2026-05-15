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

package facade_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestRestrictedEvaluation(t *testing.T) {
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{}, stdlib.Providers()...)
	if err != nil {
		t.Fatal(err)
	}
	result, err := interpreter.Eval(context.Background(), "6 * 7")
	if err != nil || string(result.Value) != "42" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = interpreter.EvalFile(context.Background(), `package main; func main() { println("hello") }`, "main")
	if err != nil || result.Output != "hello\n" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRestrictedDeniesAmbientAuthority(t *testing.T) {
	for _, source := range []string{
		`package main; import "os"; func main() { os.Exit(23) }`,
		`package main; import "os"; func main() { println(os.Environ()) }`,
		`package main; import ("os"; "io/fs"); func main() { fs.ReadFile(os.DirFS("/tmp"), "marker") }`,
		`package main; import "os"; func main() { os.OpenInRoot("/tmp", "marker") }`,
		`package main; import "net/http"; func main() { http.Get("https://example.com") }`,
		`package main; import "unsafe"; func main() { println(unsafe.Sizeof(1)) }`,
		`package main; func main() { go func() {}() }`,
		`package main; func main() { make(chan int) }`,
	} {
		t.Run(source, func(t *testing.T) {
			interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{}, stdlib.Providers()...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := interpreter.EvalFile(context.Background(), source, "main"); err == nil {
				t.Fatal("restricted operation succeeded")
			}
		})
	}
}

func TestRestrictedManifestIsPrivate(t *testing.T) {
	imports := []string{"math"}
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: imports}, stdlib.Providers()...)
	if err != nil {
		t.Fatal(err)
	}
	imports[0] = "os"

	result, err := interpreter.Eval(context.Background(), "import \"math\"\nmath.Sqrt(49)")
	if err != nil || string(result.Value) != "7" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := interpreter.Eval(context.Background(), "import \"math\"\nmath.Gamma(5)"); err != nil {
		t.Fatal("full math package unavailable:", err)
	}

	if _, err := interpreter.Eval(context.Background(), "import \"os\"\nos.Getpid()"); err == nil {
		t.Fatal("import outside the allowlist succeeded")
	}

	if _, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: []string{"definitely/not/a/package"}}, stdlib.Providers()...); !errors.Is(err, pipit.ErrInvalidRestrictedConfig) {
		t.Fatal(err)
	}
}

func TestRestrictedAllowlistedCapabilityDenied(t *testing.T) {

	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: []string{"os"}}, stdlib.Providers()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interpreter.Eval(context.Background(), "import \"os\"\nos.ReadFile(\"/etc/hostname\")"); err == nil {
		t.Fatal("allowlisted os.ReadFile reached the host under the default deny hook")
	}
}

func TestRestrictedLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		config pipit.RestrictedConfig
		source string
	}{
		{name: "source", config: pipit.RestrictedConfig{MaxSourceBytes: 3}, source: "12345"},
		{name: "cost", config: pipit.RestrictedConfig{CostBudget: 100}, source: "for {}"},
		{name: "allocation", config: pipit.RestrictedConfig{MaxAllocationElements: 4}, source: "make([]byte, 128)"},
		{name: "output", config: pipit.RestrictedConfig{MaxOutputBytes: 4}, source: `println("12345678")`},
		{name: "return", config: pipit.RestrictedConfig{MaxReturnBytes: 4}, source: `"12345678"`},
		{name: "encoded return", config: pipit.RestrictedConfig{MaxReturnBytes: 4}, source: `"\x00"`},
		{name: "encoded number", config: pipit.RestrictedConfig{MaxReturnBytes: 1}, source: "12345"},
		{name: "object", config: pipit.RestrictedConfig{}, source: "make([]byte, 4)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			interpreter, err := pipit.NewRestrictedInterpreter(test.config, stdlib.Providers()...)
			if err != nil {
				t.Fatal(err)
			}
			result, err := interpreter.Eval(context.Background(), test.source)
			if err == nil {
				t.Fatalf("limit not enforced: %+v", result)
			}
			if test.name == "output" && len(result.Output) > 4 {
				t.Fatal("output exceeded limit")
			}
		})
	}
}

func TestRestrictedConfigAndCancellation(t *testing.T) {
	for _, config := range []pipit.RestrictedConfig{
		{Timeout: -1}, {CostBudget: -1}, {MaxArenaBytes: -1}, {MaxSourceBytes: -1},
		{MaxOutputBytes: -1}, {MaxReturnBytes: -1}, {MaxStringBytes: -1},
		{MaxAllocationElements: -1}, {MaxCallDepth: -1},
	} {
		if _, err := pipit.NewRestrictedInterpreter(config, stdlib.Providers()...); !errors.Is(err, pipit.ErrInvalidRestrictedConfig) {
			t.Fatal(err)
		}
	}
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{}, stdlib.Providers()...)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := interpreter.Eval(ctx, "1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := interpreter.Eval(context.Background(), "private := 42; _ = private"); err != nil {
		t.Fatal(err)
	}
	if _, err := interpreter.Eval(context.Background(), "private"); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("state leaked: %v", err)
	}
}

func TestRestrictedDeadlineAndFreshRun(t *testing.T) {
	interpreter, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{
		Timeout:    20 * time.Millisecond,
		CostBudget: 100_000_000,
	}, stdlib.Providers()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interpreter.Eval(context.Background(), "for {}"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity lost: %v", err)
	}
	result, err := interpreter.Eval(context.Background(), "42")
	if err != nil || string(result.Value) != "42" {
		t.Fatalf("fresh run failed: result=%+v err=%v", result, err)
	}
}
