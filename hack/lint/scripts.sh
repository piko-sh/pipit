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

# hack/lint/scripts.sh - Lint all bash scripts in hack/ with shellcheck
#
# Usage:
#   ./hack/lint/scripts.sh

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Array of script paths to lint.
SCRIPTS=()

# Count of failed scripts.
FAILED=0

# verify_shellcheck checks that shellcheck is installed.
# Returns:
#   Exits with code 1 if not found
verify_shellcheck() {
    if ! pipit::util::verify_binary "shellcheck" "brew install shellcheck OR apt install shellcheck"; then
        exit 1
    fi
}

# find_scripts locates all bash scripts in the hack/ directory.
# Globals:
#   PIPIT_ROOT - Read
#   SCRIPTS - Set to array of script paths
find_scripts() {
    while IFS= read -r -d '' script; do
        SCRIPTS+=("$script")
    done < <(find "${PIPIT_ROOT}/hack" -name "*.sh" -type f -print0 | sort -z)

    pipit::log::info "Found ${#SCRIPTS[@]} scripts to lint"
}

# lint_scripts runs shellcheck on each script.
# Globals:
#   SCRIPTS - Read
#   FAILED - Incremented for each failing script
lint_scripts() {
    for script in "${SCRIPTS[@]}"; do
        local relative_path
        relative_path=$(pipit::util::relative_path "$script")

        if shellcheck -x -s bash -S warning -e SC1091 "$script" 2>&1; then
            pipit::log::success "$relative_path"
        else
            pipit::log::error "$relative_path"
            FAILED=$((FAILED + 1))
        fi
    done
}

# print_summary displays shellcheck results.
# Globals:
#   SCRIPTS - Read
#   FAILED - Read
print_summary() {
    pipit::log::blank
    pipit::log::header "Summary"
    pipit::log::info "Total scripts: ${#SCRIPTS[@]}"
    pipit::log::info "Passed: $((${#SCRIPTS[@]} - FAILED))"
    pipit::log::info "Failed: $FAILED"

    if [[ $FAILED -gt 0 ]]; then
        pipit::log::blank
        pipit::log::error "Some scripts have shellcheck issues."
        pipit::log::info "Run shellcheck directly on failing scripts for details:"
        pipit::log::detail "shellcheck -x -s bash <script>"
        exit 1
    fi

    pipit::log::success "All scripts passed shellcheck!"
}

# main runs shellcheck on all hack/ scripts.
# Arguments:
#   $@ - Unused
main() {
    verify_shellcheck

    pipit::log::header "Linting hack/ scripts with shellcheck"

    find_scripts
    pipit::log::blank
    lint_scripts
    print_summary
}

main "$@"
