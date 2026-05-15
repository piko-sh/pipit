---
title: "Isolated execution"
description: "Set up experimental Linux workers, sessions, and crash recovery."
section: "How-to guides"
order: 3
---

# Isolated execution

The isolated APIs and `pipit isolated` run source in a separate Linux worker.
This feature is experimental; read [execution security](security.md) before
using it with untrusted input.

## Host prerequisites

You need:

- Linux amd64 or arm64 with user namespaces, cgroup v2 memory/CPU/task controls
  and `cgroup.kill`, process namespaces, seccomp syscall filtering, and pidfd
  process handles.
  The filesystem broker also needs Landlock ABI 8, as described in its guide.
- An explicitly delegated cgroup parent; Pipit does not create delegation for you.
- Static worker and watchdog executables approved by the host.
- An absolute state directory on trusted local storage, owned by the host user
  with mode `0700`. Pipit rejects it when group or other permission bits are set.
  Keep it outside any filesystem grants, including mount aliases.

Pipit refuses to launch if required controls are missing. Build the executables
from the checkout:

```sh
CGO_ENABLED=0 go build -o ./bin/pipit-worker ./cmd/pipit-worker
CGO_ENABLED=0 go build -o ./bin/pipit-watchdog ./cmd/pipit-watchdog
```

Record their SHA-256 digests through your trusted build or review process. Paths,
digests, tenant identities, grants, and limits must come from host policy. A hash
provided by the script does not approve an executable.

## Prepare a local systemd session

For a local trial, use a Linux host with a systemd user manager that supports
`DelegateSubgroup`. The manager must be allowed to delegate `cpu`, `memory`, and
`pids` controllers. If your host disables user namespaces or delegation, ask its
administrator to enable them before continuing.

Build the CLI from the same checkout:

```sh
go build -o ./bin/pipit ./cmd/pipit
```

From the checkout, open a shell in a new service with its own delegated cgroup:

```sh
systemd-run --user --pty --wait --collect --unit=pipit-local-trial \
  --property='Delegate=cpu memory pids' --property=DelegateSubgroup=supervisor \
  --working-directory="$PWD" /bin/bash
```

Run the following inside that shell. It enables controllers only in the new
service's cgroup and creates private state storage:

```sh
membership=$(sed -n 's/^0:://p' /proc/self/cgroup)
case "$membership" in
  */pipit-local-trial.service/supervisor) ;;
  *) echo 'Run this inside the pipit-local-trial service.' >&2; exit 1 ;;
esac
CGROUP_PARENT="/sys/fs/cgroup${membership%/supervisor}"
printf '+cpu +memory +pids\n' > "$CGROUP_PARENT/cgroup.subtree_control"
WORKER="$PWD/bin/pipit-worker"
WATCHDOG="$PWD/bin/pipit-watchdog"
WORKER_SHA256=$(sha256sum "$WORKER" | cut -d' ' -f1)
WATCHDOG_SHA256=$(sha256sum "$WATCHDOG" | cut -d' ' -f1)
STATE_DIR=$(mktemp -d /tmp/pipit-state.XXXXXX)
export PATH="$PWD/bin:$PATH"
```

For this trial, you are choosing to trust the binaries you just built. In a
service, obtain approved digests from your build or review process instead.
Keep this shell open for the commands below. After the trial, run the recovery
command below before entering `exit`. Keep the state directory if recovery fails.

## Run from the CLI

The local setup above defines these variables. For an existing host setup, set
`WORKER`, `WATCHDOG`, `CGROUP_PARENT`, and `STATE_DIR` to absolute paths. Set
`WORKER_SHA256` and `WATCHDOG_SHA256` to the approved 64-digit SHA-256 values:

```sh
pipit isolated \
  --worker "$WORKER" --worker-sha256 "$WORKER_SHA256" \
  --watchdog "$WATCHDOG" --watchdog-sha256 "$WATCHDOG_SHA256" \
  --cgroup-parent "$CGROUP_PARENT" --state-dir "$STATE_DIR" \
  --tenant example --imports math \
  -e 'import "math"
math.Sqrt(49)'
```

Expected scalar output is `7`. For a complete source file, replace `-e` and its
source with `--entrypoint main - < script.go`. Flags must precede the positional `-`.
Directories, module fetching, bytecode, and script arguments are unsupported.
Custom host symbols registered in the CLI are not forwarded to the worker.

Use the same host flags with `--check` to run a fixed live probe. It checks launch,
execution, and cleanup. Passing this check confirms that the worker can run on
this host; it does not replace a security review.

## Embed a worker

