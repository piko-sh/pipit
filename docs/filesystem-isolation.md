---
title: "Filesystem isolation"
description: "Grant experimental isolated scripts bounded access to named filesystem roots."
section: "How-to guides"
order: 4
---

# Filesystem isolation

`NewIsolatedFilesystemWorker` pairs a source worker with a separately confined
filesystem broker. Scripts use `pipit/fs` to access directories that the host names and grants. This API is
experimental; follow the prerequisites and cleanup rules in
[isolated execution](isolated-execution.md).

The broker also requires Linux Landlock ABI 8. Build it from the checkout
and approve its digest independently:

```sh
CGO_ENABLED=0 go build -o ./bin/pipit-filesystem-broker ./cmd/pipit-filesystem-broker
```

## Grant roots

The CLI uses `--broker`, `--broker-sha256`, and repeated
`--root name=path:rights` flags. Rights are `r` (read), `w` (replace), and `l` (list).
Save this script as `read.go`. It reads from the root named `data`:

```go
package main

import "pipit/fs"

func answer() string {
	data, err := fs.Read("data", "message.txt", 4096)
	if err != nil {
		return "read failed"
	}
	return string(data)
}
```

Set the worker variables as described in [isolated execution](isolated-execution.md#run-from-the-cli).
Set `BROKER` and `DATA_DIR` to absolute paths and `BROKER_SHA256` to the approved
digest. Create `message.txt` inside the granted data directory:

```sh
printf 'hello from the broker' > "$DATA_DIR/message.txt"
pipit isolated \
  --worker "$WORKER" --worker-sha256 "$WORKER_SHA256" \
  --watchdog "$WATCHDOG" --watchdog-sha256 "$WATCHDOG_SHA256" \
  --cgroup-parent "$CGROUP_PARENT" --state-dir "$STATE_DIR" \
  --tenant example \
  --broker "$BROKER" --broker-sha256 "$BROKER_SHA256" \
  --root "data=$DATA_DIR:r" --entrypoint answer - < read.go
```

Expected output is `"hello from the broker"`, a JSON string.

For embedding, `IsolatedFilesystemConfig` combines the worker policy with
`BrokerPath`, `BrokerSHA256`, and `Roots`. Each `FilesystemRoot` has a `Name`,
absolute `HostPath`, and `Rights` bitmask. A config holds at most 64 roots. Pass it to
`NewIsolatedFilesystemWorker(ctx, config)` and call `EvalFile(source, "answer")` with the script
contents.
Follow the [constructor-error and cleanup rules](isolated-execution.md#embed-a-worker).
The worker accepts one submission.

Rights are independent. Root names start with a lower-case letter, use only
lower-case letters, digits, `_`, and `-`, and are at most 64 bytes. The constructor adds `pipit/fs`; do not add it to ordinary
restricted or isolated import lists. Scripts receive root names, not host paths or
native filesystem handles.

Keep roots and state storage on trusted local filesystems. State storage must be
outside every granted tree, including mount aliases. Grant trees must not be
rearranged by untrusted host processes.

## Script operations

The `pipit/fs` package supplies:

```text
Read(root, path string, maximum int) ([]byte, error)
Write(root, path string, data []byte) error
List(root, path string, maximum int) (names []string, skipped int, err error)
```

Once Pipit accepts a submission, any failed file operation ends it, even if the
script handles the error. This includes a denied operation and a missing file. In
this example, a failed read still fails the submission instead of returning
`"read failed"` as a successful result.

Paths must be relative to a named root, at most 4,096 bytes and 64 components
deep, with components of at most 255 bytes. Components that end in `.` or a space,
contain control characters or `\:<>"|?*`, or are Windows device names such as `CON`
or `NUL` are rejected. Symlink and mount traversal are rejected.
`.` is accepted only for listing the root. The case-insensitive `.pipit-stage-`
prefix is reserved in every path component; scripts cannot access staging entries.

| Operation | Behaviour |
|---|---|
| `Read` | Returns a bounded prefix of a regular file; no consistent snapshot guarantee |
| `Write` | Stages and creates or replaces one regular file with a single link; parent directories must exist and the filesystem must support `O_TMPFILE` |
| `List` | Returns bounded names and a skipped count; no pagination or snapshot guarantee |

Writes do not update the existing inode in place. Replacement files use private
permissions rather than inheriting previous metadata. If an error occurs after the replacement becomes visible,
the new file may remain in place. Read-only, write-only, and list-only grants remain distinct.

## Bounds and cleanup

Each read or write is at most 64 KiB; each list is at most 256 names. Default
cumulative budgets are 100 calls, 4 MiB read reservations, 4 MiB write reservations,
and 4,096 directory entries. A failed operation, or one that returns less data, still uses its reserved budget.
Positive limits can only reduce the defaults. If reducing `Limits.Calls` below
four, also reduce `Limits.Outstanding` to fit.

Calls are serialised. Source execution and all filesystem calls share the worker's
execution deadline. The source, broker, and their supervisors share aggregate
memory, CPU, and task limits. Native diagnostic budgets remain per process.

Before replacing a file, the broker saves a recovery record. Closing the worker
stops the broker and removes its leftover temporary files. Cleanup keeps files
that were already replaced and leaves unrelated files alone. If cleanup fails,
Pipit returns an error. Keep the worker handle and retry cleanup.

After a host crash, use `NewIsolatedFilesystemRecovery` or `pipit isolated recover`
with the original worker policy, broker approval, and root grants. See the
[recovery instructions](isolated-execution.md#recover-after-a-host-crash).

Filesystem-enabled sessions, HTTPS grants, and custom brokers are not available.
