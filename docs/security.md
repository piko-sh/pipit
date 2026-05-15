---
title: "Execution security"
description: "Understand host access, cooperative restrictions, and experimental process isolation."
section: "Explanation"
order: 2
---

# Execution security

Pipit runs code with different levels of host access depending on how you create
the interpreter. Ordinary embedding and the `run`, `eval`, and `repl` commands
execute in the host process. Registering the standard library exposes native
operations, including filesystem and network access.

Isolation is work in progress. The Linux backend implements native controls, but
the security boundary has not completed its audits and independent review. Do not
treat Pipit as an established hostile-code execution service.

## Execution modes

| Mode | Entry point | Current boundary |
|---|---|---|
| Trusted | `NewInterpreter` | Your process and the host functions you register |
| Restricted | `NewRestrictedInterpreter` | Cooperative in-process limits, import selection, and capability checks |
| Isolated | `NewIsolatedWorker` / `NewIsolatedSession` | Experimental Linux worker process with native confinement |

Trusted execution suits scripts you control or review. Restricted execution
reduces accidental access and resource use, but still relies on code cooperating
within the host process. Isolated execution moves the script into a separate
process. It refuses to run when required native controls are absent.

## Imports and native functions

An ordinary interpreter registers no host packages by default. Standard-library
providers and custom symbol tables determine what a script can import. Registered
functions run native Go, and returned objects may carry access to host resources.

Import selection and CLI capability gates do not intercept every route to a native
side effect. A custom helper can access resources independently of its import name.
Expose only the functions and objects your application intends scripts to use.

## Restricted execution

[Restrict imports and capabilities](api.md#restrict-imports-and-capabilities) shows
a runnable embedding example with standard-library providers.

Each submission has fresh state. Concurrent submissions are rejected. Results
contain bounded output and an optional JSON scalar; arbitrary host objects cannot
cross the result boundary.

With providers, an empty import list selects the available packages in
`PureSurface()`. A nil capability hook installs `DenyCapabilityHook`. Restricted
execution disables features including goroutines, channels, unsafe operations,
and panic/recover. Zero resource limits select finite defaults; they do not disable
limits. See the [restricted defaults](api-reference.md#restricted-limits) for
values and units.

## Resource controls and capability gates

Interpreter limits apply to specific operations. An allocation cap counts elements
in one allocation; it is not a limit on total process memory. Instruction-cost
metering measures interpreted execution, not compilation or arbitrary native work.
A timeout can stop interpreted loops but cannot forcibly interrupt a native call
already in progress.

The CLI adds a process exit backstop when a native call outlasts its timeout.
`run` and `eval` return status 124 on timeout. Cost metering is opt-in.

`--gate=network,disk,exec,env` requests approval for selected standard-library
operations. Saved approvals belong in host-controlled storage. They record approval for local
source and invocation details. They do not authenticate all external dependencies. See the [CLI reference](cli.md#capability-gating) for usage.

## Experimental isolation

Isolated execution requires Linux amd64/arm64 with user namespaces and pidfd
support, approved static worker and watchdog executables, delegated cgroup v2
controls, and a private state directory. The launcher uses namespaces,
privilege removal, syscall filtering, and process resource limits. Other platforms
return `ErrIsolatedUnavailable`.

See [isolated execution](isolated-execution.md) for setup and cleanup responsibilities.
A separately confined [filesystem broker](filesystem-isolation.md) provides named-root
file access. Filesystem sessions and HTTP/custom brokers are not available.

Report vulnerabilities using the [security policy](../SECURITY.md).
