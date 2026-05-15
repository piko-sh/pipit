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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

const (
	// isolatedSourceLimit is the maximum byte count for inline or stdin source submitted to
	// a native worker.
	isolatedSourceLimit = 1 << 20

	// isolatedCleanupRetries is the upper bound on retry attempts when a partial launch
	// cleanup fails.
	isolatedCleanupRetries = 3
)

// isolatedOptions holds explicit host policy flags, never script declarations.
type isolatedOptions struct {
	// worker is the absolute path to the static worker.
	worker *string

	// digest is the hex-encoded SHA-256 of the worker.
	digest *string

	// watchdog is the path to the static watchdog.
	watchdog *string

	// watchdogDigest is the hex SHA-256 of the watchdog.
	watchdogDigest *string

	// parent is the delegated cgroup-v2 parent on Linux.
	parent *string

	// tenant is the host-selected tenant identity.
	tenant *string

	// stateDir is the private directory for launch records.
	stateDir *string

	// logEvents enables audit event logging to stderr.
	logEvents *bool

	// broker is the path to the filesystem broker binary.
	broker *string

	// brokerDigest is the hex SHA-256 of the broker.
	brokerDigest *string

	// roots holds the filesystem root grants.
	roots *isolatedRootGrants

	// imports lists the reviewed import grants.
	imports *string

	// expression holds the inline source from -e.
	expression *string

	// entrypoint names the function to call in file mode.
	entrypoint *string

	// printResult enables JSON scalar output printing.
	printResult *bool

	// check selects probe mode without user source.
	check *bool

	// repl selects the persistent session mode.
	repl *bool
}

// isolatedEvaluator is the one-shot surface the worker kinds share.
type isolatedEvaluator interface {
	// Eval evaluates inline source.
	//
	// Takes source (string) which is the inline source text.
	//
	// Returns pipit.RestrictedResult which is the evaluation output.
	// Returns error when execution fails.
	Eval(source string) (pipit.RestrictedResult, error)

	// EvalFile evaluates file source with a named entrypoint.
	//
	// Takes source (string) which is the file source text.
	// Takes entrypoint (string) which is the function name.
	//
	// Returns pipit.RestrictedResult which is the evaluation output.
	// Returns error when execution fails.
	EvalFile(source, entrypoint string) (pipit.RestrictedResult, error)

	// Close releases the native worker resources.
	//
	// Returns error when cleanup fails.
	Close() error
}

// isolatedRootGrants collects repeated -root flags of the form name=path:rights.
type isolatedRootGrants []pipit.FilesystemRoot

// String renders the grants for flag defaults.
//
// Returns string which lists the grant names.
func (grants *isolatedRootGrants) String() string {
	if grants == nil {
		return ""
	}
	names := make([]string, 0, len(*grants))
	for _, grant := range *grants {
		names = append(names, grant.Name)
	}
	return strings.Join(names, ",")
}

// Set parses one name=path:rights grant.
//
// Takes value (string) which is the flag argument.
//
// Returns error when the grant is malformed or names an unknown right.
func (grants *isolatedRootGrants) Set(value string) error {
	name, rest, found := strings.Cut(value, "=")
	separator := strings.LastIndex(rest, ":")
	if !found || name == "" || separator < 1 {
		return errors.New("a -root grant is name=path:rights")
	}
	var rights pipit.FilesystemRights
	for _, right := range rest[separator+1:] {
		switch right {
		case 'r':
			rights |= pipit.FilesystemRead
		case 'w':
			rights |= pipit.FilesystemWrite
		case 'l':
			rights |= pipit.FilesystemList
		default:
			return fmt.Errorf("unknown -root right %q; use r, w and l", right)
		}
	}
	if rights == 0 {
		return errors.New("a -root grant needs at least one of r, w and l")
	}
	*grants = append(*grants, pipit.FilesystemRoot{Name: name, HostPath: rest[:separator], Rights: rights})
	return nil
}

