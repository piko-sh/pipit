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

# hack/generate/flatc.sh - Generate the bytecode FlatBuffers bindings from the schema
#
# Usage:
#   ./hack/generate/flatc.sh              Regenerate in place
#   ./hack/generate/flatc.sh --validate   Fail when the committed bindings are stale
#
# Environment:
#   PIPIT_FLATC      Path to flatc, otherwise .tools/flatc then PATH
#   PIPIT_GOIMPORTS  Path to goimports, otherwise GOPATH/bin then PATH
#
# Verified byte-identical with flatc 25.2.10 and 25.12.19.

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The schema that is the source of truth for the wire format.
readonly SCHEMA="internal/schema/bytecode.fbs"

# The directory holding the committed bindings.
readonly GEN_DIR="internal/schema/schemagen"

# flatc appends the schema's namespace to -o, so the output points one level up.
readonly OUT_PARENT="internal/schema"

# resolve_tool prints the first usable path for a tool.
# Arguments:
#   $1 - Display name
#   $2 - Environment override value, may be empty
#   $@ - Candidate paths, in order of preference
# Outputs:
#   Writes the resolved path to stdout
# Returns:
#   1 when no candidate is executable
resolve_tool() {
    local name="$1"
    local override="$2"
    shift 2

    if [[ -n "$override" ]]; then
        if [[ -x "$override" ]]; then
            echo "$override"
            return 0
        fi
        pipit::log::error "$name: $override is not executable"
        return 1
    fi

    local candidate
    for candidate in "$@"; do
        if [[ -n "$candidate" && -x "$candidate" ]]; then
            echo "$candidate"
            return 0
        fi
    done

    pipit::log::error "$name not found. Install it, or set the matching environment override."
    return 1
}

# generate_bindings renders the schema into a staging directory.
# Globals:
#   SCHEMA - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Staging directory
generate_bindings() {
    local staging="$1"

    local flatc goimports
    flatc=$(resolve_tool flatc "${PIPIT_FLATC:-}" \
        "${PIPIT_ROOT}/.tools/flatc" "$(command -v flatc || true)")
    goimports=$(resolve_tool goimports "${PIPIT_GOIMPORTS:-}" \
        "$("${GO}" env GOPATH)/bin/goimports" "$(command -v goimports || true)")

    "$flatc" --go -o "$staging" "${PIPIT_ROOT}/${SCHEMA}"
    "$goimports" -w "${staging}/schemagen"
}

# validate_bindings fails when the committed bindings no longer match the schema.
# Globals:
#   GEN_DIR - Read
#   SCHEMA - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Staging directory holding freshly generated bindings
validate_bindings() {
    local staging="$1"

    if diff -r "${PIPIT_ROOT}/${GEN_DIR}" "${staging}/schemagen" >/dev/null 2>&1; then
        pipit::log::success "${GEN_DIR} matches ${SCHEMA}"
        return 0
    fi

    pipit::log::error "${GEN_DIR} is stale: it no longer matches ${SCHEMA}"
    pipit::log::info "Regenerate with: make generate-flatc"
    diff -r "${PIPIT_ROOT}/${GEN_DIR}" "${staging}/schemagen" >&2 || true
    exit 1
}

# install_bindings replaces the committed bindings with the staged ones.
# Globals:
#   GEN_DIR - Read
#   OUT_PARENT - Read
#   SCHEMA - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Staging directory holding freshly generated bindings
install_bindings() {
    local staging="$1"

    rm -rf "${PIPIT_ROOT:?}/${GEN_DIR}"
    cp -r "${staging}/schemagen" "${PIPIT_ROOT}/${OUT_PARENT}/"
    pipit::log::success "regenerated ${GEN_DIR} from ${SCHEMA}"
}

# main generates or validates the FlatBuffers bindings.
# Arguments:
#   $1 - --validate to check the committed bindings instead of rewriting them
main() {
    local validate=0

    if [[ "${1:-}" == "--validate" ]]; then
        validate=1
    elif [[ -n "${1:-}" ]]; then
        pipit::log::fatal "Unknown argument: $1 (expected: --validate)"
    fi

    if (( validate )); then
        pipit::log::header "Validating the FlatBuffers bindings"
    else
        pipit::log::header "Generating the FlatBuffers bindings"
    fi

    local staging
    staging=$(mktemp -d)
    # shellcheck disable=SC2064
    trap "rm -rf '${staging}'" EXIT

    generate_bindings "$staging"

    if (( validate )); then
        validate_bindings "$staging"
    else
        install_bindings "$staging"
    fi

    pipit::log::footer
}

main "$@"
