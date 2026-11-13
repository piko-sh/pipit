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

//go:build linux && (amd64 || arm64)

package sandboxworker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
			process, err := sandboxlinux.LaunchWorker(t.Context(), scenarioConfig)
			if process != nil {
				t.Cleanup(func() { _ = process.Close(t.Context()) })
			}
			require.NoError(t, err)
			session, err := OpenSession(t.Context(), process, Configuration{Profile: SessionProfile, Imports: nil})
			requireWorkerOK(t, err, process, "open")
			switch scenario {
			case "retained state":
				_, err = session.Submit(t.Context(), "var value = 40")
				require.NoError(t, err)
				result, err := session.Submit(t.Context(), "value + 2")
				require.NoError(t, err, "result=%+v", result)
				require.Equal(t, "42", string(result.Value))
				require.NoError(t, session.Close(t.Context()))
			case "idle expiry":
				session.expires = time.Now().Add(50 * time.Millisecond)
				require.NoError(t, session.armIdle())
				require.ErrorIs(t, process.Wait(), context.DeadlineExceeded, "idle process survived")
				_, err = session.Submit(t.Context(), "42")
				require.Error(t, err, "an expired worker is not reused")
			case "output budget":
				_, err = session.Submit(t.Context(), "print(\"abcd\")")
				require.NoError(t, err)
				result, err := session.Submit(t.Context(), "print(\"abcd\")")
				require.ErrorIs(t, err, sandboxlinux.ErrWorkerOutput, "the native output budget does not reset")
				require.Nil(t, result.Value)
			case "cancel active":
				gate := gateSessionRead(t, session)
				done := make(chan error, 1)
				go func() { _, err := session.Submit(t.Context(), "for {}"); done <- err }()
				<-gate.entered
				_ = session.Close(t.Context())
				require.Error(t, <-done, "active close allowed success")
			}
		})
	}
}
