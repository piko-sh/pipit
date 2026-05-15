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

# hack/test/test.sh - Run pipit's test suites in any of their lanes
#
# Usage:
#   ./hack/test/test.sh quick              Both modules, default lane (the default)
#   ./hack/test/test.sh short              Both modules, -short
#   ./hack/test/test.sh safe               The pure-Go engine lane
#   ./hack/test/test.sh race               The race detector
#   ./hack/test/test.sh torture            Both GC-torture lanes
#   ./hack/test/test.sh torture-paranoid   GC torture plus arena poisoning
#   ./hack/test/test.sh golden             Golden disassembly snapshots
#   ./hack/test/test.sh integration        The integration suites
#   ./hack/test/test.sh isolation          Linux sandbox tests, no skip fallback
#   ./hack/test/test.sh gotoolchain        The Go toolchain's own run-mode tests
#   ./hack/test/test.sh all                quick, safe and golden

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Module directories that carry their own test suite in the default lane.
readonly TEST_MODULE_DIRS=(. cmd/pipit sdk/stdlib sdk/selfhost tests/facade)

# The interpreter packages, which every specialised lane runs against.
readonly ENGINE_PACKAGES="./internal/..."

# Where the stress lanes write the self-hosting report. The lane rewrites the tracked
# docs/selfhost-report.md by default; only the integration lane should do that, so the
# safe, race and torture lanes point the harness at a scratch file instead.
readonly SELFHOST_SCRATCH_REPORT="${TMPDIR:-/tmp}/pipit-selfhost-report-$$.md"

# The integration suites, one module each, all behind the `integration` tag.
INTEGRATION_DIRS=()
while IFS= read -r suite; do
    INTEGRATION_DIRS+=("$suite")
done < <(pipit::go::integration_dirs)
readonly INTEGRATION_DIRS

# Array of failed suite names.
FAILED=()

# run_modules runs go test in every module directory.
# Globals:
#   TEST_MODULE_DIRS - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $@ - Flags for go test
run_modules() {
    for dir in "${TEST_MODULE_DIRS[@]}"; do
        pipit::log::info "go test $* ($dir)"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test "$@" ./... )
    done
}

# run_suites runs go test in every integration suite module.
# Globals:
#   INTEGRATION_DIRS - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Comma-separated build tags, always including integration
#   $@ - Further flags for go test
run_suites() {
    local tags="$1"
    shift

    for dir in "${INTEGRATION_DIRS[@]}"; do
        pipit::log::info "go test -tags $tags $* ($dir)"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test -tags "$tags" "$@" ./... )
    done
}

# run_suite_dir runs go test in one integration suite module.
# Globals:
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Suite directory, relative to PIPIT_ROOT
#   $@ - Further flags for go test
run_suite_dir() {
    local dir="$1"
    shift

    ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test -tags integration "$@" ./... )
}

# test_quick runs the full suite of both modules in the default lane.
test_quick() {
    run_modules
}

# test_short runs the short suite of both modules.
test_short() {
    run_modules -short
}

# test_safe runs the pure-Go engine lane.
#
# The interpreter has a second build lane that swaps every unsafe fast path for an
# allocating equivalent. It is the portability and toolchain-breakage contract, so
# a change that only passes on the default lane is not proven.
test_safe() {
    "${GO}" test -short -tags safe "$ENGINE_PACKAGES"
    PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration,safe -short
}

# test_race runs the interpreter suite under the race detector.
test_race() {
    "${GO}" test -race -count=1 -timeout=40m "$ENGINE_PACKAGES"
    PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration -race -count=1 -timeout=40m
}

# test_torture_default runs the GC-torture lane over the default build.
test_torture_default() {
    GOGC=1 GODEBUG=clobberfree=1 "${GO}" test -count=1 -timeout=40m "$ENGINE_PACKAGES"
    GOGC=1 GODEBUG=clobberfree=1 PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration -count=1 -timeout=40m
}

# test_torture_safe runs the GC-torture lane over the pure-Go build.
test_torture_safe() {
    GOGC=1 GODEBUG=clobberfree=1 "${GO}" test -tags safe -count=1 -timeout=40m "$ENGINE_PACKAGES"
    GOGC=1 GODEBUG=clobberfree=1 PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration,safe -count=1 -timeout=40m
}

# test_torture_paranoid runs GC stress and arena poisoning together.
test_torture_paranoid() {
    GOGC=1 GODEBUG=clobberfree=1 "${GO}" test -tags pipit_arena_paranoid -count=1 -timeout=60m "$ENGINE_PACKAGES"
    GOGC=1 GODEBUG=clobberfree=1 PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration,pipit_arena_paranoid -count=1 -timeout=60m
}

