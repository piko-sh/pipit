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

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"time"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/symtab"
)

const (
	// defaultRestrictedSourceBytes is the default source size limit per submission.
	defaultRestrictedSourceBytes = 1 << 20

	// defaultRestrictedOutputBytes is the default captured output limit.
	defaultRestrictedOutputBytes = 64 << 10

	// defaultRestrictedReturnBytes is the default encoded result limit.
	defaultRestrictedReturnBytes = 256 << 10

	// defaultRestrictedStringBytes is the default string operation limit.
	defaultRestrictedStringBytes = 1 << 20

	// defaultRestrictedAllocationElements is the default single-allocation limit.
	defaultRestrictedAllocationElements = 65536

	// defaultRestrictedCallDepth is the default interpreted call stack limit.
	defaultRestrictedCallDepth = 256

	// defaultRestrictedCostBudget is the default metered cost budget.
	defaultRestrictedCostBudget = 10_000_000

	// defaultRestrictedArenaBytes is the default register arena limit.
	defaultRestrictedArenaBytes = 64 << 20

	// restrictedExpressionDepth is the expression nesting depth limit.
	restrictedExpressionDepth = 256

	// restrictedConstantPoolSize is the constant pool entry limit.
	restrictedConstantPoolSize = 65536

	// restrictedSpecialisations is the type specialisation limit.
	restrictedSpecialisations = 1024

	// restrictedMethods is the method resolution limit.
	restrictedMethods = 1024

	// restrictedFilesystemPackage is the script-visible import the filesystem broker serves;
	// it is always permitted when a filesystem is attached, independently of the native
	// registry.
	restrictedFilesystemPackage = "pipit/fs"
)

var (
	// ErrInvalidRestrictedConfig reports an invalid restricted configuration. It wraps
	// policy.ErrInvalidConfig.
	ErrInvalidRestrictedConfig = fmt.Errorf("invalid restricted configuration: %w", policy.ErrInvalidConfig)

	// ErrRestrictedBusy reports a concurrent restricted submission. It wraps policy.ErrBusy.
	ErrRestrictedBusy = fmt.Errorf("restricted interpreter is already running: %w", policy.ErrBusy)

	// ErrRestrictedLimit reports a source, output, or returned value limit. It wraps
	// policy.ErrLimit.
	ErrRestrictedLimit = fmt.Errorf("restricted resource limit exceeded: %w", policy.ErrLimit)

	// ErrRestrictedResult reports a result that cannot be a JSON scalar.
	ErrRestrictedResult = errors.New("restricted result must be a scalar value")
)

// RestrictedConfig configures cooperative in-process restrictions. Zero limits select
// finite defaults; negative limits are invalid.
type RestrictedConfig struct {
	// Hook gates every native capability the script reaches. A nil hook installs
	// policy.DenyCapabilityHook, which permits pure computation but refuses every
	// host-touching capability (files, network, subprocess, environment).
	Hook policy.CapabilityHook

	// Logger receives the interpreter's diagnostics for submissions run under this
	// configuration. Nil resolves the process default logger at each call site.
	Logger *slog.Logger `json:"-"`

	// Imports names reviewed packages to expose. An empty list denies all imports.
	Imports []string

	// Timeout bounds cooperative compilation and execution. The default is two seconds.
	Timeout time.Duration

	// MaxSourceBytes bounds each submission before parsing. The default is 1 MiB.
	MaxSourceBytes int

	// MaxOutputBytes bounds captured print and println output. The default is 64 KiB.
	MaxOutputBytes int

	// MaxReturnBytes bounds the encoded JSON scalar. The default is 256 KiB.
	MaxReturnBytes int

	// MaxStringBytes bounds interpreter string operations. The default is 1 MiB.
	MaxStringBytes int

	// MaxAllocationElements bounds a single allocation, not aggregate process memory.
	MaxAllocationElements int

	// MaxCallDepth bounds the interpreted call stack. The default is 256 calls.
	MaxCallDepth int

	// CostBudget bounds metered interpreter work. The default is 10,000,000 cost units.
	CostBudget int64

	// MaxArenaBytes bounds the register arena, not all Go memory. The default is 64 MiB.
	MaxArenaBytes int64
}

// RestrictedResult contains only captured output and a bounded JSON scalar. It never
// exposes native objects, interpreter state, functions, or compiled bytecode.
type RestrictedResult struct {
	// Output contains captured print and println output.
	Output string

	// Value contains a JSON scalar, or nil when the entrypoint returns no value.
	Value json.RawMessage

	// CostUsed reports metered execution cost, not compilation cost.
	CostUsed int64

	// OutputTruncated reports whether the capture writer discarded output bytes.
	OutputTruncated bool
}

