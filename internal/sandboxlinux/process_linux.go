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

package sandboxlinux

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// defaultWorkerLifetime is the default whole-process lifetime.
	defaultWorkerLifetime = 17 * time.Second

	// defaultWorkerOutput is the default combined output byte limit.
	defaultWorkerOutput = 64 << 10

	// maximumWorkerOutput is the largest allowed combined output limit.
	maximumWorkerOutput = 1 << 20

	// workerCleanupTimeout is the deadline for resource cleanup after exit.
	workerCleanupTimeout = 5 * time.Second

	// workerPipeTimeout is the grace period for draining worker pipes.
	workerPipeTimeout = time.Second
)

var (
	// ErrWorkerOutput reports that script output and native diagnostics exceeded their
	// shared budget.
	ErrWorkerOutput = errors.New("worker combined output limit exceeded")
)

// WorkerConfig contains host-private launch settings, never worker-selected claims. Zero
// limits select finite defaults.
type WorkerConfig struct {
	// ImageStore holds the optional private image store.
	ImageStore *sandboxbroker.LinuxImageStore

	// Checkpoint, when set, names the empty stores a top-level launch records its recovery
	// identity in before any child exists, so a host crash leaves a claimable record; it
	// requires ImageStore. Launches into an existing group leave it nil.
	Checkpoint *WorkerCheckpoint

	// Cache, when set, supplies pre-staged, hash-verified images keyed by digest. A launch
	// whose Digest hits the cache copies the pinned bytes and skips re-hashing and ELF
	// validation; a miss stages from Executable as usual.
	Cache *WorkerImageCache

	// WatchdogExecutable holds the approved watchdog binary path.
	WatchdogExecutable string

	// Executable holds the approved worker binary path.
	Executable string

	// CgroupParent holds the delegated cgroup parent path.
	CgroupParent string

	// Tenant is the host-selected identity whose service reservation and admission slot the
	// launch uses; see ValidateTenant.
	Tenant string

	// Limits holds the finite cgroup resource bounds.
	Limits Limits

	// Lifetime holds the whole-process backstop duration.
	Lifetime time.Duration

	// OutputBytes holds the combined output byte budget.
	OutputBytes int

	// Digest holds the approved worker executable hash.
	Digest [sha256.Size]byte

	// WatchdogDigest holds the approved watchdog executable hash.
	WatchdogDigest [sha256.Size]byte
}

// WorkerProcess owns one verified image, resource group, process and private channel.
// Call Close even after Wait succeeds.
type WorkerProcess struct {
	// lifetimeDone is closed once the worker's lifetime has ended, through cancel, its
	// deadline or the context it was launched under (watchLifetime).
	lifetimeDone <-chan struct{}

	// lifetimeCause reports why the lifetime ended, nil while it lasts.
	lifetimeCause func() error

	// cancel stops the lifetime context.
	cancel context.CancelFunc

	// releaseAdmission frees the process-wide admission slot.
	releaseAdmission func()

	// image holds the owned worker image.
	image *WorkerImage

	// group holds the owned child cgroup.
	group *Group

	// serviceGroup holds the top-level service cgroup, if any.
	serviceGroup *Group

	// container holds the optional intermediate container cgroup.
	container *Group

	// watchdog holds the optional supervisor process.
	watchdog *WorkerProcess

	// stopWatchdogExit cancels the watchdog exit callback.
	stopWatchdogExit func() bool

	// stream holds the private IPC transport socket.
	stream *os.File

	// output holds the bounded diagnostic output writer.
	output *workerOutput

	// brokerParent holds the optional aggregate parent for a broker.
	brokerParent *Group

	// checkpoint holds the optional recovery checkpoint store.
	checkpoint *sandboxbroker.LinuxRecoveryCheckpointStore

	// done is closed after reaping and the first cleanup attempt.
	done chan struct{}

	// runErr caches the terminal process or cancellation error.
	runErr error

	// cleanupErr caches the first cleanup attempt error.
	cleanupErr error

	// phaseDeadline holds the active phase deadline.
	phaseDeadline time.Time

	// phaseTimer holds the armed phase expiry timer.
	phaseTimer *time.Timer

	// phaseErr caches the phase deadline error.
	phaseErr error

	// brokerNamespace holds the broker staging namespace, if any.
	brokerNamespace string

	// mutex guards mutable state during cleanup.
	mutex sync.Mutex

	// phaseMutex guards phase deadline state.
	phaseMutex sync.Mutex

	// phaseFinished is true once the phase has ended.
	phaseFinished bool

	// released is true once all native resources are cleaned up.
	released bool
}

// Stream returns the raw private channel, never a diagnostic stream. Use WorkerProcess
// itself as the protocol transport to enforce hard phase deadlines.
//
// Returns *os.File which is nil if launch failed before channel creation.
func (process *WorkerProcess) Stream() *os.File {
	return process.stream
}