// RunIsolated handles experimental native source execution and persistent sessions. It
// never registers CLI host symbols, fetches modules or falls back to trusted Eval.
//
// Takes args ([]string) which supplies host-approved worker settings and source
// selection.
// Takes streams (output.IO) which supplies source input and bounded result destinations.
//
// Returns int which is zero only after evaluation and native cleanup succeed.
func RunIsolated(ctx context.Context, args []string, streams output.IO) int {
	streams.Stdout = isolatedOutputWriter(streams.Stdout)
	streams.Stderr = isolatedOutputWriter(streams.Stderr)
	if len(args) > 0 && args[0] == "recover" {
		return runIsolatedRecover(ctx, args[1:], streams)
	}
	flags, options := isolatedFlagSet("pipit isolated", streams)
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit isolated [worker flags] -e '<source>'")
		fmt.Fprintln(streams.Stderr, "       pipit isolated [worker flags] [-entrypoint main] -")
		fmt.Fprintln(streams.Stderr, "       pipit isolated [worker flags] --check")
		fmt.Fprintln(streams.Stderr, "       pipit isolated [worker flags] --repl")
		fmt.Fprintln(streams.Stderr, "       pipit isolated recover [worker flags]")
		fmt.Fprintln(streams.Stderr, "Experimental native isolation; no fallback or module loading.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 1
	}
	config, err := isolatedConfiguration(options)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	filesystem, hasFilesystem, err := isolatedFilesystemConfiguration(options, config)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if ctx == nil || ctx.Err() != nil {
		fmt.Fprintln(streams.Stderr, "pipit: isolated invocation has no active context")
		return 1
	}
	if *options.repl {
		if hasFilesystem {
			fmt.Fprintln(streams.Stderr, "pipit: the isolated REPL has no filesystem kind; drop -broker and -root")
			return 1
		}
		if err := isolatedReplSelection(options, flags); err != nil {
			fmt.Fprintf(streams.Stderr, errorFormat, err)
			return 1
		}
		return runIsolatedRepl(ctx, config, streams, *options.printResult)
	}
	source, err := isolatedInvocationSource(options, flags, streams.Stdin)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	return runIsolatedSource(ctx, isolatedLauncher(ctx, config, &filesystem, hasFilesystem), source, options, streams)
}

// retryIsolatedCleanup finishes the cleanup a constructor could not complete, up to three
// attempts, so a partial launch never leaves native resources held.
//
// Takes launchErr (error) which may wrap *pipit.IsolatedCleanupError.
//
// Returns error which is the last cleanup failure, or nil when nothing needed retrying or
// the retry succeeded.
func retryIsolatedCleanup(ctx context.Context, launchErr error) error {
	var cleanup *pipit.IsolatedCleanupError
	if !errors.As(launchErr, &cleanup) {
		return nil
	}
	var last error
	for range isolatedCleanupRetries {
		if last = cleanup.Retry(ctx); last == nil {
			return nil
		}
	}
	return last
}

// isolatedLauncher selects the worker kind the one-shot run launches: the filesystem
// worker when the filesystem flags were given, otherwise the plain worker.
//
// Takes config (pipit.IsolatedConfig) which is the decoded worker configuration.
// Takes filesystem (*pipit.IsolatedFilesystemConfig) which is used only when selected.
// Takes hasFilesystem (bool) which selects the filesystem kind.
//
// Returns func() (isolatedEvaluator, error) which launches the chosen worker.
func isolatedLauncher(ctx context.Context, config pipit.IsolatedConfig, filesystem *pipit.IsolatedFilesystemConfig, hasFilesystem bool) func() (isolatedEvaluator, error) {
	if hasFilesystem {
		return func() (isolatedEvaluator, error) {
			worker, err := pipit.NewIsolatedFilesystemWorker(ctx, *filesystem)
			if worker == nil {
				return nil, err
			}
			return worker, err
		}
	}
	return func() (isolatedEvaluator, error) {
		worker, err := pipit.NewIsolatedWorker(ctx, config)
		if worker == nil {
			return nil, err
		}
		return worker, err
	}
}

// isolatedFlagSet declares the worker, tenant, state and filesystem flags every isolated
// invocation shares.
//
// Takes name (string) which labels the flag set in usage and errors.
// Takes streams (output.IO) which receives flag diagnostics.
//
// Returns the flag set and the option bindings it fills.
func isolatedFlagSet(name string, streams output.IO) (*flag.FlagSet, isolatedOptions) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	roots := &isolatedRootGrants{}
	flags.Var(roots, "root", "Filesystem grant as name=path:rights with rights drawn from r, w and l (repeatable)")
	return flags, isolatedOptions{
		worker:         flags.String("worker", "", "Absolute path to an independently approved static worker"),
		digest:         flags.String("worker-sha256", "", "Independently approved SHA-256 digest, in hexadecimal"),
		watchdog:       flags.String("watchdog", "", "Absolute path to an independently approved static watchdog"),
		watchdogDigest: flags.String("watchdog-sha256", "", "Independently approved watchdog SHA-256 digest, in hexadecimal"),
		parent:         flags.String("cgroup-parent", "", "Explicitly delegated cgroup-v2 parent on Linux"),
		tenant:         flags.String("tenant", "default", "Host-selected tenant identity keying the service reservation"),
		stateDir:       flags.String("state-dir", "", "Absolute private directory recording each launch for recovery"),
		logEvents:      flags.Bool("log", false, "Write the isolation boundary's audit events to stderr"),
		broker:         flags.String("broker", "", "Absolute path to an independently approved static filesystem broker"),
		brokerDigest:   flags.String("broker-sha256", "", "Independently approved broker SHA-256 digest, in hexadecimal"),
		roots:          roots,
		imports:        flags.String("imports", "", "Reviewed imports to grant (currently: math)"),
		expression:     flags.String("e", "", "Expression or statements to evaluate"),
		entrypoint:     flags.String("entrypoint", "", "Treat input as a complete Go file and call this entrypoint"),
		printResult:    flags.Bool("print", true, "Print the resulting JSON scalar"),
		check:          flags.Bool("check", false, "Probe the configured native worker without running user source"),
		repl:           flags.Bool("repl", false, "Run a bounded persistent session in a dedicated native worker"),
	}
}

