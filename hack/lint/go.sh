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

# hack/lint/go.sh - Lint every Go module in the workspace with golangci-lint
#
# Every module reads the same .golangci.yml at the repository root, so the
# layering rules under depguard are enforced here too; there is no separate
# layering target.
#
# A lint target is "dir|tags|goos/goarch", where empty fields mean the default
# build. Targets sit in three groups:
#   core         the root module (default build and each diagnostic build),
#                cmd/pipit, the sdk modules and tests/facade
#   integration  every integration suite, once per build-tag set its files use
#   extended     the safe and fuzz builds, cmd/wasm and the benchmarks
# A run without --only lints core and integration.
#
# Usage:
#   ./hack/lint/go.sh
#   ./hack/lint/go.sh --only integration
#   ./hack/lint/go.sh --only extended --fix

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Targets of the core group. The root module is linted once more under each diagnostic
# build tag, whose files the default build leaves out.
CORE_TARGETS=(".||" ".|pipit_testhooks|" ".|pipit_reuse_audit|" ".|pipit_arena_paranoid|" ".|pipit_store_census|"
    ".|pipit_opcode_census|" "cmd/pipit||" "sdk/extract||" "sdk/link||" "sdk/stdlib||" "sdk/selfhost||" "tests/facade||")

# Targets of the extended group.
EXTENDED_TARGETS=(".|safe|" ".|fuzz|" "cmd/wasm||js/wasm" "tests/bench|bench|" "tests/bench|crosslang|"
    "tests/bench|pipit_opcode_census|")

# Groups selected with --only; empty means core and integration.
ONLY_GROUPS=()

# Extra arguments forwarded to golangci-lint.
LINT_ARGS=()

# Targets to lint, filled by select_targets.
LINT_TARGETS=()

# Count of targets that reported a finding.
FAILED=0

# verify_golangci_lint checks that golangci-lint is installed.
# Returns:
#   Exits with code 1 if not found
verify_golangci_lint() {
    if ! pipit::util::verify_binary "golangci-lint" "https://golangci-lint.run/welcome/install/"; then
        exit 1
    fi
}

# parse_args reads the command line.
# Globals:
#   ONLY_GROUPS - Appended to for each --only
#   LINT_ARGS - Set to the forwarded arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)
                pipit::log::info "Usage: hack/lint/go.sh [--only core|integration|extended] [golangci-lint arguments]"
                pipit::log::detail "--only  Lint one group; repeat it for several"
                pipit::log::detail "--fix   Apply the fixes golangci-lint can make itself"
                exit 0
                ;;

            --only)
                [[ $# -ge 2 ]] || pipit::log::fatal "--only needs a group"
                ONLY_GROUPS+=("$2")
                shift 2
                ;;

            *)
                LINT_ARGS+=("$1")
                shift
                ;;
        esac
    done
}

# integration_targets prints one target per build-tag set an integration suite
# uses: integration always, plus integration,<tag> for each of crossarch, fuzz
# and safe that a file's build line names.
# Globals:
#   PIPIT_ROOT - Read
# Outputs:
#   Writes one target per line
integration_targets() {
    local suite tag
    while IFS= read -r suite; do
        printf '%s\n' "${suite}|integration|"
        for tag in crossarch fuzz safe; do
            if grep -rqsE "^//go:build .*\\b${tag}\\b" --include='*.go' "${PIPIT_ROOT}/${suite}"; then
                printf '%s\n' "${suite}|integration,${tag}|"
            fi
        done
    done < <(pipit::go::integration_dirs)
}

# select_targets fills LINT_TARGETS from the chosen groups.
# Globals:
#   ONLY_GROUPS - Read
#   LINT_TARGETS - Set
select_targets() {
    local groups=("${ONLY_GROUPS[@]+"${ONLY_GROUPS[@]}"}") group target
    if [[ ${#groups[@]} -eq 0 ]]; then
        groups=(core integration)
    fi
    for group in "${groups[@]}"; do
        case "$group" in
            core)
                LINT_TARGETS+=("${CORE_TARGETS[@]}")
                ;;

            integration)
                while IFS= read -r target; do
                    LINT_TARGETS+=("$target")
                done < <(integration_targets)
                ;;

            extended)
                LINT_TARGETS+=("${EXTENDED_TARGETS[@]}")
                ;;

            *)
                pipit::log::fatal "Unknown lint group: $group (want core, integration or extended)"
                ;;
        esac
    done
}

# lint_targets runs golangci-lint for each target.
# Globals:
#   LINT_TARGETS - Read
#   LINT_ARGS - Read
#   FAILED - Incremented for each failing target
lint_targets() {
    local total=${#LINT_TARGETS[@]}
    local current=0 target dir tags platform

    for target in "${LINT_TARGETS[@]}"; do
        current=$((current + 1))
        IFS='|' read -r dir tags platform <<<"$target"
        pipit::log::step "$current" "$total" "golangci-lint ${target}"

        local tag_args=()
        if [[ -n "$tags" ]]; then
            tag_args=(--build-tags "$tags")
        fi
        local env_args=()
        if [[ -n "$platform" ]]; then
            env_args=("GOOS=${platform%%/*}" "GOARCH=${platform##*/}")
        fi

        if ( cd "${PIPIT_ROOT}/${dir}" && env "${env_args[@]+"${env_args[@]}"}" golangci-lint run "${tag_args[@]+"${tag_args[@]}"}" "${LINT_ARGS[@]+"${LINT_ARGS[@]}"}" ./... ); then
            pipit::log::success "$target"
        else
            pipit::log::error "$target"
            FAILED=$((FAILED + 1))
        fi
    done
}

# print_summary displays the lint results.
# Globals:
#   LINT_TARGETS - Read
#   FAILED - Read
print_summary() {
    pipit::log::blank
    pipit::log::header "Summary"
    pipit::log::info "Total targets: ${#LINT_TARGETS[@]}"
    pipit::log::info "Passed: $((${#LINT_TARGETS[@]} - FAILED))"
    pipit::log::info "Failed: $FAILED"

    if [[ $FAILED -gt 0 ]]; then
        pipit::log::blank
        pipit::log::error "Some targets have lint findings."
        exit 1
    fi

    pipit::log::success "All targets passed golangci-lint!"
}

# main lints the selected targets.
# Arguments:
#   $@ - --only groups and arguments forwarded to golangci-lint
main() {
    parse_args "$@"
    verify_golangci_lint
    select_targets

    pipit::log::header "Linting Go modules with golangci-lint"

    lint_targets
    print_summary
}

main "$@"
