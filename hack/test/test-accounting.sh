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

# hack/test/test-accounting.sh - Check that hack/test/test.sh reports every failing suite
#
# Sources test.sh, replaces the commands its lanes run with stubs that pass or fail on
# demand, and checks that a lane exits non-zero and names the failing suite whenever any
# step fails, including a failing step followed by a passing one. Runs no Go tests.
#
# Usage:
#   ./hack/test/test-accounting.sh

# Names of the stub steps that fail in the current case, space separated.
FAILING_STEPS=""

# Count of failed checks.
CHECKS_FAILED=0

# A scratch repository root holding the module and suite directories test.sh walks, with
# hack/ linked to the real scripts.
ACCOUNTING_ROOT="$(mktemp -d)"
trap 'rm -rf "${ACCOUNTING_ROOT}"' EXIT
ln -s "$(cd "$(dirname "$0")/.." && pwd -P)" "${ACCOUNTING_ROOT}/hack"
for directory in cmd/pipit sdk/stdlib sdk/selfhost tests/facade; do
    mkdir -p "${ACCOUNTING_ROOT}/${directory}"
done
for suite in bytecode golden language; do
    mkdir -p "${ACCOUNTING_ROOT}/tests/integration/${suite}"
    touch "${ACCOUNTING_ROOT}/tests/integration/${suite}/go.mod"
done
PIPIT_ROOT="${ACCOUNTING_ROOT}"

# stub_go stands in for the go command. It prints the step it ran, named after the
# directory it runs in and its last argument, and fails when the case names that step.
# Globals:
#   FAILING_STEPS - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $@ - go test arguments. The last is the package pattern.
# Returns:
#   1 when the step is named in FAILING_STEPS, 0 otherwise
stub_go() {
    local directory="${PWD#"${PIPIT_ROOT}"}"
    directory="${directory#/}"
    local step="go:${directory:-.}:${*: -1}"
    echo "step ${step}"
    [[ " ${FAILING_STEPS} " != *" ${step} "* ]]
}
export GO=stub_go

# shellcheck source=./test.sh
source "$(dirname "$0")/test.sh"
cd "${PIPIT_ROOT}" || exit 1

# check_case runs a lane function in a subshell with the named steps failing.
# Globals:
#   FAILING_STEPS - Set
#   CHECKS_FAILED - Incremented on a failed check
# Arguments:
#   $1 - Description of the case
#   $2 - Steps that fail, space separated
#   $3 - Expected exit status, zero or nonzero
#   $4 - Text the lane's output must contain, or empty
#   $5 - Lane function to run
check_case() {
    local description="$1" failing="$2" expected="$3" wanted_text="$4" lane="$5"
    local output status=0
    output="$(FAILING_STEPS="$failing" "$lane" 2>&1)" || status=$?

    local passed=true
    if [[ "$expected" == "zero" && $status -ne 0 ]] || [[ "$expected" == "nonzero" && $status -eq 0 ]]; then
        passed=false
    fi
    if [[ -n "$wanted_text" && "$output" != *"$wanted_text"* ]]; then
        passed=false
    fi

    if [[ "$passed" == true ]]; then
        pipit::log::success "$description (exit $status)"
    else
        pipit::log::error "$description: exit $status, expected $expected exit and output containing '$wanted_text'"
        printf '%s\n' "$output" | sed 's/^/    /'
        CHECKS_FAILED=$((CHECKS_FAILED + 1))
    fi
}

# lane_all runs test_all, which exits, in its own subshell.
lane_all() {
    ( test_all )
}

# lane_safe runs the pure-Go lane.
lane_safe() {
    test_safe
}

# lane_modules runs the module loop.
lane_modules() {
    run_modules
}

# lane_hazards runs the hazards lane.
lane_hazards() {
    test_hazards
}

# lane_torture runs both GC-torture lanes as the torture command does.
lane_torture() {
    main torture
}

# main_accounting runs every case and reports the result.
main_accounting() {
    pipit::log::header "test.sh failure accounting"

    check_case "every suite passing" "" zero "All suites passed" lane_all
    check_case "a failing first module, later ones passing" "go:.:./..." nonzero "- Default lane" lane_all
    check_case "a failing middle suite of the pure-Go lane" "go:tests/integration/golden:./..." nonzero \
        "- Pure-Go lane" lane_all
    check_case "a failing engine step followed by passing suites" "go:.:./internal/..." nonzero \
        "- Pure-Go lane" lane_all
    check_case "the pure-Go lane on its own" "go:tests/integration/bytecode:./..." nonzero "" lane_safe
    check_case "the module loop carries on after a failure" "go:.:./..." nonzero \
        "step go:tests/facade:./..." lane_modules
    check_case "a failing hazards variant followed by passing ones" "go:.:./internal/engine/" nonzero "" lane_hazards
    check_case "a failing first torture lane followed by a passing one" "go:.:./internal/..." nonzero "" lane_torture
    check_case "every hazards variant passing" "" zero "" lane_hazards

    pipit::log::header "Summary"
    pipit::log::info "Failed checks: ${CHECKS_FAILED}"
    if [[ $CHECKS_FAILED -gt 0 ]]; then
        exit 1
    fi
    pipit::log::success "test.sh reports every failing suite"
}

main_accounting "$@"
