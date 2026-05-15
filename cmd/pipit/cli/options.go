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

package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	// defaultLogLevel is the level the CLI logs at when -log-level is absent. Warnings and
	// errors reach the operator; the library's debug and info records stay out of the way.
	defaultLogLevel = slog.LevelWarn

	// logLevelFlagName is the top-level flag that picks the logging level. There is no short
	// spelling: -v already means --version, and `pipit test -v` belongs to testing's own
	// flag set.
	logLevelFlagName = "log-level"
)

// Option configures a CLI invocation. Pass to Main or MainContext.
type Option func(*config)

// config holds the resolved configuration for a CLI invocation.
type config struct {
	// extras holds host symbol exports the caller wants merged in addition to pipit's
	// built-in stdlib and Pipit symbols.
	extras pipit.SymbolExports

	// logger receives the library's diagnostics for this invocation. The CLI is the process
	// root, so it owns the logger and hands it to the library, which puts it on the context
	// every deeper layer reads it back from.
	logger *slog.Logger
}

// newConfig builds a config from the supplied options.
//
// Takes opts ([]Option) which are applied in order to the new config.
//
// Returns *config which holds the resolved invocation configuration.
func newConfig(opts []Option) *config {
	config := &config{extras: nil, logger: nil}
	for _, opt := range opts {
		opt(config)
	}
	return config
}

// configKey is the context.Value key under which the cli config is stashed. Using a
// private struct type prevents collisions.
type configKey struct{}

// WithSymbols registers extra host symbols so scripts run by the CLI can import them,
// such as an extracted net/http package.
//
// Calling WithSymbols multiple times merges the supplied maps; later entries win on
// conflicting keys.
//
// Takes extras (pipit.SymbolExports) which holds the host symbol exports to merge into
// the invocation.
//
// Returns Option which merges extras into the CLI configuration.
func WithSymbols(extras pipit.SymbolExports) Option {
	return func(c *config) {
		if c.extras == nil {
			c.extras = pipit.SymbolExports{}
		}
		maps.Copy(c.extras, extras)
	}
}

// WithLogger installs the logger that receives the library's diagnostics. A logger set
// here takes precedence over the -log-level flag.
//
// Takes logger (*slog.Logger) which receives the library's log records.
//
// Returns Option which installs logger on the CLI configuration.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		c.logger = logger
	}
}

// ExtraSymbols returns any extra host symbols registered for the current invocation, or
// nil when none were supplied.
//
// Returns SymbolExports which holds the registered extras, or nil.
func ExtraSymbols(ctx context.Context) pipit.SymbolExports {
	return configFromContext(ctx).extras
}

// withConfig attaches config to ctx so subcommands can recover the caller's options
// without threading them through every signature.
//
// Takes config (*config) which carries the caller's resolved options.
//
// Returns Context which carries config for later retrieval.
func withConfig(ctx context.Context, config *config) context.Context {
	return context.WithValue(ctx, configKey{}, config)
}

// configFromContext returns the cli config attached to ctx, or an empty default if none
// was registered.
//
// Returns *config which is the attached config or an empty default.
func configFromContext(ctx context.Context) *config {
	if config, ok := ctx.Value(configKey{}).(*config); ok {
		return config
	}
	return &config{}
}

// loggerFromContext returns the logger configured for the current invocation.
//
// Returns *slog.Logger which is the invocation's logger, or nil when none was configured.
func loggerFromContext(ctx context.Context) *slog.Logger {
	return configFromContext(ctx).logger
}

// loggerOptions returns the interpreter options that hand the invocation's logger to the
// library.
//
// Use it for the commands that build an interpreter themselves rather than through
// runner.New, which reads the logger off its Limits instead.
//
// Returns []pipit.Option which is nil when the invocation configured no logger.
func loggerOptions(ctx context.Context) []pipit.Option {
	logger := loggerFromContext(ctx)
	if logger == nil {
		return nil
	}
	return []pipit.Option{pipit.WithLogger(logger)}
}

// parseLogLevel maps a -log-level value to its slog level.
//
// Takes name (string) which is the level name the operator supplied.
//
// Returns Level which is the matching slog level.
// Returns error when name is not one of debug, info, warn, or error.
func parseLogLevel(name string) (slog.Level, error) {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return defaultLogLevel, fmt.Errorf("unknown -%s value %q (want debug, info, warn, or error)", logLevelFlagName, name)
	}
}

// newStderrLogger builds the logger the CLI hands to the library: plain text records on
// stderr, filtered to the chosen level.
//
// Takes level (slog.Level) which is the lowest level that is reported.
// Takes stderr (io.Writer) which receives the records.
//
// Returns *slog.Logger which writes to stderr at level.
func newStderrLogger(level slog.Level, stderr io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level: level, AddSource: false, ReplaceAttr: nil}))
}

// allExports returns the union of the Go standard library and any extras supplied via
// WithSymbols on the current invocation.
//
// Returns SymbolExports which holds the merged built-in and extra symbol exports.
func allExports(ctx context.Context) pipit.SymbolExports {
	base := stdlib.Exports()
	maps.Copy(base, ExtraSymbols(ctx))
	return base
}
