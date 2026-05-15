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

# hack/generate/all.sh - Run every code generator, or validate every generated file
#
# Usage:
#   ./hack/generate/all.sh              Regenerate everything
#   ./hack/generate/all.sh --validate   Fail when any generated file is stale

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Path to the generate directory.
readonly GENERATE_DIR="${PIPIT_ROOT}/hack/generate"

# Array of failed generator names.
FAILED=()

# run_generator executes one generator script and tracks the result.
# Globals:
#   FAILED - Modified with failed generator names
# Arguments:
#   $1 - Display name
#   $2 - Script path
#   $@ - Arguments for the script
run_generator() {
    local name="$1"
    local script="$2"
    shift 2

    pipit::log::header "$name"

    if "$script" "$@"; then
        pipit::log::success "$name"
    else
        pipit::log::error "$name"
        FAILED+=("$name")
    fi

    pipit::log::footer
}

# run_all runs every generator in dependency order.
# Globals:
#   GENERATE_DIR - Read
# Arguments:
#   $@ - Arguments forwarded to each generator
run_all() {
    run_generator "Engine dispatch tables" "${GENERATE_DIR}/engine.sh" "$@"
    run_generator "Dispatch assembly" "${GENERATE_DIR}/asmgen.sh" "$@"
    run_generator "Named-scalar pool tags" "${GENERATE_DIR}/symtab.sh" "$@"
    run_generator "FlatBuffers bindings" "${GENERATE_DIR}/flatc.sh" "$@"
    run_generator "Stdlib symbol tables" "${GENERATE_DIR}/symbols.sh" interp "$@"
    run_generator "Self-hosting symbol tables" "${GENERATE_DIR}/symbols.sh" selfhost "$@"
}

# print_summary displays the generator results.
# Globals:
#   FAILED - Read
print_summary() {
    pipit::log::blank
    pipit::log::header "Summary"
    pipit::log::info "Failed: ${#FAILED[@]}"

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        pipit::log::blank
        pipit::log::error "The following generators failed:"
        for name in "${FAILED[@]}"; do
            pipit::log::detail "- $name"
        done
        exit 1
    fi

    pipit::log::success "All generators succeeded!"
}

# main runs every generator.
# Arguments:
#   $1 - --validate to check the committed files instead of rewriting them
main() {
    local flags=()

    if [[ "${1:-}" == "--validate" ]]; then
        flags=(--validate)
        pipit::log::header "Validating every generated file"
    else
        pipit::log::header "Running every code generator"
    fi
    pipit::log::footer

    run_all "${flags[@]+"${flags[@]}"}"
    print_summary
}

main "$@"