// Done signals process reaping and completion of the first cleanup attempt. A closed
// channel does not prove cleanup succeeded; Wait or Close reports errors.
//
// Returns a receive-only lifecycle notification channel.
func (process *WorkerProcess) Done() <-chan struct{} {
	return process.done
}

// Wait waits for process reaping and the first resource cleanup attempt.
//
// Returns error for process failure, cancellation, output overflow or cleanup failure.
func (process *WorkerProcess) Wait() error {
	<-process.done
	return errors.Join(process.runErr, process.cleanupErr, process.output.failure())
}

// Close cancels execution, closes IPC, waits for reaping and retries resource cleanup. An
// unsuccessful cleanup retains the image and cgroup for another Close attempt.
//
// Returns error for execution failure or a resource cleanup that still needs retrying.
func (process *WorkerProcess) Close(ctx context.Context) error {
	process.cancel()
	if process.stream != nil {
		_ = process.stream.Close()
	}
	<-process.done
	return errors.Join(process.runErr, process.cleanup(ctx), process.output.failure())
}

// Output snapshots bounded combined stdout and stderr, including before exit.
//
// Returns string containing at most the configured diagnostic byte limit.
//
// Safe for concurrent use by multiple goroutines.
func (process *WorkerProcess) Output() string {
	process.output.mutex.Lock()
	defer process.output.mutex.Unlock()
	return string(process.output.data)
}

// Released reports whether every native resource the launch owned (the worker's group and
// image, its supervisor, the service reservation and the launch record's lease) has been
// cleaned up. A failed run still releases once cleanup succeeds; a failed cleanup leaves
// it false until a later Close succeeds.
//
// Returns bool which is true only after a successful cleanup.
//
// Safe for concurrent use by multiple goroutines.
func (process *WorkerProcess) Released() bool {
	if process == nil {
		return true
	}
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.released
}

// watchLifetime keeps the means to observe when lifetime ends, so the deadline and
// readiness checks see it without holding the context.
func (process *WorkerProcess) watchLifetime(lifetime context.Context) {
	process.lifetimeDone = lifetime.Done()
	process.lifetimeCause = lifetime.Err
}

// lifetimeErr reports why the worker's lifetime ended.
//
// Returns error which is nil while the lifetime lasts.
func (process *WorkerProcess) lifetimeErr() error {
	return process.lifetimeCause()
}

// onLifetimeEnd calls end once the worker's lifetime ends, unless stop is called first.
//
// Takes end (func()) which runs on its own goroutine.
//
// Returns func() bool which stops the call and reports whether it stopped it.
//
// Safe for concurrent use. The callback runs on its own goroutine. The returned function
// uses sync.Once to close the stop channel once.
func (process *WorkerProcess) onLifetimeEnd(end func()) func() bool {
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-process.lifetimeDone:
			end()
		case <-stopped:
		}
	}()
	return func() bool {
		stoppedFirst := false
		once.Do(func() {
			close(stopped)
			stoppedFirst = process.lifetimeErr() == nil
		})
		return stoppedFirst
	}
}

// watch reaps the worker and unblocks protocol readers when its lifetime ends.
//
// Takes command (*exec.Cmd) which has successfully started
//
//	exactly once.
//
// Runs on its own goroutine; signals completion through done.
//
// Not safe for concurrent use. Called exactly once per process.
func (process *WorkerProcess) watch(ctx context.Context, command *exec.Cmd) {
	stopClose := context.AfterFunc(ctx, func() { _ = process.stream.Close() })
	go func() {
		err := command.Wait()
		stopClose()
		process.finish(ctx, err)
	}()
}

