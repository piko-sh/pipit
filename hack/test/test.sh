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
#   ./hack/test/test.sh spill-torture      Most locals spilled, every body compiled twice
#   ./hack/test/test.sh golden             Golden disassembly snapshots
#   ./hack/test/test.sh integration        The integration suites
#   ./hack/test/test.sh isolation          Linux sandbox tests, no skip fallback
#   ./hack/test/test.sh gotoolchain        The Go toolchain's own run-mode tests
#   ./hack/test/test.sh all                quick, safe and golden
#   ./hack/test/test.sh reuse-all          Every licensed-reuse lane and gate
#   ./hack/test/test.sh wasm               The js/wasm lane, run under node
#   ./hack/test/test.sh inline-audit       Which dispatch handlers js/wasm inlines
#   ./hack/test/test.sh inline-audit-update  Re-record them

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

# The integration suite that holds the licensed-reuse hazard tests and the store census.
readonly BYTECODE_SUITE=tests/integration/bytecode

# Repetitions of each test in the hazards lane. The races the hazard tests probe depend on
# timing, so a release check can raise it.
readonly HAZARD_COUNT="${PIPIT_HAZARD_COUNT:-1}"

# CENSUS_SKIP names tests whose bounds the census instrumentation invalidates. It changes
# two allocation counts and slows the unwinding tests beyond their wall-clock limits.
readonly CENSUS_SKIP='TestDeferTrivialScalarArgsZeroAlloc|TestASMInlineCallMixedKindReturns|TestR5NativeBlockedDefer|TestMainReturnUnwindingIsBounded|TestHostCancellationAndDeadlineRunTheStoppedDefers'

# WASM_PACKAGES lists the packages run in full under js/wasm.
readonly WASM_PACKAGES=(./internal/isa/ ./internal/mem/ ./internal/compile/passes/ ./internal/compile/inline/ ./internal/verify/ ./internal/leakcheck/)

# WASM_ENGINE_TESTS selects the interpreter tests run under js/wasm.
readonly WASM_ENGINE_TESTS='FastPath|Global|SliceByte|StructField|TinyLeaf|TypedElement|ReflectValue|ReflectFree|PlainSlice|ElementField'

# WASM_ENGINE_SKIP names tests whose bounds hold only for the native build.
readonly WASM_ENGINE_SKIP='TestStructFieldTypeAllocatesNothing'

# Array of failed suite names.
FAILED=()

# Every lane function below returns non-zero when any step it runs fails, by recording
# each step's status explicitly rather than relying on errexit. run_suite calls a lane in
# an if condition, where bash ignores errexit inside the function, so a failing step
# followed by a passing one would otherwise report the lane as passed.

# run_modules runs go test in every module directory, carrying on past a failing one.
# Globals:
#   TEST_MODULE_DIRS - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $@ - Flags for go test
# Returns:
#   1 when any module failed, 0 otherwise
run_modules() {
    local status=0 dir
    for dir in "${TEST_MODULE_DIRS[@]}"; do
        pipit::log::info "go test $* ($dir)"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test "$@" ./... ) || status=1
    done
    return "$status"
}

# run_suites runs go test in every integration suite module, carrying on past a failing
# one.
# Globals:
#   INTEGRATION_DIRS - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Comma-separated build tags, always including integration
#   $@ - Further flags for go test
# Returns:
#   1 when any suite failed, 0 otherwise
run_suites() {
    local tags="$1"
    shift

    local status=0 dir
    for dir in "${INTEGRATION_DIRS[@]}"; do
        pipit::log::info "go test -tags $tags $* ($dir)"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test -tags "$tags" "$@" ./... ) || status=1
    done
    return "$status"
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

# run_suite_dir_tagged runs go test in one integration suite module with its own build
# tags.
# Globals:
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Suite directory, relative to PIPIT_ROOT
#   $2 - Comma-separated build tags, including integration
#   $@ - Further flags for go test
run_suite_dir_tagged() {
    local dir="$1"
    local tags="$2"
    shift 2

    ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test -tags "$tags" "$@" ./... )
}

