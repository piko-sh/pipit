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
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/logging"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestContextWithLoggerRoundTrip(t *testing.T) {
	t.Parallel()
	logger := newTestLogger()
	ctx := logging.ContextWithLogger(context.Background(), logger)
	require.Same(t, logger, logging.LoggerFrom(ctx))
}

func TestContextWithLoggerNilContextStartsFromBackground(t *testing.T) {
	t.Parallel()
	var nilCtx context.Context
	logger := newTestLogger()
	ctx := logging.ContextWithLogger(nilCtx, logger)
	require.NotNil(t, ctx)
	require.Same(t, logger, logging.LoggerFrom(ctx))
}

func TestContextWithLoggerNilLoggerReturnsContextUnchanged(t *testing.T) {
	t.Parallel()
	type key struct{}
	base := context.WithValue(context.Background(), key{}, "marker")
	ctx := logging.ContextWithLogger(base, nil)
	require.Equal(t, base, ctx)
	require.Equal(t, "marker", ctx.Value(key{}))
}

func TestContextWithLoggerInnermostWins(t *testing.T) {
	t.Parallel()
	outer := newTestLogger()
	inner := newTestLogger()
	ctx := logging.ContextWithLogger(logging.ContextWithLogger(context.Background(), outer), inner)
	require.Same(t, inner, logging.LoggerFrom(ctx))
}

func TestLoggerFromFallsBackToDefault(t *testing.T) {
	t.Parallel()
	var nilCtx context.Context
	cases := []struct {
		name string
		ctx  context.Context
	}{
		{name: "nil context", ctx: nilCtx},
		{name: "background", ctx: context.Background()},
		{name: "unrelated value", ctx: context.WithValue(context.Background(), struct{}{}, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := logging.LoggerFrom(tc.ctx)
			require.NotNil(t, got)
			require.Same(t, slog.Default(), got)
		})
	}
}

func TestErrAttr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want slog.Attr
	}{
		{name: "nil error", err: nil, want: slog.String("error", "<nil>")},
		{name: "plain error", err: errors.New("boom"), want: slog.String("error", "boom")},
		{name: "wrapped error", err: errors.Join(errors.New("first"), errors.New("second")), want: slog.String("error", "first\nsecond")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := logging.ErrAttr(tc.err)
			require.True(t, tc.want.Equal(got), "want %v, got %v", tc.want, got)
			require.Equal(t, "error", got.Key)
		})
	}
}