// prepare verifies resources and starts the worker without exposing source.
//
// Takes config (WorkerConfig) which holds host-private launch policy.
// Takes inherited ([]*os.File) which holds approved bootstrap handles.
// Takes parent (*Group) which is the optional aggregate parent.
// Takes guarded (bool) which selects watchdog protection.
//
// Returns *exec.Cmd which has been started inside its resource group.
// Returns error when image, resources or atomic launch fails.
func (process *WorkerProcess) prepare(ctx context.Context, config WorkerConfig, inherited []*os.File, parent *Group, guarded bool) (*exec.Cmd, error) {
	parent, err := process.prepareServiceParent(ctx, config, parent)
	if err != nil {
		return nil, err
	}
	if guarded {
		parent, err = process.prepareWatchdog(ctx, config, parent)
		if err != nil {
			return nil, err
		}
	}
	process.group, err = parent.newChild(ctx, config.Limits)
	if err != nil {
		return nil, err
	}
	process.image, err = prepareWorkerImageCached(ctx, config.Executable, config.Digest, config.ImageStore, config.Cache)
	if err != nil {
		return nil, err
	}
	root, err := process.image.Root()
	if err != nil {
		return nil, err
	}
	attributes, err := WorkerAttributes(root)
	if err != nil {
		return nil, err
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	process.stream = os.NewFile(uintptr(pair[0]), "host-worker-ipc")
	child := os.NewFile(uintptr(pair[1]), "worker-ipc")
	defer child.Close()
	command := exec.CommandContext(ctx, "/worker")
	command.Dir = "/"
	command.Env = []string{}
	command.ExtraFiles = []*os.File{child}
	command.ExtraFiles = append(command.ExtraFiles, inherited...)
	command.SysProcAttr = attributes
	command.Stdout = process.output
	command.Stderr = process.output
	command.WaitDelay = workerPipeTimeout
	if err := process.group.start(command); err != nil {
		return nil, err
	}
	return command, nil
}

// finish publishes terminal results only after process reaping and cleanup.
//
// Takes err (error) which records launch or process-wait failure.
//
// Not safe for concurrent use. Called exactly once from the watch goroutine.
func (process *WorkerProcess) finish(ctx context.Context, err error) {
	process.phaseMutex.Lock()
	process.phaseFinished = true
	if process.phaseTimer != nil {
		process.phaseTimer.Stop()
	}
	if !process.phaseDeadline.IsZero() && !time.Now().Before(process.phaseDeadline) {
		process.phaseErr = context.DeadlineExceeded
	}
	process.runErr = errors.Join(err, ctx.Err(), process.phaseErr)
	process.phaseMutex.Unlock()
	process.output.mutex.Lock()
	if process.output.exceeded {
		process.runErr = errors.Join(process.runErr, ErrWorkerOutput)
	}
	process.output.mutex.Unlock()
	process.cancel()
	if process.runErr != nil && process.stream != nil {
		_ = process.stream.Close()
	}
	process.cleanupErr = process.cleanup(ctx)
	signalled := process.output.failure() != nil
	_, logger := logging.From(ctx)
	logger.Info("isolated.exit",
		"failed", process.runErr != nil, "output_exceeded", signalled)
	close(process.done)
}

// cleanup kills descendants, confirms cgroup emptiness and then removes the image.
//
// Returns error while resource ownership must be retained for retry.
//
// Safe for concurrent use by multiple goroutines.
func (process *WorkerProcess) cleanup(ctx context.Context) error {
	process.mutex.Lock()
	defer process.mutex.Unlock()
	if process.group != nil {
		closeCtx, cancel := sandboxwire.CleanupContext(ctx)
		err := process.group.closeGroup(closeCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	if process.image != nil {
		if err := process.image.Close(); err != nil {
			return err
		}
	}
	if err := process.cleanupWatchdog(ctx); err != nil {
		return err
	}
	if process.serviceGroup != nil {
		closeCtx, cancel := sandboxwire.CleanupContext(ctx)
		err := process.serviceGroup.closeGroup(closeCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	if process.checkpoint != nil {
		if err := process.checkpoint.Close(); err != nil {
			return err
		}
		process.checkpoint = nil
	}
	if process.releaseAdmission != nil {
		process.releaseAdmission()
		process.releaseAdmission = nil
	}
	process.released = true
	_, logger := logging.From(ctx)
	logger.Info("isolated.cleanup")
	return nil
}

// prepareServiceParent reserves the service group for a top-level launch and records its
// checkpoint, or validates a launch into an existing group.
//
// Takes config (WorkerConfig) which is host-private launch policy.
// Takes parent (*Group) which is nil for a top-level launch.
//
// Returns *Group which the worker's own group is created under.
// Returns error when the reservation or the record fails, or when a checkpoint is
// requested for a launch into an existing group.
func (process *WorkerProcess) prepareServiceParent(ctx context.Context, config WorkerConfig, parent *Group) (*Group, error) {
	if parent != nil {
		if config.Checkpoint != nil {
			return nil, ErrInvalidLimits
		}
		return parent, nil
	}
	var err error
	process.serviceGroup, err = newServiceGroup(ctx, config.CgroupParent, config.Limits, config.Tenant)
	if err != nil {
		return nil, err
	}
	if err := process.persistLaunchCheckpoint(ctx, config); err != nil {
		return nil, err
	}
	return process.serviceGroup, nil
}

// workerOutput shares one budget between captured diagnostics and charged script output.
type workerOutput struct {
	// cancel stops the worker on output overflow.
	cancel context.CancelFunc

	// data holds the captured diagnostic bytes.
	data []byte

	// limit holds the maximum combined output bytes.
	limit int

	// used tracks the consumed output budget.
	used int

	// mutex guards concurrent output writes.
	mutex sync.Mutex

	// exceeded is true once the output budget overflows.
	exceeded bool
}

var _ io.Writer = (*workerOutput)(nil)

// Write captures only the remaining budget and terminates on the first excess byte.
//
// Takes data ([]byte) which comes from native stdout or stderr.
//
// Returns int which is the retained byte count.
// Returns error when the combined budget is exceeded.
//
// Safe for concurrent use; the output mutex serialises writes.
func (output *workerOutput) Write(data []byte) (int, error) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	if output.exceeded {
		return 0, ErrWorkerOutput
	}
	retained := min(len(data), output.limit-output.used)
	output.data = append(output.data, data[:retained]...)
	output.used += retained
	if retained != len(data) {
		output.exceeded = true
		output.cancel()
		return retained, ErrWorkerOutput
	}
	return retained, nil
}

// LaunchWorker starts a verified, confined worker process.
//
// Takes config (WorkerConfig) which supplies host-approved authority and finite limits.
//
// Returns *WorkerProcess which owns resources, including on partial failure.
// Returns error when any prerequisite or launch step fails.
func LaunchWorker(ctx context.Context, config WorkerConfig) (*WorkerProcess, error) {
	return launchConfinedProcess(ctx, config, &workerAdmission, nil, nil)
}

// LaunchWorkerInGroup starts a source worker beneath a pinned aggregate parent. The
// caller retains the parent until this process has completed cleanup.
//
// Takes config (WorkerConfig) which is the approved configuration.
// Takes parent (*Group) which is the mandatory aggregate group.
//
// Returns a process owner, including partial failures, without a sibling-group fallback.
func LaunchWorkerInGroup(ctx context.Context, config WorkerConfig, parent *Group) (*WorkerProcess, error) {
	if parent == nil {
		return nil, ErrInvalidLimits
	}
	return launchConfinedProcess(ctx, config, &workerAdmission, nil, parent)
}

// launchConfinedProcess owns the shared fail-closed process lifecycle.
//
// Takes config (WorkerConfig) which is the launch configuration.
// Takes gate (*admissionTable) which is the role-specific admission gate.
// Takes inherited ([]*os.File) which holds the trusted inherited handles.
// Takes parent (*Group) which is the optional aggregate parent.
//
// Returns an owner even on partial failure; callers must close every returned owner.
func launchConfinedProcess(ctx context.Context, config WorkerConfig, gate *admissionTable, inherited []*os.File, parent *Group) (*WorkerProcess, error) {
	return launchNativeProcess(ctx, config, gate, inherited, parent, true)
}

// launchNativeProcess shares resource ownership between guarded work and its supervisor.
//
// Takes config (WorkerConfig) which is the trusted launch policy.
// Takes gate (*admissionTable) which is the role-specific admission gate.
// Takes inherited ([]*os.File) which holds the trusted inherited handles.
// Takes parent (*Group) which is the optional aggregate parent.
// Takes guarded (bool) which selects whether this is protected work rather than a
// watchdog.
//
// Returns retained ownership on every partial failure without an unguarded fallback.
func launchNativeProcess(ctx context.Context, config WorkerConfig, gate *admissionTable, inherited []*os.File, parent *Group, guarded bool) (*WorkerProcess, error) {
	if ctx == nil {
		return nil, errors.New("missing worker context")
	}
	ctx, logger := logging.From(ctx)
	if config.Lifetime == 0 {
		config.Lifetime = defaultWorkerLifetime
	}
	if config.OutputBytes == 0 {
		config.OutputBytes = defaultWorkerOutput
	}
	if config.Lifetime < 0 || config.OutputBytes < 1 || config.OutputBytes > maximumWorkerOutput {
		return nil, ErrInvalidLimits
	}
	if err := validateTenant(config.Tenant); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithTimeoutCause(ctx, config.Lifetime, sandboxwire.ErrLifetimeEnded)
	releaseAdmission, err := gate.acquire(lifetime, config.Tenant)
	if err != nil {
		cancel()
		return nil, err
	}
	var process WorkerProcess
	process.watchLifetime(lifetime)
	process.cancel = cancel
	process.releaseAdmission = releaseAdmission
	logger.Info("isolated.admitted", "tenant", config.Tenant, "guarded", guarded)
	process.done = make(chan struct{})
	process.output = &workerOutput{
		cancel: cancel, data: nil, limit: config.OutputBytes, used: 0,
		mutex: sync.Mutex{}, exceeded: false,
	}
	command, err := process.prepare(lifetime, config, inherited, parent, guarded)
	if err != nil {
		process.finish(lifetime, err)
		return &process, errors.Join(process.runErr, process.cleanupErr)
	}
	logger.Info("isolated.launch", "tenant", config.Tenant, "guarded", guarded,
		"images", config.ImageStore != nil)
	process.watch(lifetime, command)
	return &process, nil
}
