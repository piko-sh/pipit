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
	"os"
	"testing"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func evalString(t *testing.T, interpreter *pipit.Interpreter, source string) string {
	t.Helper()
	result, err := interpreter.Eval(context.Background(), source)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	text, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %v (%T)", result, result)
	}
	return text
}

func TestWithEnvOverlaysHostEnvironment(t *testing.T) {
	t.Setenv("PIPIT_TEST_HOST_VAR", "from-host")
	t.Setenv("PIPIT_TEST_SHADOWED", "host-value")
	overlay := map[string]string{"PIPIT_TEST_SHADOWED": "overlay-value", "PIPIT_TEST_ONLY": "yes"}
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithEnv(overlay), pipit.WithMaxExecutionTime(5*time.Second))
	overlay["PIPIT_TEST_ONLY"] = "mutated-after-construction"

	got := evalString(t, interpreter, `
import "os"
_, present := os.LookupEnv("PIPIT_TEST_HOST_VAR")
os.Setenv("PIPIT_TEST_WRITTEN", "script")
os.Unsetenv("PIPIT_TEST_HOST_VAR")
_, stillPresent := os.LookupEnv("PIPIT_TEST_HOST_VAR")
os.Getenv("PIPIT_TEST_SHADOWED") + "|" + os.Getenv("PIPIT_TEST_ONLY") + "|" + os.ExpandEnv("$PIPIT_TEST_WRITTEN") + "|" +
	map[bool]string{true: "seen", false: "hidden"}[present] + "|" + map[bool]string{true: "seen", false: "hidden"}[stillPresent]
`)
	if got != "overlay-value|yes|script|seen|hidden" {
		t.Fatalf("overlay semantics: %q", got)
	}
	if _, leaked := os.LookupEnv("PIPIT_TEST_WRITTEN"); leaked {
		t.Fatal("a script Setenv reached the host process")
	}
	if os.Getenv("PIPIT_TEST_HOST_VAR") != "from-host" {
		t.Fatal("a script Unsetenv reached the host process")
	}
}

func TestWithSealedEnvHidesHostEnvironment(t *testing.T) {
	t.Setenv("PIPIT_TEST_HOST_VAR", "from-host")
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithSealedEnv(map[string]string{"ONLY": "1"}), pipit.WithMaxExecutionTime(5*time.Second))
	got := evalString(t, interpreter, `
import (
	"os"
	"strings"
)
strings.Join(os.Environ(), ",") + "|" + os.Getenv("PIPIT_TEST_HOST_VAR") + "|" + os.Getenv("HOME")
`)
	if got != "ONLY=1||" {
		t.Fatalf("sealed environment leaked: %q", got)
	}
}

func TestWithArgsDrivesOsArgsAndFlag(t *testing.T) {
	t.Parallel()
	args := []string{"prog", "-name=pipit", "-count", "3", "rest", "of", "args"}
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithArgs(args), pipit.WithMaxExecutionTime(5*time.Second))
	args[1] = "-name=mutated"
	got := evalString(t, interpreter, `
import (
	"flag"
	"fmt"
	"os"
	"strings"
)
name := flag.String("name", "default", "who")
count := flag.Int("count", 0, "how many")
flag.Parse()
fmt.Sprintf("%s|%d|%s|%d|%s|%s", *name, *count, strings.Join(flag.Args(), " "), flag.NArg(), os.Args[0], strings.Join(os.Args[1:], " "))
`)
	if got != "pipit|3|rest of args|3|prog|-name=pipit -count 3 rest of args" {
		t.Fatalf("argument vector: %q", got)
	}
}

func TestWithoutArgsHostArgvIsUntouched(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	got := evalString(t, interpreter, `
import "os"
os.Args[0]
`)
	if got != os.Args[0] {
		t.Fatalf("expected the host argv without WithArgs, got %q", got)
	}
}
