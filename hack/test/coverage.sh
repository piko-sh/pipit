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

# hack/test/coverage.sh - Measure test coverage and refresh the README badges
#
# Usage:
#   ./hack/test/coverage.sh total           Library module percentage
#   ./hack/test/coverage.sh cli-total       cmd/pipit module percentage
#   ./hack/test/coverage.sh update-badges   Measure both and rewrite README.md

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The README whose badges update-badges rewrites.
readonly README_FILE="${PIPIT_ROOT}/README.md"

# Packages excluded from the denominator.
readonly COVERAGE_EXCLUDE_PATTERN='(/internal/adapters/driven_system_symbols|/internal/schema/schemagen|/internal/leakcheck|/examples/)'

# deduplicate_coverage_profile merges duplicate blocks in a coverage profile.
#
# Arguments:
#   $1 - Input profile
#   $2 - Output profile
deduplicate_coverage_profile() {
    awk 'NR==1 { print; next }
    {
        block = $1
        if ($3 + 0 > max_count[block] + 0) {
            max_count[block] = $3 + 0
        }
        stmt_count[block] = $2
    }
    END {
        for (b in stmt_count) {
            printf "%s %s %d\n", b, stmt_count[b], max_count[b]
        }
    }' "$1" > "$2"
}

# module_coverage prints the total statement coverage of one module.
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
#   $2 - go list pattern
# Outputs:
#   Writes the percentage to stdout, or nothing on failure
# Returns:
#   1 when the module could not be measured
module_coverage() {
    local module_dir="$1"
    local pattern="$2"
    local profile_name
    profile_name=$(echo "$module_dir" | tr '/' '-')
    local profile="${TMPDIR:-/tmp}/pipit-coverage-${profile_name}.out"
    local deduped="${profile%.out}.deduped.out"
    local coverpkgs

    cd "${PIPIT_ROOT}/${module_dir}" || return 1
    rm -f "$profile" "$deduped"

    coverpkgs=$("${GO}" list "$pattern" | grep -vE "$COVERAGE_EXCLUDE_PATTERN" | paste -sd,)
    if [[ -z "$coverpkgs" ]]; then
        return 1
    fi

    if ! "${GO}" test -short -covermode=atomic -coverpkg="$coverpkgs" \
        -coverprofile="$profile" "$pattern" > /dev/null 2>&1; then
        return 1
    fi

    deduplicate_coverage_profile "$profile" "$deduped"
    "${GO}" tool cover -func="$deduped" 2>/dev/null |
        grep '^total:' | awk '{print $NF}' | tr -d '%'
}

# coverage_colour maps a percentage onto the shields.io colour ramp.
# Arguments:
#   $1 - Coverage percentage (numeric, for example 50.2)
# Outputs:
#   Writes the colour name to stdout
coverage_colour() {
    local percentage="${1%%.*}"

    if [[ -z "$percentage" ]]; then
        echo "lightgrey"
    elif [[ "$percentage" -lt 50 ]]; then
        echo "red"
    elif [[ "$percentage" -lt 65 ]]; then
        echo "orange"
    elif [[ "$percentage" -lt 75 ]]; then
        echo "yellow"
    elif [[ "$percentage" -lt 85 ]]; then
        echo "yellowgreen"
    elif [[ "$percentage" -lt 90 ]]; then
        echo "green"
    else
        echo "brightgreen"
    fi
}

# update_readme_badge rewrites one shields.io badge in place.
# Globals:
#   README_FILE - Read
# Arguments:
#   $1 - Badge label
#   $2 - Display value
#   $3 - Colour name
update_readme_badge() {
    local label="$1"
    local value="${2//%/%25}"
    local colour="$3"

    sed -i "s|${label}-[^?]*?|${label}-${value}-${colour}?|g" "$README_FILE"
}

# update_badges measures every module and rewrites the README badges to match.
update_badges() {
    local library cli

    pipit::log::info "measuring library coverage..."
    library=$(module_coverage "." "./..." || true)
    pipit::log::info "measuring cmd/pipit coverage..."
    cli=$(module_coverage "cmd/pipit" "./..." || true)

    if [[ -n "$library" ]]; then
        update_readme_badge "Library_Coverage" "$(printf '%.0f%%' "$library")" \
            "$(coverage_colour "$library")"
        pipit::log::detail "library:   ${library}%"
    else
        update_readme_badge "Library_Coverage" "unknown" "lightgrey"
        pipit::log::detail "library:   failed"
    fi

    if [[ -n "$cli" ]]; then
        update_readme_badge "CLI_Coverage" "$(printf '%.0f%%' "$cli")" \
            "$(coverage_colour "$cli")"
        pipit::log::detail "cmd/pipit: ${cli}%"
    else
        update_readme_badge "CLI_Coverage" "unknown" "lightgrey"
        pipit::log::detail "cmd/pipit: failed"
    fi

    pipit::log::success "README.md badges updated"
}

# main dispatches to the requested coverage mode.
# Arguments:
#   $1 - Mode to run: total, cli-total or update-badges
main() {
    local command="${1:-total}"

    case "$command" in
        total)
            module_coverage "." "./..."
            ;;

        cli-total)
            module_coverage "cmd/pipit" "./..."
            ;;

        update-badges)
            update_badges
            ;;

        help|--help|-h)
            pipit::log::info "Usage: hack/test/coverage.sh [total|cli-total|update-badges]"
            ;;

        *)
            pipit::log::fatal "Unknown command: $command (expected: total, cli-total, update-badges)"
            ;;
    esac
}

main "$@"