// RestrictedInterpreter executes each submission in fresh interpreter state. It rejects
// concurrent calls without queuing them.
type RestrictedInterpreter struct {
	// filesystem is the optional broker proxy for pipit/fs access.
	filesystem *sandboxbroker.FilesystemProxy

	// provider supplies the full native symbol registry the allowlist selects from; a nil
	// provider keeps the math-only surface for callers that do not inject one.
	provider symtab.SymbolProviderPort

	// active admits at most one submission without retaining queued source strings.
	active chan struct{}

	// config is a private copy that is never changed after construction.
	config RestrictedConfig
}

// NewRestrictedInterpreter copies and validates policy. With no injected provider only
// the reviewed math subset is available.
//
// Takes config (RestrictedConfig) which selects limits and reviewed imports.
//
// Returns *RestrictedInterpreter which evaluates source with fresh state per call.
// Returns error when a limit is negative or an import has no reviewed manifest.
func NewRestrictedInterpreter(config RestrictedConfig) (*RestrictedInterpreter, error) {
	return newRestrictedInterpreter(config, nil)
}

// NewRestrictedInterpreterWithProvider builds a restricted interpreter over the full
// symbol registry the provider supplies.
//
// Takes config (RestrictedConfig) which selects limits and imports.
// Takes provider (symtab.SymbolProviderPort) which supplies the native registry.
//
// Returns *RestrictedInterpreter over the allowlisted registry and any validation error.
func NewRestrictedInterpreterWithProvider(config RestrictedConfig, provider symtab.SymbolProviderPort) (*RestrictedInterpreter, error) {
	if provider == nil {
		return nil, ErrInvalidRestrictedConfig
	}
	return newRestrictedInterpreter(config, provider)
}

// newRestrictedInterpreter validates and defaults config, then binds the provider.
//
// Takes config (RestrictedConfig) which holds the limits and import allowlist.
// Takes provider (symtab.SymbolProviderPort) which may be nil.
//
// Returns the interpreter, or an error for an invalid limit or import.
func newRestrictedInterpreter(config RestrictedConfig, provider symtab.SymbolProviderPort) (*RestrictedInterpreter, error) {
	if config.Timeout < 0 || config.CostBudget < 0 || config.MaxArenaBytes < 0 {
		return nil, ErrInvalidRestrictedConfig
	}
	limits := []struct {
		value    *int
		fallback int
	}{
		{value: &config.MaxSourceBytes, fallback: defaultRestrictedSourceBytes},
		{value: &config.MaxOutputBytes, fallback: defaultRestrictedOutputBytes},
		{value: &config.MaxReturnBytes, fallback: defaultRestrictedReturnBytes},
		{value: &config.MaxStringBytes, fallback: defaultRestrictedStringBytes},
		{value: &config.MaxAllocationElements, fallback: defaultRestrictedAllocationElements},
		{value: &config.MaxCallDepth, fallback: defaultRestrictedCallDepth},
	}
	for _, limit := range limits {
		if *limit.value < 0 {
			return nil, ErrInvalidRestrictedConfig
		}
		if *limit.value == 0 {
			*limit.value = limit.fallback
		}
	}
	if config.Timeout == 0 {
		config.Timeout = 2 * time.Second
	}
	if config.CostBudget == 0 {
		config.CostBudget = defaultRestrictedCostBudget
	}
	if config.MaxArenaBytes == 0 {
		config.MaxArenaBytes = defaultRestrictedArenaBytes
	}
	config.Imports = append([]string(nil), config.Imports...)
	if err := validateRestrictedImports(&config, provider); err != nil {
		return nil, err
	}
	if config.Hook == nil {
		config.Hook = policy.DenyCapabilityHook{}
	}
	return &RestrictedInterpreter{config: config, active: make(chan struct{}, 1), filesystem: nil, provider: provider}, nil
}

// Eval evaluates an expression or statements with no state retained between calls.
//
// Takes source (string) which is the expression or statement source.
//
// Returns RestrictedResult which contains bounded output and a JSON scalar.
// Returns error when evaluation, policy checks, or resource accounting fail.
func (interpreter *RestrictedInterpreter) Eval(ctx context.Context, source string) (RestrictedResult, error) {
	return interpreter.run(ctx, source, "", false)
}

// EvalFile evaluates a complete source file and calls its named entrypoint.
//
// Takes source (string) which is a complete Go source file.
// Takes entrypoint (string) which names the function to call.
//
// Returns RestrictedResult which contains bounded output and a JSON scalar.
// Returns error when evaluation, policy checks, or resource accounting fail.
func (interpreter *RestrictedInterpreter) EvalFile(ctx context.Context, source, entrypoint string) (RestrictedResult, error) {
	return interpreter.run(ctx, source, entrypoint, true)
}