// runIsolatedSource evaluates one bounded source selection and reaps its worker.
//
// Takes launch (func) which creates the selected worker kind.
// Takes source (string) which is the bounded source text.
// Takes options (isolatedOptions) which holds the parsed command-line policy.
// Takes streams (output.IO) which provides host output destinations.
//
// Returns zero only after execution, output and native cleanup succeed.
func runIsolatedSource(_ context.Context, launch func() (isolatedEvaluator, error), source string, options isolatedOptions, streams output.IO) (exitCode int) {
	worker, err := launch()
	if worker != nil {
		defer func() {
			if closeErr := worker.Close(); closeErr != nil {
				fmt.Fprintf(streams.Stderr, errorFormat, closeErr)
				exitCode = 1
			}
		}()
	}
	if err != nil {
		if cleanupErr := retryIsolatedCleanup(context.Background(), err); cleanupErr != nil {
			fmt.Fprintf(streams.Stderr, errorFormat, cleanupErr)
		} else {
			fmt.Fprintf(streams.Stderr, errorFormat, err)
		}
		return 1
	}
	var result pipit.RestrictedResult
	if *options.entrypoint == "" {
		result, err = worker.Eval(source)
	} else {
		result, err = worker.EvalFile(source, *options.entrypoint)
	}
	if *options.check {
		return writeIsolatedCheck(result, err, streams)
	}
	return writeIsolatedResult(result, err, *options.printResult, streams)
}

// isolatedInvocationSource rejects source selection in probe mode without reading stdin.
//
// Takes options (isolatedOptions) which selects the invocation mode.
// Takes flags (*flag.FlagSet) which records explicitly supplied source flags.
// Takes stdin (io.Reader) which supplies source only outside probe mode.
//
// Returns string which is the fixed probe source or bounded user source.
// Returns error when the source selection conflicts or the input read fails.
func isolatedInvocationSource(options isolatedOptions, flags *flag.FlagSet, stdin io.Reader) (string, error) {
	if !*options.check {
		return isolatedSource(*options.expression, flags.Args(), stdin)
	}
	conflict := len(flags.Args()) != 0
	flags.Visit(func(option *flag.Flag) {
		if option.Name == "e" || option.Name == "entrypoint" {
			conflict = true
		}
	})
	if conflict {
		return "", errors.New("isolated --check cannot be combined with source or an entrypoint")
	}
	return "1", nil
}

// writeIsolatedCheck reports a completed live probe, not a security certification.
//
// Takes result (pipit.RestrictedResult) which follows worker reaping and cleanup.
// Takes evaluationErr (error) which records evaluation or confinement failure.
// Takes streams (output.IO) which provides host output destinations.
//
// Returns int which reports probe or output failure.
func writeIsolatedCheck(result pipit.RestrictedResult, evaluationErr error, streams output.IO) int {
	if evaluationErr == nil && (string(result.Value) != "1" || result.Output != "" || result.OutputTruncated) {
		evaluationErr = errors.New("isolated worker returned an unexpected probe result")
	}
	if evaluationErr != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, evaluationErr)
		return 1
	}
	if _, err := fmt.Fprintln(streams.Stdout, "Native worker probe passed. Experimental; not a security certification."); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	return 0
}

