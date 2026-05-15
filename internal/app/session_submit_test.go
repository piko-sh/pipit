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

package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

func TestSessionEnforcesImportPolicyBeforeExecution(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		option Option
		code   string
	}{
		{name: "empty allowlist", option: WithImportAllowlist(), code: "import \"math\""},
		{name: "other allowlist", option: WithImportAllowlist("math"), code: "import \"os\""},
		{name: "denied", option: WithDeniedImports("math"), code: "import \"math\""},
		{name: "raw import", option: WithDeniedImports("math"), code: "import alias " + "`math`"},
		{name: "escaped import", option: WithDeniedImports("math"), code: "import \"\\x6dath\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			service := NewService(test.option, WithStderr(&output))
			symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
				"math": {"Value": reflect.ValueOf(42)},
				"os":   {"Value": reflect.ValueOf(42)},
			})
			symbols.SynthesiseAll()
			service.UseSymbols(symbols)
			session := service.NewSession()
			_, err := session.Submit(context.Background(), test.code+"\nvar value = func() int { println(42); return 1 }()")
			if err == nil || !strings.Contains(err.Error(), "not permitted") {
				t.Fatalf("session bypassed import policy: %v", err)
			}
			if output.Len() != 0 || len(session.imports) != 0 || session.submitCount != 0 {
				t.Fatal("denied submission executed or changed session history")
			}
		})
	}
}

func TestSessionEnforcesSubmissionSourceLimitBeforeParsing(t *testing.T) {
	t.Parallel()
	session := NewService(WithMaxSourceSize(32)).NewSession()
	_, err := session.Submit(context.Background(), strings.Repeat("?", 33))
	if !errors.Is(err, fault.ErrSourceSizeLimit) || session.rootFunction != nil {
		t.Fatalf("oversized source reached session preprocessing: %v", err)
	}
}

func TestSessionEnforcesCombinedSourceLimit(t *testing.T) {
	t.Parallel()
	session := NewService(WithMaxSourceSize(180)).NewSession()
	if _, err := session.Submit(context.Background(), "var first = \""+strings.Repeat("x", 60)+"\""); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Submit(context.Background(), "var second = \""+strings.Repeat("x", 60)+"\""); !errors.Is(err, fault.ErrSourceSizeLimit) {
		t.Fatalf("retained declarations escaped source budget: %v", err)
	}
	if session.submitCount != 1 || len(session.declaredNames) != 1 {
		t.Fatal("oversized combined source was committed")
	}
}

func TestSessionCancelledBeforePreprocessing(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := session.Submit(ctx, "var value = 42")
	if !errors.Is(err, context.Canceled) || session.rootFunction != nil || session.submitCount != 0 {
		t.Fatalf("cancelled submission allocated or committed session state: %v", err)
	}
}

func TestSessionCompilesAllCodeBeforeExecution(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		code string
	}{
		{name: "function", code: "var value = func() int { println(42); return 1 }()\nfunc denied() { go func() {}() }"},
		{name: "body", code: "var value = func() int { println(42); return 1 }()\ngo func() {}()"},
		{name: "init", code: "func init() { println(42) }\ngo func() {}()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			session := NewService(WithFeatures(policy.InterpFeaturesRestricted), WithStderr(&output)).NewSession()
			if _, err := session.Submit(context.Background(), test.code); err == nil {
				t.Fatal("forbidden goroutine compiled successfully")
			}
			if output.Len() != 0 {
				t.Fatalf("code executed before compilation completed: %q", output.String())
			}
			if session.submitCount != 0 || len(session.declaredNames) != 0 {
				t.Fatal("failed compilation committed session history")
			}
		})
	}
}

func TestSessionCompilationBarrierPreservesInitialisationOrder(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	session := NewService(WithFeatures(policy.InterpFeaturesRestricted), WithStderr(&output)).NewSession()
	code := "var value = initial()\nfunc initial() int { println(1); return 40 }\nfunc init() { println(2); value++ }\nprintln(3)\nfunc() int { return value + 1 }()"
	result, err := session.Submit(context.Background(), code)
	if err != nil || result != 42 || output.String() != "1\n2\n3\n" {
		t.Fatalf("initialisation order changed: result=%v output=%q error=%v", result, output.String(), err)
	}
	output.Reset()
	result, err = session.Submit(context.Background(), "value")
	if err != nil || result != 41 || output.Len() != 0 {
		t.Fatalf("initialisation repeated: result=%v output=%q error=%v", result, output.String(), err)
	}
}