# run_bench_tools runs the tests of the benchmark commands and of the harness they share.
# Arguments:
#   $@ - Extra flags passed to go test
run_bench_tools() {
    (
        cd "${PIPIT_ROOT}/tests/bench" &&
            "${GO}" test "$@" ./cross_language/cmd/... ./cross_language/harness/ &&
            "${GO}" test -tags pipit_opcode_census "$@" ./cross_language/cmd/dispatch_report/
    )
}

# test_quick runs the full suite of both modules in the default lane.
test_quick() {
    local status=0
    run_modules || status=1
    run_bench_tools || status=1
    return "$status"
}

# test_short runs the short suite of both modules.
test_short() {
    local status=0
    run_modules -short || status=1
    run_bench_tools -short || status=1
    return "$status"
}

# test_safe runs the pure-Go engine lane.
#
# The interpreter has a second build lane that swaps every unsafe fast path for an
# allocating equivalent. It is the portability and toolchain-breakage contract, so
# a change that only passes on the default lane is not proven.
test_safe() {
    local status=0
    "${GO}" test -short -tags safe "$ENGINE_PACKAGES" || status=1
    PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration,safe -short || status=1
    return "$status"
}

# test_race runs the interpreter suite under the race detector.
test_race() {
    local status=0
    "${GO}" test -race -count=1 -timeout=40m "$ENGINE_PACKAGES" || status=1
    PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration -race -count=1 -timeout=40m || status=1
    return "$status"
}

# test_torture_default runs the GC-torture lane over the default build.
test_torture_default() {
    local status=0
    GOGC=1 GODEBUG=clobberfree=1 "${GO}" test -count=1 -timeout=40m "$ENGINE_PACKAGES" || status=1
    GOGC=1 GODEBUG=clobberfree=1 PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration -count=1 -timeout=40m || status=1
    return "$status"
}

# test_torture_safe runs the GC-torture lane over the pure-Go build.
test_torture_safe() {
    local status=0
    GOGC=1 GODEBUG=clobberfree=1 "${GO}" test -tags safe -count=1 -timeout=40m "$ENGINE_PACKAGES" || status=1
    GOGC=1 GODEBUG=clobberfree=1 PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration,safe -count=1 -timeout=40m || status=1
    return "$status"
}

# test_torture_paranoid runs GC stress and arena poisoning together.
test_torture_paranoid() {
    local status=0
    GOGC=1 GODEBUG=clobberfree=1 "${GO}" test -tags pipit_arena_paranoid -count=1 -timeout=60m "$ENGINE_PACKAGES" || status=1
    GOGC=1 GODEBUG=clobberfree=1 PIPIT_SELFHOST_ARGS="-report ${SELFHOST_SCRATCH_REPORT}" run_suites integration,pipit_arena_paranoid -count=1 -timeout=60m || status=1
    return "$status"
}

