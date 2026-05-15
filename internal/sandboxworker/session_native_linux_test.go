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

package sandboxworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxlinux"
)

func testNativeSession(t *testing.T, config sandboxlinux.WorkerConfig) {
	t.Helper()
	config.Lifetime = sessionLifetime
	config.OutputBytes = sessionOutputBytes
	for _, scenario := range []string{"retained state", "idle expiry", "output budget", "cancel active"} {
		t.Run(scenario, func(t *testing.T) {
			scenarioConfig := config
			if scenario == "output budget" {

				scenarioConfig.OutputBytes = 10
			}
			process, err := sandboxlinux.LaunchWorker(context.Background(), scenarioConfig)
			if process != nil {
				t.Cleanup(func() { _ = process.Close() })
			}
			if err != nil {
				t.Fatal(err)
			}
			session, err := OpenSession(process, Configuration{Profile: SessionProfile, Imports: nil})
			if err != nil {
				t.Fatalf("open: %v; diagnostics: %s", err, process.Output())
			}
			switch scenario {
			case "retained state":
				if _, err := session.Submit("var value = 40"); err != nil {
					t.Fatal(err)
				}
				result, err := session.Submit("value + 2")
				if err != nil || string(result.Value) != "42" {
					t.Fatalf("result=%+v error=%v", result, err)
				}
				if err := session.Close(); err != nil {
					t.Fatal(err)
				}
			case "idle expiry":
				session.expires = time.Now().Add(50 * time.Millisecond)
				if err := session.armIdle(); err != nil {
					t.Fatal(err)
				}
				if err := process.Wait(); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("idle process survived: %v", err)
				}
				if _, err := session.Submit("42"); err == nil {
					t.Fatal("expired worker was reused")
				}
			case "output budget":
				if _, err := session.Submit("print(\"abcd\")"); err != nil {
					t.Fatal(err)
				}
				if result, err := session.Submit("print(\"abcd\")"); !errors.Is(err, sandboxlinux.ErrWorkerOutput) || result.Value != nil {
					t.Fatalf("native output budget reset: %+v %v", result, err)
				}
			case "cancel active":
				gate := gateSessionRead(t, session)
				done := make(chan error, 1)
				go func() { _, err := session.Submit("for {}"); done <- err }()
				<-gate.entered
				_ = session.Close()
				if err := <-done; err == nil {
					t.Fatal("active close allowed success")
				}
			}
		})
	}
}
