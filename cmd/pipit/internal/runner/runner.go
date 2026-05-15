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

// Package runner builds Interpreter handles from a CLI-friendly Limits struct, so each
// command does not re-derive option wiring.
package runner

import (
	"log/slog"
	"os"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	// goDispatchEnv names the environment variable that forces the Go dispatch loop.
	goDispatchEnv = "PIPIT_GO_DISPATCH"

	// maxLimitOptions is the number of interpreter options Limits can produce, used to
	// presize the slice Options returns.
	maxLimitOptions = 9
)

// Limits collects the sandbox knobs every command exposes as flags. Zero values select
// interpreter defaults.
type Limits struct {
	// BytecodeStore wires a directory store for SaveCompiled / LoadCompiled. Empty disables
	// bytecode persistence.
	BytecodeStore pipit.BytecodeStorePort

	// Logger receives the library's diagnostics. Nil leaves the library on the process
	// default logger.
	Logger *slog.Logger

	// Symbols registers extra host packages so scripts can import them. Nil leaves the
	// interpreter with the default stdlib + Pipit symbol set.
	Symbols pipit.SymbolExports

	// Timeout caps wall-clock time per evaluation.
	Timeout time.Duration

	// MaxAlloc caps the element count of a single allocation.
	MaxAlloc int

	// MaxCallDepth caps call-stack depth.
	MaxCallDepth int

	// MaxOutputSize caps total bytes print/println may write.
	MaxOutputSize int

	// CostBudget enables instruction cost metering with this budget. Zero disables metering.
	CostBudget int64

	// MaxGoroutines caps concurrent goroutines spawned by scripts.
	MaxGoroutines int32
}

// Options converts Limits to a slice of pipit.Option values.
//
// Returns []Option which holds the enabled interpreter options.
func (limits Limits) Options() []pipit.Option {
	options := make([]pipit.Option, 0, maxLimitOptions)
	if limits.Timeout > 0 {
		options = append(options, pipit.WithMaxExecutionTime(limits.Timeout))
	}
	if limits.MaxAlloc > 0 {
		options = append(options, pipit.WithMaxAllocSize(limits.MaxAlloc))
	}
	if limits.MaxGoroutines > 0 {
		options = append(options, pipit.WithMaxGoroutines(limits.MaxGoroutines))
	}
	if limits.MaxCallDepth > 0 {
		options = append(options, pipit.WithMaxCallDepth(limits.MaxCallDepth))
	}
	if limits.MaxOutputSize > 0 {
		options = append(options, pipit.WithMaxOutputSize(limits.MaxOutputSize))
	}
	if limits.CostBudget > 0 {
		options = append(options, pipit.WithCostBudget(limits.CostBudget))
	}
	if limits.BytecodeStore != nil {
		options = append(options, pipit.WithBytecodeStore(limits.BytecodeStore))
	}
	if limits.Logger != nil {
		options = append(options, pipit.WithLogger(limits.Logger))
	}
	return options
}

// New constructs an Interpreter from the given Limits and extra options.
//
// The Go standard library is registered first, so Limits.Symbols shadows it rather than
// the other way round. The CLI always offers the whole library: a user running a script
// has not asked to trade packages for binary size.
//
// Takes limits (Limits) which supplies the sandbox option wiring.
// Takes extras (...pipit.Option) which layers on options like WithDebugger.
//
// Returns *Interpreter which is configured with the derived options.
func New(limits Limits, extras ...pipit.Option) *pipit.Interpreter {
	allOptions := append([]pipit.Option{stdlib.WithStandardLibrary()}, limits.Options()...)
	allOptions = append(allOptions, extras...)
	if os.Getenv(goDispatchEnv) != "" {
		allOptions = append(allOptions, pipit.WithForceGoDispatch())
	}
	if len(limits.Symbols) > 0 {
		return pipit.NewInterpreterWithSymbols(limits.Symbols, allOptions...)
	}
	return pipit.NewInterpreter(allOptions...)
}