# test_spill_torture runs the Go-parity corpus with most locals forced into the spill
# area.
test_spill_torture() {
    local status=0 limit
    for limit in 16 4 1; do
        PIPIT_FORCE_SPILL_LIMIT="$limit" run_suite_dir tests/integration/snippets -count=1 -timeout=40m || status=1
    done
    PIPIT_REGISTER_RETRY_CHECK=1 run_suite_dir tests/integration/golden -count=1 || status=1
    return "$status"
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


# test_reuse_audit runs the reuse audit over the interpreter and over the bytecode suite.
test_reuse_audit() {
    local status=0
    GODEBUG=asyncpreemptoff=1 "${GO}" test -tags pipit_reuse_audit -count=1 -timeout=40m ./internal/engine/... || status=1
    GODEBUG=asyncpreemptoff=1 \
        run_suite_dir_tagged "$BYTECODE_SUITE" integration,pipit_reuse_audit -count=1 -timeout=60m || status=1
    return "$status"
}

# test_census runs the bytecode suite in the store-census build.
test_census() {
    run_suite_dir_tagged "$BYTECODE_SUITE" integration,pipit_store_census \
        -count=1 -timeout=60m -skip "$CENSUS_SKIP"
}

# test_wasm runs the selected js/wasm tests under node.
# Globals:
#   WASM_PACKAGES - Read
#   WASM_ENGINE_TESTS - Read
#   WASM_ENGINE_SKIP - Read
#   PIPIT_ROOT - Read
# Returns:
#   1 when any step failed, 0 otherwise
test_wasm() {
    local status=0 runner
    pipit::util::verify_binary node "https://nodejs.org/" || return 1
    runner="$("${GO}" env GOROOT)/lib/wasm/go_js_wasm_exec"
    if [[ ! -x "$runner" ]]; then
        pipit::log::error "$("${GO}" version) has no js/wasm test runner at ${runner}"
        return 1
    fi

    pipit::log::info "go test, js/wasm (${WASM_PACKAGES[*]})"
    GOOS=js GOARCH=wasm "${GO}" test -exec="$runner" -count=1 "${WASM_PACKAGES[@]}" || status=1

    pipit::log::info "go test, js/wasm (internal/engine: ${WASM_ENGINE_TESTS})"
    GOOS=js GOARCH=wasm "${GO}" test -exec="$runner" -count=1 -short -timeout=20m \
        -run "$WASM_ENGINE_TESTS" -skip "$WASM_ENGINE_SKIP" ./internal/engine/ || status=1

    pipit::log::info "go test, js/wasm (cmd/wasm)"
    ( cd "${PIPIT_ROOT}/cmd/wasm" && GOOS=js GOARCH=wasm "${GO}" test -exec="$runner" -count=1 -timeout=90s . ) || status=1

    return "$status"
}

# test_inline_audit reports which of the js/wasm dispatch loop's ranked operations reach
# their handler without a Go call, and fails when one recorded as inlined no longer is.
# Arguments:
#   $@ - Passed to the audit. -update records the inlined set instead of comparing with it
test_inline_audit() {
    "${GO}" run ./hack/inlineaudit "$@"
}

# test_hazards runs the whole bytecode suite and the interpreter's TestReuseHazard* tests.
# The suite's licensed-reuse tests (arena_reuse_*_test.go) drive guest programs through
# every route that arena reuse could corrupt. The interpreter's test prefix is described
# in register_arena_licence_hazard_helpers_test.go.
test_hazards() {
    local status=0 variant
    for variant in default godispatch audit paranoid race; do
        local tags="" flags=()
        case "$variant" in
            audit) tags=",pipit_reuse_audit" ;;
            paranoid) tags=",pipit_arena_paranoid" ;;
            race) flags=(-race) ;;
        esac
        (
            case "$variant" in
                godispatch) export PIPIT_TEST_GO_DISPATCH=1 ;;
                audit) export GODEBUG=asyncpreemptoff=1 ;;
                paranoid) export GOGC=1 GODEBUG=clobberfree=1 ;;
            esac

            local variant_status=0
            pipit::log::info "hazards, ${variant}: ${BYTECODE_SUITE}"
            run_suite_dir_tagged "$BYTECODE_SUITE" "integration,pipit_testhooks${tags}" "${flags[@]}" \
                -count="$HAZARD_COUNT" -timeout=60m || variant_status=1

            pipit::log::info "hazards, ${variant}: internal/engine"
            "${GO}" test "${flags[@]}" -tags "pipit_testhooks${tags}" -count="$HAZARD_COUNT" -timeout=30m \
                -run '^TestReuseHazard' ./internal/engine/ || variant_status=1
            exit "$variant_status"
        ) || status=1
    done
    return "$status"
}

# test_abort_inject runs the interpreter with pipit_testhooks, which arms an abort at each
# injection site in turn. It also runs the bytecode suite's budget-abort, goroutine-panic
# and soft-limit cases, the abort-injection sweep (TestAbortInjectionSweep in
# abort_injection_test.go), and the fail-closed tests that stop a VM between a Lock and its
# deferred Unlock (TestStopInTheLockWindow* in abort_unwind_inject_test.go).
test_abort_inject() {
    local status=0
    "${GO}" test -tags pipit_testhooks -count=1 -timeout=40m ./internal/engine/... || status=1

    pipit::log::info "abort cases"
    run_suite_dir "$BYTECODE_SUITE" -count=5 -timeout=30m \
        -run 'TestBudgetAbortInACallback|TestGoroutinePanicDoesNotFail|TestSoftLimitServes' || status=1

    local variant
    for variant in default audit paranoid godispatch; do
        local tags="integration,pipit_testhooks"
        case "$variant" in
            audit) tags+=",pipit_reuse_audit" ;;
            paranoid) tags+=",pipit_arena_paranoid" ;;
        esac
        (
            unset PIPIT_TEST_GO_DISPATCH GODEBUG GOGC
            case "$variant" in
                audit) export GODEBUG=asyncpreemptoff=1 ;;
                paranoid) export GOGC=1 GODEBUG=clobberfree=1 ;;
                godispatch) export PIPIT_TEST_GO_DISPATCH=1 ;;
            esac
            pipit::log::info "abort-injection sweep, ${variant}"
            run_suite_dir_tagged "$BYTECODE_SUITE" "$tags" -count=1 -timeout=30m \
                -run '^(TestAbortInjectionSweep|TestStopInTheLockWindow.*)$'
        ) || status=1
    done
    return "$status"
}

