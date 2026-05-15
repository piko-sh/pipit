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

# hack/generate/engine.sh - Generate the engine's opcode-keyed dispatch tables
#
# Usage:
#   ./hack/generate/engine.sh                       Run every generator, in order
#   ./hack/generate/engine.sh --validate            Check every generated file
#   ./hack/generate/engine.sh opcode-tables         Run one generator
#   ./hack/generate/engine.sh flat-switch --validate

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The package directory the generators must run from.
readonly ENGINE_DIR="${PIPIT_ROOT}/internal/engine"

# Generators in dependency order, as "name:file".
readonly ENGINE_GENERATORS=(
    "opcode-tables:gen_opcode_tables.go"
    "flat-switch:gen_flat_switch.go"
    "kind-families:gen_kind_families.go"
)

# run_generator runs one generator from the engine package directory.
# Globals:
#   ENGINE_DIR - Read
# Arguments:
#   $1 - Generator source file
#   $@ - Extra flags for the generator
run_generator() {
    local file="$1"
    shift

    ( cd "$ENGINE_DIR" && "${GO}" run "$file" "$@" )
}

# generator_file resolves a generator name to its source file.
# Globals:
#   ENGINE_GENERATORS - Read
# Arguments:
#   $1 - Generator name
# Outputs:
#   Writes the source file name to stdout
generator_file() {
    local name="$1"

    for entry in "${ENGINE_GENERATORS[@]}"; do
        if [[ "${entry%%:*}" == "$name" ]]; then
            echo "${entry##*:}"
            return 0
        fi
    done

    pipit::log::fatal "Unknown generator: $name (expected: opcode-tables, flat-switch, kind-families)"
}

# generate_all runs every generator in dependency order.
# Globals:
#   ENGINE_GENERATORS - Read
# Arguments:
#   $@ - Extra flags for the generators
generate_all() {
    local total=${#ENGINE_GENERATORS[@]}
    local current=0

    for entry in "${ENGINE_GENERATORS[@]}"; do
        current=$((current + 1))
        pipit::log::step "$current" "$total" "${entry%%:*}"
        run_generator "${entry##*:}" "$@"
    done
}

# main generates or validates the engine's dispatch tables.
# Arguments:
#   $1 - A generator name, or --validate, or nothing for all of them
#   $2 - --validate, when a generator was named
main() {
    local target="${1:-all}"
    local flags=()

    if [[ "$target" == "--validate" ]]; then
        target="all"
        flags=(-validate)
    elif [[ "${2:-}" == "--validate" ]]; then
        flags=(-validate)
    fi

    if [[ ${#flags[@]} -gt 0 ]]; then
        pipit::log::header "Validating the generated engine tables"
    else
        pipit::log::header "Generating the engine tables"
    fi

    if [[ "$target" == "all" ]]; then
        generate_all "${flags[@]+"${flags[@]}"}"
    else
        run_generator "$(generator_file "$target")" "${flags[@]+"${flags[@]}"}"
    fi

    pipit::log::footer

    if [[ ${#flags[@]} -gt 0 ]]; then
        pipit::log::success "engine tables are up to date"
    else
        pipit::log::success "engine table generation complete!"
    fi
}

main "$@"