// isolatedConfiguration decodes only explicit host-supplied worker approval.
//
// Takes options (isolatedOptions) which holds parsed command-line policy.
//
// Returns pipit.IsolatedConfig which is the populated configuration.
// Returns error when the executable approval is missing or malformed.
func isolatedConfiguration(options isolatedOptions) (pipit.IsolatedConfig, error) {
	var config pipit.IsolatedConfig
	digest, err := hex.DecodeString(*options.digest)
	if err != nil || len(digest) != sha256.Size || *options.worker == "" {
		return config, errors.New("isolated execution requires -worker and an approved 64-digit -worker-sha256")
	}
	config.WorkerPath = *options.worker
	config.WorkerSHA256 = [sha256.Size]byte(digest)
	watchdogDigest, err := hex.DecodeString(*options.watchdogDigest)
	if err != nil || len(watchdogDigest) != sha256.Size || *options.watchdog == "" {
		return config, errors.New("isolated execution requires -watchdog and an approved 64-digit -watchdog-sha256")
	}
	config.WatchdogPath = *options.watchdog
	config.WatchdogSHA256 = [sha256.Size]byte(watchdogDigest)
	config.LinuxCgroupParent = *options.parent
	config.Tenant = *options.tenant
	if *options.stateDir == "" {
		return config, errors.New("isolated execution requires an absolute private -state-dir")
	}
	config.StateDirectory = *options.stateDir
	if *options.logEvents {
		config.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if *options.imports != "" {
		config.Imports = strings.Split(*options.imports, ",")
	}
	return config, nil
}

// isolatedFilesystemConfiguration decodes the filesystem kind's broker approval and root
// grants when any of them is given.
//
// Takes options (isolatedOptions) which holds parsed command-line policy.
// Takes worker (pipit.IsolatedConfig) which is the decoded worker configuration.
//
// Returns pipit.IsolatedFilesystemConfig which is the filesystem configuration.
// Returns bool which is true when the filesystem kind is selected; false when neither a
// broker nor a root was given.
// Returns error when the broker approval is incomplete.
func isolatedFilesystemConfiguration(options isolatedOptions, worker pipit.IsolatedConfig) (pipit.IsolatedFilesystemConfig, bool, error) {
	var config pipit.IsolatedFilesystemConfig
	if *options.broker == "" && *options.brokerDigest == "" && len(*options.roots) == 0 {
		return config, false, nil
	}
	digest, err := hex.DecodeString(*options.brokerDigest)
	if err != nil || len(digest) != sha256.Size || *options.broker == "" {
		return config, false, errors.New("the filesystem kind requires -broker and an approved 64-digit -broker-sha256")
	}
	config.Worker = worker
	config.BrokerPath = *options.broker
	config.BrokerSHA256 = [sha256.Size]byte(digest)
	config.Roots = append([]pipit.FilesystemRoot(nil), *options.roots...)
	return config, true, nil
}

// isolatedSource reads source bytes without opening script-selected paths or modules.
// Stdin acquisition is host input, before worker admission and execution deadlines.
//
// Takes expression (string) which selects inline source when non-empty.
// Takes positional ([]string) which must select exactly stdin or no extra arguments.
// Takes stdin (io.Reader) which is bounded before allocating source storage.
//
// Returns string which contains at most the source byte limit.
// Returns error when the selection is ambiguous, the source is oversized or the read
// fails.
func isolatedSource(expression string, positional []string, stdin io.Reader) (string, error) {
	if expression != "" {
		if len(positional) != 0 || len(expression) > isolatedSourceLimit {
			return "", errors.New("ambiguous or oversized isolated source")
		}
		return expression, nil
	}
	if len(positional) != 1 || positional[0] != "-" || stdin == nil {
		return "", errors.New("select isolated source with -e or a single '-' for stdin")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, isolatedSourceLimit+1))
	if err != nil {
		return "", fmt.Errorf("reading isolated source: %w", err)
	}
	if len(data) == 0 || len(data) > isolatedSourceLimit {
		return "", errors.New("empty or oversized isolated source")
	}
	return string(data), nil
}

// writeIsolatedResult emits validated bounded output and a scalar value. One-shot callers
// reap before publication; a session stays alive between submissions.
//
// Takes result (pipit.RestrictedResult) which contains no live interpreter objects.
// Takes evaluationErr (error) which records execution or native boundary failure.
// Takes printResult (bool) which enables JSON scalar output.
// Takes streams (output.IO) which provides host output destinations.
//
// Returns int which reports evaluation or output failure.
func writeIsolatedResult(result pipit.RestrictedResult, evaluationErr error, printResult bool, streams output.IO) int {
	_, outputErr := io.WriteString(streams.Stdout, result.Output)
	if err := errors.Join(evaluationErr, outputErr); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if printResult && len(result.Value) != 0 && string(result.Value) != "null" {
		if _, err := fmt.Fprintln(streams.Stdout, string(result.Value)); err != nil {
			fmt.Fprintf(streams.Stderr, errorFormat, err)
			return 1
		}
	}
	return 0
}