# test_reuse_all runs the interpreter and the integration suites, every licensed-reuse
# gate, and the structural checks (hack/licencecheck).
# Globals:
#   ENGINE_PACKAGES - Read
#   FAILED - Read
#   PIPIT_ROOT - Read
test_reuse_all() {
    local self="${PIPIT_ROOT}/hack/test/test.sh"

    run_suite "Interpreter" "${GO}" test -count=1 -timeout=40m "$ENGINE_PACKAGES"
    run_suite "Integration suites" "$self" integration
    run_suite "Reuse audit" "$self" reuse-audit
    run_suite "Reuse hazards" "$self" hazards
    run_suite "Store census" "$self" census
    run_suite "Abort injection" "$self" abort-inject
    run_suite "Licence checker" "${PIPIT_ROOT}/hack/lint/vet.sh" licence

    pipit::log::header "Summary"
    pipit::log::info "Failed: ${#FAILED[@]}"

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        for name in "${FAILED[@]}"; do
            pipit::log::detail "- $name"
        done
        exit 1
    fi

    pipit::log::success "All licensed-reuse lanes passed"
}

# run_suite executes one suite and tracks the result.
# Globals:
#   FAILED - Modified with failed suite names
# Arguments:
#   $1 - Display name for the suite
#   $@ - Function or command to execute, with its arguments
run_suite() {
    local name="$1"
    shift

    pipit::log::header "$name"

    if "$@"; then
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
    spill-torture        Run with most locals spilled and every body compiled twice
    golden               Compare the golden disassembly snapshots
    golden-update        Re-record the golden disassembly snapshots
    integration          Run the integration suites
    isolation            Run the Linux sandbox tests with no skip fallback
    gotoolchain          Run the Go toolchain corpus through bin/pipit
    gotoolchain-update   Re-record the passing Go toolchain set
    all                  Run quick, safe and golden
    reuse-audit          Run the reuse audit over the interpreter and the bytecode suite
    hazards              Run the bytecode suite and the reuse-hazard tests in five builds
    census               Run the bytecode suite under the store census (about 10 GB)
    abort-inject         Run with abort injection: the engine, the abort cases and the sweep
    reuse-all            Run the interpreter, the integration suites, every licensed-reuse
                         lane and the structural checks
    wasm                 Run the js/wasm lane under node
    inline-audit         Check which dispatch handlers the js/wasm loop inlines
    inline-audit-update  Re-record the handlers the js/wasm loop inlines

Examples:
    $(basename "$0") quick
    $(basename "$0") torture-paranoid
    PIPIT_HAZARD_COUNT=5 $(basename "$0") hazards
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
            local status=0
            test_torture_default || status=1
            test_torture_safe || status=1
            return "$status"
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

        spill-torture)
            test_spill_torture
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

        reuse-audit)
            test_reuse_audit
            ;;

        hazards)
            test_hazards
            ;;

        census)
            test_census
            ;;

        abort-inject)
            test_abort_inject
            ;;

        reuse-all)
            test_reuse_all
            ;;

        wasm)
            test_wasm
            ;;

        inline-audit)
            test_inline_audit
            ;;

        inline-audit-update)
            test_inline_audit -update
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

# The lanes run when the script is executed. hack/test/test-accounting.sh sources it to
# check the failure accounting with stub suites.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
