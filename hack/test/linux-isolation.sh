#!/bin/bash
# Copyright 2026 PolitePixels Limited
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# This project stands against fascism, authoritarianism, and all forms of
# oppression. We built this to empower people, not to enable those who would
# strip others of their rights and dignity.

# hack/test/linux-isolation.sh - Run the Linux sandbox tests with no skip fallback
#
# Usage:
#   ./hack/test/linux-isolation.sh

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The transient systemd unit the tests run inside.
readonly ISOLATION_UNIT="pipit-isolation-tests"

# Commands whose confined binaries the suite needs built ahead of time, as
# "name:module directory:package". The worker registers the standard library, so it is its
# own module and builds from its own directory.
readonly ISOLATION_BINARIES=(
    "worker:cmd/pipit-worker:."
    "filesystem-broker:.:./cmd/pipit-filesystem-broker"
    "host-watchdog:.:./cmd/pipit-watchdog"
)

# Test binaries the suite needs compiled ahead of time.
readonly ISOLATION_TEST_BINARIES=(
    "linux.test:./internal/sandboxlinux"
    "worker.test:./internal/sandboxworker"
    "embedding.test:./internal/sandboxhost"
    "cli.test:./cmd/pipit/cli"
)

# run_delegated runs the test binaries inside the delegated cgroup.
#
# Arguments:
#   $1 - Directory holding the pre-built binaries
run_delegated() {
    local artifacts="$1"

    local membership
    membership=$(sed -n 's/^0:://p' /proc/self/cgroup)

    case "$membership" in
        */"${ISOLATION_UNIT}".service/supervisor) ;;
        *) pipit::log::fatal "Unexpected test cgroup: $membership" ;;
    esac

    local parent="/sys/fs/cgroup${membership%/supervisor}"
    printf '+cpu +memory +pids\n' > "$parent/cgroup.subtree_control"

    export PIPIT_TEST_CGROUP_PARENT="$parent"
    export PIPIT_TEST_WORKER_BINARY="$artifacts/worker"
    export PIPIT_TEST_FILESYSTEM_BROKER_BINARY="$artifacts/filesystem-broker"
    export PIPIT_TEST_HOST_WATCHDOG_BINARY="$artifacts/host-watchdog"

    "$artifacts/linux.test" -test.v -test.run '^TestCgroupNative' -test.count=3 -test.timeout=90s
    "$artifacts/linux.test" -test.v -test.run '^TestNative' -test.count=3 -test.timeout=90s
    "$artifacts/worker.test" -test.v -test.run '^TestNativeWorker(Source|Supervised)$' -test.count=3 -test.timeout=90s -pipit-worker-test-binary "$artifacts/worker"
    "$artifacts/worker.test" -test.v -test.run '^TestNativeFilesystemRelay$' -test.count=3 -test.timeout=90s
    "$artifacts/embedding.test" -test.v -test.run '^TestIsolatedEmbeddingNative$' -test.count=3 -test.timeout=90s
    "$artifacts/cli.test" -test.v -test.run '^TestRunIsolatedNative$' -test.count=3 -test.timeout=90s
}

# build_artifacts compiles every binary the delegated run needs.
# Globals:
#   ISOLATION_BINARIES - Read
#   ISOLATION_TEST_BINARIES - Read
# Arguments:
#   $1 - Directory to build into
build_artifacts() {
    local artifacts="$1"

    for entry in "${ISOLATION_BINARIES[@]}"; do
        local name="${entry%%:*}" rest="${entry#*:}"
        pipit::log::info "building ${name}"
        ( cd "${PIPIT_ROOT}/${rest%%:*}" && CGO_ENABLED=0 "${GO}" build -o "$artifacts/${name}" "${rest#*:}" )
    done

    for entry in "${ISOLATION_TEST_BINARIES[@]}"; do
        pipit::log::info "compiling ${entry%%:*}"
        CGO_ENABLED=0 "${GO}" test -c -o "$artifacts/${entry%%:*}" "${entry##*:}"
    done
}

# run_under_systemd builds the artefacts and re-execs this script in a scope.
# Globals:
#   ISOLATION_UNIT - Read
#   PIPIT_ROOT - Read
run_under_systemd() {
    if ! pipit::util::verify_binary "systemd-run" "install systemd, or run the suite on a machine with it"; then
        exit 1
    fi

    local artifacts
    artifacts=$(mktemp -d "${TMPDIR:-/tmp}/${ISOLATION_UNIT}.XXXXXXXX")
    # shellcheck disable=SC2064
    trap "rm -rf -- '${artifacts}'" EXIT

    build_artifacts "$artifacts"

    systemd-run --user --wait --pipe --collect --unit="${ISOLATION_UNIT}" \
        --setenv="PIPIT_REQUIRE_BROKER_LANDLOCK=${PIPIT_REQUIRE_BROKER_LANDLOCK:-1}" \
        --setenv="PIPIT_TEST_REQUIRE_USERNS=${PIPIT_TEST_REQUIRE_USERNS:-1}" \
        --property='Delegate=cpu memory pids' --property=DelegateSubgroup=supervisor \
        --property=RuntimeMaxSec=180s --property=TimeoutStopSec=5s \
        /bin/bash "${PIPIT_ROOT}/hack/test/linux-isolation.sh" --delegated "$artifacts"
}

# main either runs inside the delegated cgroup or sets one up.
# Arguments:
#   $1 - --delegated when re-executed inside the scope
#   $2 - The artefact directory, when delegated
main() {
    if [[ "${1:-}" == "--delegated" ]]; then
        run_delegated "$2"
        return 0
    fi

    pipit::log::header "Linux isolation tests (no skip fallback)"
    run_under_systemd
    pipit::log::footer
    pipit::log::success "Linux isolation tests passed"
}

main "$@"
