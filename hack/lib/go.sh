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

# hack/lib/go.sh - Go toolchain helpers for pipit scripts
#
# This file should be sourced, not executed directly.
# All functions are namespaced with pipit::go::

# Prevent double-sourcing
if [[ -n "${_PIPIT_GO_LOADED:-}" ]]; then
    return 0
fi
readonly _PIPIT_GO_LOADED=1

# The Go command, overridable for toolchain experiments.
GO="${GO:-go}"

# The import path of this repository's root module.
readonly PIPIT_MODULE="pipit.sh/pipit"

# pipit::go::integration_dirs lists every integration suite module directory, relative
# to PIPIT_ROOT. The suites are one module each, and four scripts need the same list:
# discovering it here keeps a newly added suite from being linted and vetted but never
# actually tested.
# Globals:
#   PIPIT_ROOT - Read
# Outputs:
#   Writes one module directory per line, such as tests/integration/apps
pipit::go::integration_dirs() {
    local suite
    for suite in "${PIPIT_ROOT}"/tests/integration/*/; do
        [[ -f "${suite}go.mod" ]] || continue
        printf '%s\n' "tests/integration/$(basename "${suite%/}")"
    done
}

# pipit::go::with_local_replace runs a command in a module with a temporary
# replace directive pointing at the working copy, then drops it again.
#
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
#   $2 - Replacement target, relative to that module directory
#   $@ - Command to run inside the module
# Returns:
#   The command's exit status, after the replace has been dropped
pipit::go::with_local_replace() {
    local dir="$1"
    local target="$2"
    shift 2

    local required=()
    while IFS= read -r module; do
        [[ -n "$module" ]] && required+=("$module")
    done < <(pipit::go::repository_modules)

    local self="${PIPIT_MODULE}"
    [[ "$dir" != "." ]] && self="${PIPIT_MODULE}/${dir}"

    local existing
    existing="$(pipit::go::existing_replaces "$dir")"

    local status=0
    (
        cd "${PIPIT_ROOT}/${dir}" || exit 1
        local added=() module suffix
        for module in "${required[@]}"; do
            [[ "$module" == "$self" ]] && continue
            grep -qxF "$module" <<<"$existing" && continue
            suffix="${module#"${PIPIT_MODULE}"}"
            "${GO}" mod edit -replace "${module}=${target}${suffix}"
            added+=("$module")
        done
        "$@"
        local inner=$?
        for module in "${added[@]+"${added[@]}"}"; do
            "${GO}" mod edit -dropreplace "${module}"
        done
        exit "${inner}"
    ) || status=$?

    return "${status}"
}

# pipit::go::existing_replaces prints the module paths a module already replaces, so a
# temporary replace never removes one the module committed.
# Globals:
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
# Outputs:
#   Writes one module path per line
pipit::go::existing_replaces() {
    local dir="$1"

    grep -oE "^(replace[[:space:]]+|[[:space:]]+)[^[:space:]]+[[:space:]]+=>" \
        "${PIPIT_ROOT}/${dir}/go.mod" |
        sed -E 's/^(replace)?[[:space:]]*//; s/[[:space:]]+=>$//' |
        sort -u
}

# pipit::go::repository_modules prints every module path this repository defines.
# Globals:
#   PIPIT_MODULE - Read
# Outputs:
#   Writes one module path per line
pipit::go::repository_modules() {
    local directory

    while IFS= read -r directory; do
        if [[ "$directory" == "." ]]; then
            printf '%s\n' "${PIPIT_MODULE}"
        else
            printf '%s/%s\n' "${PIPIT_MODULE}" "$directory"
        fi
    done < <(pipit::go::module_dirs)
}

# pipit::go::module_dirs prints every module directory in the repository, relative to
# PIPIT_ROOT, with the root module written as ".".
# Globals:
#   PIPIT_ROOT - Read
# Outputs:
#   Writes one module directory per line, sorted
pipit::go::module_dirs() {
    local manifest directory

    while IFS= read -r manifest; do
        directory="${manifest%/go.mod}"
        directory="${directory#"${PIPIT_ROOT}"}"
        directory="${directory#/}"
        printf '%s\n' "${directory:-.}"
    done < <(pipit::util::find_go_modules) | sort
}

# pipit::go::path_to_root returns the relative path from a module directory back to
# PIPIT_ROOT.
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
# Outputs:
#   The relative path, such as ../..
pipit::go::path_to_root() {
    local directory="$1" up="" segment

    if [[ "$directory" == "." ]]; then
        printf '%s\n' "."
        return 0
    fi

    while IFS= read -r segment; do
        [[ -n "$segment" ]] && up="../${up}"
    done < <(tr '/' '\n' <<<"$directory")
    printf '%s\n' "${up%/}"
}

# pipit::go::build compiles a package for one platform.
# Arguments:
#   $1 - Output file
#   $2 - Package to build
#   $3 - GOOS (defaults to the host)
#   $4 - GOARCH (defaults to the host)
#   $5 - ldflags (defaults to none)
# Outputs:
#   Logs the resulting binary size
pipit::go::build() {
    local output="$1"
    local package="$2"
    local goos="${3:-$(pipit::util::host_os)}"
    local goarch="${4:-$(pipit::util::host_arch)}"
    local ldflags="${5:-}"

    pipit::util::ensure_dir "$(dirname "$output")"

    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        "${GO}" build -trimpath -ldflags "$ldflags" -o "$output" "$package"

    local size
    size=$(du -h "$output" | cut -f1)
    pipit::log::success "$(pipit::util::relative_path "$output") (${goos}/${goarch}, ${size})"
}
