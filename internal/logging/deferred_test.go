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
package logging_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/logging"
)

func TestDeferredReplaysThroughTheCallersLoggerOnce(t *testing.T) {
	t.Parallel()
	deferred := logging.NewDeferred()
	_, early := logging.From(deferred.Context())
	early.With(slog.String("provider", "stdlib")).WithGroup("package").Warn("rejected", slog.String("path", "os"))

	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))
	deferred.Replay(ctx)
	require.Contains(t, logged.String(), "msg=rejected provider=stdlib package.path=os")

	logged.Reset()
	deferred.Replay(ctx)
	require.Empty(t, logged.String(), "a record is reported once")
}

func TestDeferredCountsRecordsPastItsBound(t *testing.T) {
	t.Parallel()
	deferred := logging.NewDeferred()
	_, early := logging.From(deferred.Context())
	for range 256 + 3 {
		early.Warn("w")
	}
	var logged bytes.Buffer
	deferred.Replay(logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil))))
	require.Contains(t, logged.String(), "dropped=3")
}
