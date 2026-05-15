//go:build linux && (amd64 || arm64)

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

package sandboxhost_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxhost"
)

func testIsolatedSessionNative(t *testing.T, config sandboxhost.IsolatedConfig) {
	t.Helper()
	t.Run("state and immutable grants", func(t *testing.T) {
		session, err := sandboxhost.NewIsolatedSession(context.Background(), config)
		if session != nil {
			t.Cleanup(func() { _ = session.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		config.Imports[0] = "os"
		for _, source := range []string{"import \"math\"", "var value = math.Sqrt(144)"} {
			if _, err := session.Submit(context.Background(), source); err != nil {
				t.Fatal(err)
			}
		}
		result, err := session.Submit(context.Background(), "value + 30")
		if err != nil || string(result.Value) != "42" {
			t.Fatalf("result=%+v error=%v diagnostics=%s", result, err, session.Diagnostics())
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Submit(context.Background(), "42"); !errors.Is(err, sandboxhost.ErrIsolatedSessionClosed) {
			t.Fatalf("closed worker reused: %v", err)
		}
	})
	config.Imports = nil
	t.Run("submission cancellation", func(t *testing.T) {
		session, err := sandboxhost.NewIsolatedSession(context.Background(), config)
		if session != nil {
			t.Cleanup(func() { _ = session.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		result, err := session.Submit(ctx, "for {}")
		if !errors.Is(err, context.DeadlineExceeded) || result.Value != nil {
			t.Fatalf("cancelled submission published result: %+v %v", result, err)
		}
		if _, err := session.Submit(context.Background(), "42"); !errors.Is(err, sandboxhost.ErrIsolatedSessionClosed) {
			t.Fatalf("cancelled worker reused: %v", err)
		}
	})
	t.Run("missing enforcement", func(t *testing.T) {
		config.LinuxCgroupParent = t.TempDir()
		session, err := sandboxhost.NewIsolatedSession(context.Background(), config)
		if session != nil {
			t.Cleanup(func() { _ = session.Close() })
		}
		if !errors.Is(err, sandboxhost.ErrIsolatedUnavailable) {
			t.Fatalf("missing boundary did not fail closed: %v", err)
		}
	})
}
