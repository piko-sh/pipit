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

package logging

import (
	"context"
	"log/slog"
)

// loggerContextKey keys the logger a host attaches to a context.
type loggerContextKey struct{}

// ContextWithLogger returns a copy of ctx carrying logger.
//
// The interpreter logs only warnings and errors, and it resolves the logger per call
// rather than capturing one at construction, so a host can scope a logger to a single
// request or evaluation. Passing a nil logger returns ctx unchanged.
//
// Takes logger (*slog.Logger) which the interpreter should use for work performed under
// the returned context.
//
// Returns context.Context which carries the logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if logger == nil {
		return ctx
	}

	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// LoggerFrom returns the logger attached to ctx, falling back to the process default.
//
// Resolution happens at the call site rather than at construction so that a host calling
// slog.SetDefault() after the interpreter starts is still honoured.
//
// Returns *slog.Logger which is never nil.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}

	return slog.Default()
}

// ErrAttr builds the structured attribute the interpreter uses to report an error.
//
// Takes err (error) which may be nil.
//
// Returns slog.Attr which holds the error message under the key "error".
func ErrAttr(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}

	return slog.String("error", err.Error())
}
