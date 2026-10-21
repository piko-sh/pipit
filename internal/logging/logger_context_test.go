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

func TestWithLoggerNilContextStartsFromBackground(t *testing.T) {
	t.Parallel()
	var nilCtx context.Context
	logger := newTestLogger()
	ctx := logging.WithLogger(nilCtx, logger)
	require.NotNil(t, ctx)
	_, got := logging.From(ctx)
	require.Same(t, logger, got)
}

func TestWithLoggerKeepsOtherValues(t *testing.T) {
	t.Parallel()
	type key struct{}
	base := context.WithValue(t.Context(), key{}, "marker")
	ctx := logging.WithLogger(base, newTestLogger())
	require.Equal(t, "marker", ctx.Value(key{}))
}

func TestWithLoggerInnermostWins(t *testing.T) {
	t.Parallel()
	outer := newTestLogger()
	inner := newTestLogger()
	ctx := logging.WithLogger(logging.WithLogger(t.Context(), outer), inner)
	_, got := logging.From(ctx)
	require.Same(t, inner, got)
}

func TestFromAllocatesNothingForABoundContext(t *testing.T) {
	ctx := logging.WithLogger(t.Context(), newTestLogger())
	allocations := testing.AllocsPerRun(100, func() {
		_, _ = logging.From(ctx)
	})
	require.Zero(t, allocations)
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

func TestWithLoggerRoundTripThroughFrom(t *testing.T) {
	t.Parallel()
	logger := newTestLogger()
	ctx := logging.WithLogger(t.Context(), logger)
	gotCtx, got := logging.From(ctx)
	require.Same(t, logger, got)
	require.Equal(t, ctx, gotCtx, "a context already carrying a logger is returned unchanged")
}

func TestFromBindsDefaultIntoReturnedContext(t *testing.T) {
	t.Parallel()
	var nilCtx context.Context
	cases := []struct {
		name string
		ctx  context.Context
	}{
		{name: "nil context", ctx: nilCtx},
		{name: "background", ctx: context.Background()},
		{name: "unrelated value", ctx: context.WithValue(t.Context(), struct{}{}, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, logger := logging.From(tc.ctx)
			require.NotNil(t, ctx)
			require.Same(t, slog.Default(), logger)
			_, again := logging.From(ctx)
			require.Same(t, logger, again, "the bound logger is shared down the chain")
		})
	}
}

func TestFromKeepsBoundLoggerAfterDefaultChanges(t *testing.T) {
	ctx, first := logging.From(t.Context())
	previous := slog.Default()
	replacement := newTestLogger()
	slog.SetDefault(replacement)
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, again := logging.From(ctx)
	require.Same(t, first, again, "a chain keeps the logger it resolved first")

	_, fresh := logging.From(t.Context())
	require.Same(t, replacement, fresh, "a new chain honours the changed default")
}

func TestWithLoggerNilLoggerReturnsContextUnchanged(t *testing.T) {
	t.Parallel()
	type key struct{}
	base := context.WithValue(t.Context(), key{}, "marker")
	require.Equal(t, base, logging.WithLogger(base, nil))
}

func TestWithFallbackLoggerYieldsToALoggerAlreadyInTheContext(t *testing.T) {
	t.Parallel()
	bound := slog.New(slog.DiscardHandler)
	fallback := slog.New(slog.DiscardHandler)

	ctx := logging.WithLogger(t.Context(), bound)
	require.Equal(t, ctx, logging.WithFallbackLogger(ctx, fallback), "a bound logger is kept and nothing is allocated")
	_, got := logging.From(logging.WithFallbackLogger(ctx, fallback))
	require.Same(t, bound, got)

	_, got = logging.From(logging.WithFallbackLogger(t.Context(), fallback))
	require.Same(t, fallback, got, "the fallback is used when the context carries none")

	require.Equal(t, ctx, logging.WithFallbackLogger(ctx, nil))
}