# test_golden compares the golden disassembly snapshots.
test_golden() {
    run_suite_dir tests/integration/golden -count=1
}

# test_golden_update re-records the golden disassembly snapshots.
test_golden_update() {
    PIPIT_GOLDEN_UPDATE=1 run_suite_dir tests/integration/golden -count=1
}

# test_integration runs the integration suites.
test_integration() {
    run_suites integration -count=1 -timeout=60m
}

# test_isolation runs the Linux sandbox tests with skips turned into failures.
test_isolation() {
    "${PIPIT_ROOT}/hack/test/linux-isolation.sh"
}

# run_gotoolchain drives the Go toolchain corpus and reports the suite's status.
#
# Arguments:
#   $@ - Extra environment assignments, passed through to the test binary
# Returns:
#   The test suite's exit status
run_gotoolchain() {
    "${PIPIT_ROOT}/hack/build/build.sh" current

    local status=0
    PIPIT_GOTOOLCHAIN=1 PIPIT_BIN="${PIPIT_ROOT}/bin/pipit" env "$@" \
        "${GO}" test -tags integration -count=1 -v -run TestGoToolchainRunTests \
        "${PIPIT_ROOT}/tests/integration/gotoolchain/..." 2>&1 \
        | { grep -Ev '^(=== (RUN|PAUSE|CONT)|\s*--- PASS)' || true; } || status=$?

    return "$status"
}

# test_gotoolchain runs the Go toolchain's own run-mode tests through bin/pipit.
test_gotoolchain() {
    run_gotoolchain
}

# test_gotoolchain_update re-records the passing set for the running Go version.
test_gotoolchain_update() {
    run_gotoolchain PIPIT_GOTOOLCHAIN_UPDATE=1
}

# run_suite executes one suite and tracks the result.
# Globals:
#   FAILED - Modified with failed suite names
# Arguments:
#   $1 - Display name for the suite
#   $2 - Function to execute
run_suite() {
    local name="$1"
    local suite="$2"

    pipit::log::header "$name"

    if "$suite"; then
        pipit::log::success "$name passed"
    else
        pipit::log::error "$name failed"
        FAILED+=("$name")
    fi

    pipit::log::footer
}

# test_all runs the suites that make up the "is everything OK" check.
# Globals:
#   FAILED - Read
test_all() {
    run_suite "Default lane" test_quick
    run_suite "Pure-Go lane" test_safe
    run_suite "Golden disassembly" test_golden

    pipit::log::header "Summary"
    pipit::log::info "Failed: ${#FAILED[@]}"

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        for name in "${FAILED[@]}"; do
            pipit::log::detail "- $name"
        done
        exit 1
    fi

    pipit::log::success "All suites passed"
}

# print_usage displays help information.
print_usage() {
    cat <<EOF
Usage: $(basename "$0") <command>

Commands:
    quick                Run both modules in the default lane (default)
    short                Run both modules with -short
    safe                 Run the interpreter in the pure-Go lane
    race                 Run the interpreter under the race detector
    torture              Run both GC-torture lanes
    torture-default      Run the GC-torture lane over the default build
    torture-safe         Run the GC-torture lane over the pure-Go build
    torture-paranoid     Run GC torture plus register-arena poisoning
    golden               Compare the golden disassembly snapshots
    golden-update        Re-record the golden disassembly snapshots
    integration          Run the integration suites
    isolation            Run the Linux sandbox tests with no skip fallback
    gotoolchain          Run the Go toolchain corpus through bin/pipit
    gotoolchain-update   Re-record the passing Go toolchain set
    all                  Run quick, safe and golden

Examples:
    $(basename "$0") quick
    $(basename "$0") torture-paranoid
EOF
}

# main handles command dispatch.
# Arguments:
#   $1 - Command to execute
main() {
    local command="${1:-quick}"

    case "$command" in
        quick)
            test_quick
            ;;

        short)
            test_short
            ;;

        safe)
            test_safe
            ;;

        race)
            test_race
            ;;

        torture)
            test_torture_default
            test_torture_safe
            ;;

        torture-default)
            test_torture_default
            ;;

        torture-safe)
            test_torture_safe
            ;;

        torture-paranoid)
            test_torture_paranoid
            ;;

        golden)
            test_golden
            ;;

        golden-update)
            test_golden_update
            ;;

        integration)
            test_integration
            ;;

        isolation)
            test_isolation
            ;;

        gotoolchain)
            test_gotoolchain
            ;;

        gotoolchain-update)
            test_gotoolchain_update
            ;;

        all)
            test_all
            ;;

        help|--help|-h)
            print_usage
            ;;

        *)
            pipit::log::error "Unknown command: $command"
            pipit::log::blank
            print_usage
            exit 1
            ;;
    esac
}

main "$@"