For a Go host, pass the same policy to `NewIsolatedWorker(ctx, config)` using
[IsolatedConfig](../isolated.go). The required fields are `WorkerPath`,
`WorkerSHA256`, `WatchdogPath`, `WatchdogSHA256`, `LinuxCgroupParent`,
`StateDirectory`, and `Tenant`. Digests are `[32]byte` values. Set `Imports` to
select the packages scripts may import.

Failed construction returns an error. If it contains an `IsolatedCleanupError`,
retain that error and call `Retry(ctx)` with a fresh context to finish cleanup.
After successful construction, always call `Close`, including after evaluation
errors. If `Close` fails, keep the worker and retry cleanup a limited number of times. Avoid
launching replacement workers until cleanup completes.

A worker accepts one `Eval(source)` or `EvalFile(source, entrypoint)` submission.
Repeated use fails. The constructor context governs its lifetime. Results contain
output within the configured limit and an optional JSON scalar value. Pipit returns
the result only after the worker exits and cleanup succeeds. `Diagnostics()` contains untrusted
native output.

The supplied worker includes standard-library providers. Select imports explicitly
for a narrow grant; an empty import list uses the provider's pure defaults.
Package registration does not override native confinement, so operations requiring
unavailable OS resources can still fail.

## Limits

Zero values for lifetime, output, memory, CPU, and tasks select defaults. The
other limits are fixed:

| Resource | One-shot worker | Session |
|---|---|---|
| Compilation / execution | 5 seconds / 2 seconds | Per submission: 5 seconds / 2 seconds |
| Total lifetime, including admission/startup | 17 seconds | 15 minutes maximum |
| Memory / CPU bandwidth / native tasks | 256 MiB / one CPU / 64 | Same |
| Script output plus native diagnostics | 64 KiB | 1 MiB cumulative |
| Source | 1 MiB per submission | 1 MiB per submission, 4 MiB cumulative |
| Idle time / submissions | One submission | 60 seconds / 1,024 submissions |

A non-zero memory limit must be at least 1 MiB and page-aligned. CPU must be
between 10 and 64,000 milli-CPUs, tasks between 1 and 4,096, and output at most
1 MiB.

Pipit limits worker launches using reservations for each tenant under the delegated
cgroup parent. Failed cleanup keeps those reservations and can block further work
for that tenant.
Host storage access is synchronous; process deadlines cannot interrupt a blocked
host filesystem syscall. Use trusted local storage for state and executable images.

The CLI displays terminal control characters visibly on a terminal. Redirected
output preserves original bytes and must be treated as untrusted by its consumer.

## Sessions

`NewIsolatedSession(ctx, config)` retains imports and declarations between
`Submit(submissionContext, source)` calls. Give each session one tenant; do not
pool it across tenants. Grants cannot change after construction.

Submit one request at a time. Once a submission is accepted, any failure ends the
session, including a syntax error or cancellation. Handle constructor cleanup
errors as described for workers, and close every successfully created session.
Successful submissions leave the worker running, so `Close` is still required. `Done` indicates exit and an initial cleanup attempt,
not successful final cleanup.

For a CLI session, use the same flags as the run example and replace `-e` and its
source with `--repl`. Each line is a submission. Use `:begin` and `:end` on separate
lines for multiline input, `:help` for help, and `:quit` to exit. There is no reset,
file loading, or saved history. Filesystem grants are unavailable in sessions.

## Recover after a host crash

Each launch records recovery state beneath `<state-dir>/<tenant-hash>/<launch>/`,
where the tenant directory is named from a SHA-256 hash of the tenant, and removes
the launch record on clean closure. After a host crash, recover using the same host
configuration and executable approvals:

```sh
pipit isolated recover \
  --worker "$WORKER" --worker-sha256 "$WORKER_SHA256" \
  --watchdog "$WATCHDOG" --watchdog-sha256 "$WATCHDOG_SHA256" \
  --cgroup-parent "$CGROUP_PARENT" --state-dir "$STATE_DIR" \
  --tenant example --imports math
```

Recovery checks that the host and policy match the saved record. It stops the
processes from that launch, removes their executable copies, releases reserved
resources, and deletes recovered records. It skips records still held by another
host process. A policy change can prevent recovery. Filesystem records
also require the original broker flags and root grants.

For embedding, acquire `NewIsolatedRecovery(ctx, config)` or
`NewIsolatedFilesystemRecovery(ctx, config)`, then call `Recover(ctx)`. Construction
claims the records but does not clean them up. If recovery fails, keep the recovery
handle and retry. `Close` releases any claims that were not recovered, so call it
only when you stop retrying; it returns an error only when a claim cannot be
released. One recovery handles at most 64 records per tenant. Do not delete state records or cgroups
by hand while a live owner may need them.

To grant file access, see [filesystem isolation](filesystem-isolation.md).