// run admits one submission and discards its interpreter state after execution.
//
// Takes source (string) which is checked before parsing.
// Takes entrypoint (string) which names the function for a complete source file.
// Takes file (bool) which selects complete-file rather than expression evaluation.
//
// Returns RestrictedResult which contains bounded output and a JSON scalar.
// Returns error when admission, evaluation, or serialisation fails.
func (interpreter *RestrictedInterpreter) run(ctx context.Context, source, entrypoint string, file bool) (RestrictedResult, error) {
	if interpreter == nil || interpreter.active == nil || ctx == nil {
		return RestrictedResult{}, ErrInvalidRestrictedConfig
	}
	if err := ctx.Err(); err != nil {
		return RestrictedResult{}, err
	}
	select {
	case interpreter.active <- struct{}{}:
		defer func() { <-interpreter.active }()
	default:
		return RestrictedResult{}, ErrRestrictedBusy
	}
	config := interpreter.config
	if len(source) > config.MaxSourceBytes {
		return RestrictedResult{}, fmt.Errorf("%w: source", ErrRestrictedLimit)
	}
	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	output := &restrictedOutput{buffer: bytes.Buffer{}, remaining: config.MaxOutputBytes, truncated: false}
	service := interpreter.newService(output)
	var value any
	var err error
	if file {
		value, err = service.EvalFile(ctx, source, entrypoint)
	} else {
		value, err = service.Eval(ctx, source)
	}
	result := RestrictedResult{Value: nil, Output: output.String(), OutputTruncated: output.truncated, CostUsed: service.LastCostUsed()}
	if interpreter.filesystem != nil {
		err = errors.Join(err, interpreter.filesystem.Err())
	}
	if err != nil {
		return result, errors.Join(err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Value, err = restrictedScalar(value, config.MaxReturnBytes)
	return result, err
}

// newService builds a fresh interpreter from the private symbol manifest.
//
// Takes output (*restrictedOutput) which captures bounded builtin output.
//
// Returns *Service which is not exposed through the restricted API.
func (interpreter *RestrictedInterpreter) newService(output *restrictedOutput) *Service {
	config := interpreter.config
	service := NewService(
		WithSafeMode(),
		WithBytecodeVerification(true),
		WithCapabilityHook(config.Hook),
		WithFeatures(policy.InterpFeaturesRestricted&^policy.InterpFeatureChannels),
		WithImportAllowlist(config.Imports...),
		WithMaxExecutionTime(config.Timeout),
		WithMaxSourceSize(config.MaxSourceBytes),
		WithMaxOutputSize(config.MaxOutputBytes),
		WithMaxStringSize(config.MaxStringBytes),
		WithMaxAllocSize(config.MaxAllocationElements),
		WithMaxLiteralElements(config.MaxAllocationElements),
		WithMaxCallDepth(config.MaxCallDepth),
		WithMaxArenaSizeBytes(safeconv.Int64ToUint64(config.MaxArenaBytes)),
		WithMaxExpressionDepth(restrictedExpressionDepth),
		WithMaxConstantPoolSize(restrictedConstantPoolSize),
		WithMaxSpecialisations(restrictedSpecialisations),
		WithMaxMethods(restrictedMethods),
		WithCostBudget(config.CostBudget),
		WithStderr(output),
		WithLogger(config.Logger),
	)
	service.UseSymbols(interpreter.restrictedRegistry())
	return service
}

// restrictedRegistry builds the symbol registry for one submission: the allowlisted
// packages from the injected provider (with their pre-built types packages), or the
// math-only subset when no provider was injected, plus pipit/fs when a filesystem is
// attached. The unsafe package is always protected.
//
// Returns *symtab.SymbolRegistry which is installed for this submission only.
func (interpreter *RestrictedInterpreter) restrictedRegistry() *symtab.SymbolRegistry {
	allow := make(map[string]struct{}, len(interpreter.config.Imports))
	for _, path := range interpreter.config.Imports {
		allow[path] = struct{}{}
	}
	exports := interpreter.restrictedExports(allow)
	registry := symtab.NewSymbolRegistry(exports)
	registry.ProtectPackage(policy.PkgUnsafe)
	if typed, ok := interpreter.provider.(program.TypesPackageProviderPort); ok {
		for path, pkg := range typed.TypesPackages() {
			if _, allowed := allow[path]; allowed {
				registry.RegisterTypesPackage(path, pkg)
			}
		}
	}
	registry.SynthesiseAll()
	return registry
}

// restrictedExports selects the native symbols this submission may reach: the allowlisted
// packages from the provider, or the math-only subset, plus pipit/fs when a filesystem is
// attached.
//
// Takes allow (map[string]struct{}) which is the import allowlist.
//
// Returns symtab.SymbolExports keyed by package path.
func (interpreter *RestrictedInterpreter) restrictedExports(allow map[string]struct{}) symtab.SymbolExports {
	exports := make(symtab.SymbolExports)
	if interpreter.provider != nil {
		for path, symbols := range interpreter.provider.Exports() {
			if _, ok := allow[path]; ok && path != policy.PkgUnsafe {
				exports[path] = symbols
			}
		}
	} else if _, ok := allow["math"]; ok {
		exports["math"] = map[string]reflect.Value{
			"Abs": reflect.ValueOf(math.Abs), "Ceil": reflect.ValueOf(math.Ceil),
			"Floor": reflect.ValueOf(math.Floor), "Max": reflect.ValueOf(math.Max),
			"Min": reflect.ValueOf(math.Min), "Sqrt": reflect.ValueOf(math.Sqrt),
		}
	}
	if interpreter.filesystem != nil {
		exports[restrictedFilesystemPackage] = map[string]reflect.Value{
			"Read": reflect.ValueOf(interpreter.filesystem.Read), "Write": reflect.ValueOf(interpreter.filesystem.Write),
			"List": reflect.ValueOf(interpreter.filesystem.List),
		}
	}
	return exports
}

// restrictedOutput bounds captured builtin output without exposing buffer methods.
type restrictedOutput struct {
	// buffer accumulates captured output bytes.
	buffer bytes.Buffer

	// remaining tracks the remaining output byte budget.
	remaining int

	// truncated is true when output exceeded the budget.
	truncated bool
}

// Write appends only the bytes that fit within the remaining capture budget.
//
// Takes data ([]byte) which is the next output chunk.
//
// Returns int which counts the bytes retained.
// Returns error when the chunk exceeds the remaining budget.
func (output *restrictedOutput) Write(data []byte) (int, error) {
	if len(data) > output.remaining {
		written, _ := output.buffer.Write(data[:output.remaining])
		output.remaining = 0
		output.truncated = true
		return written, ErrRestrictedLimit
	}
	written, err := output.buffer.Write(data)
	output.remaining -= written
	return written, err
}

// String returns the captured output.
//
// Returns string which contains at most the configured output byte limit.
func (output *restrictedOutput) String() string {
	return output.buffer.String()
}

// validateRestrictedImports checks the import list against the available surface and
// applies the default (math with no provider, the pure surface with one).
//
// Takes config (*RestrictedConfig) whose Imports may be defaulted and are validated.
// Takes provider (symtab.SymbolProviderPort) which supplies the full registry, or nil.
//
// Returns error when an import is not available.
func validateRestrictedImports(config *RestrictedConfig, provider symtab.SymbolProviderPort) error {
	if provider == nil {
		for _, path := range config.Imports {
			if path != "math" {
				return fmt.Errorf("%w: unreviewed import %q", ErrInvalidRestrictedConfig, path)
			}
		}
		return nil
	}
	available := provider.Exports()
	if len(config.Imports) == 0 {
		config.Imports = restrictedDefaultImports(available)
		return nil
	}
	for _, path := range config.Imports {
		if _, ok := available[path]; !ok && path != restrictedFilesystemPackage {
			return fmt.Errorf("%w: unregistered import %q", ErrInvalidRestrictedConfig, path)
		}
	}
	return nil
}

// restrictedDefaultImports returns the pure surface intersected with what the provider
// actually registers, so the default never names a package the registry lacks.
//
// Takes available (symtab.SymbolExports) which is the provider's registry.
//
// Returns []string which is the default import allowlist.
func restrictedDefaultImports(available symtab.SymbolExports) []string {
	defaults := policy.PureSurface()
	kept := defaults[:0]
	for _, path := range defaults {
		if _, ok := available[path]; ok {
			kept = append(kept, path)
		}
	}
	return kept
}

// restrictedScalar serialises a scalar without invoking user-defined methods.
//
// Takes value (any) which is the interpreter result.
// Takes maxBytes (int) which bounds the encoded JSON value.
//
// Returns json.RawMessage which contains no live interpreter or native objects.
// Returns error when the result is unsupported or exceeds its byte limit.
func restrictedScalar(value any, maxBytes int) (json.RawMessage, error) {
	switch scalar := value.(type) {
	case nil:
		return nil, nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
	case string:
		if len(scalar) > maxBytes {
			return nil, fmt.Errorf("%w: result", ErrRestrictedLimit)
		}
	default:
		return nil, ErrRestrictedResult
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, ErrRestrictedResult, err)
	}
	if len(encoded) > maxBytes {
		return nil, fmt.Errorf("%w: encoded result", ErrRestrictedLimit)
	}
	return encoded, nil
}
