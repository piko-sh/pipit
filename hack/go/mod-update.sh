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

# hack/go/mod-update.sh - Update every module's direct dependencies to their latest
#
# Usage:
#   ./hack/go/mod-update.sh            Update every module
#   ./hack/go/mod-update.sh cmd/pipit  Update one module directory

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Count of modules whose go.mod the run changed.
UPDATED=0

# module_directories lists every module directory, relative to PIPIT_ROOT, root first.
# Globals:
#   PIPIT_ROOT - Read
# Outputs:
#   One module directory per line
module_directories() {
    local file directory
    while IFS= read -r file; do
        directory="$(pipit::util::relative_path "$(dirname "$file")")"
        if [[ "$directory" == "$(dirname "$file")" ]]; then
            directory="."
        fi
        printf '%s\n' "$directory"
    done < <(pipit::util::find_go_modules) | sort -u
}

# direct_dependencies lists a module's direct requirements, skipping this project's
# own unpublished modules.
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
# Outputs:
#   One module path per line
direct_dependencies() {
    awk '
        /^require \(/ { block = 1; next }
        block && /^\)/ { block = 0; next }
        /\/\/ indirect/ { next }
        block && NF >= 2 { print $1; next }
        /^require [^(]/ && NF >= 3 { print $2 }
    ' "${PIPIT_ROOT}/$1/go.mod" | grep -v '^pipit\.sh/' || true
}

# needs_local_replace reports whether a module requires the unpublished root module
# without already replacing it.
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
# Returns:
#   0 when a temporary replace is required
needs_local_replace() {
    local modfile="${PIPIT_ROOT}/$1/go.mod"

    if ! grep -qE "^[[:space:]]*${PIPIT_MODULE//./\\.} v" "$modfile"; then
        return 1
    fi
    if grep -qE "${PIPIT_MODULE//./\\.} =>" "$modfile"; then
        return 1
    fi
    return 0
}

# upgrade_dependencies runs one `go get <path>@latest` per direct requirement. It runs
# inside the module directory, which pipit::go::with_local_replace has already entered.
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT, for logging only
upgrade_dependencies() {
    local directory="$1"
    local dependency

    while IFS= read -r dependency; do
        [[ -z "$dependency" ]] && continue
        pipit::log::detail "$dependency"
        "${GO}" get "${dependency}@latest"
    done < <(direct_dependencies "$directory")
}

# update_module upgrades one module, behind a temporary replace when it needs one.
# Globals:
#   UPDATED - Modified when the module's go.mod changed
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Module directory, relative to PIPIT_ROOT
update_module() {
    local directory="$1"
    local before after

    if [[ -z "$(direct_dependencies "$directory")" ]]; then
        pipit::log::info "$directory has no direct dependencies"
        return 0
    fi

    pipit::log::info "updating $directory"
    before="$(cat "${PIPIT_ROOT}/${directory}/go.mod")"

    if needs_local_replace "$directory"; then
        pipit::go::with_local_replace "$directory" "$(pipit::go::path_to_root "$directory")" \
            upgrade_dependencies "$directory"
    else
        ( cd "${PIPIT_ROOT}/${directory}" && upgrade_dependencies "$directory" )
    fi

    after="$(cat "${PIPIT_ROOT}/${directory}/go.mod")"
    if [[ "$before" != "$after" ]]; then
        UPDATED=$((UPDATED + 1))
        pipit::log::success "$directory updated"
    fi
}

# main updates every module, or the one named on the command line.
# Globals:
#   UPDATED - Read
# Arguments:
#   $1 - Optional module directory, relative to PIPIT_ROOT
main() {
    pipit::log::header "Updating direct dependencies"

    if [[ -n "${1:-}" ]]; then
        update_module "${1%/}"
    else
        local directory
        while IFS= read -r directory; do
            update_module "$directory"
        done < <(module_directories)
    fi

    pipit::log::footer
    pipit::log::success "${UPDATED} module(s) updated"
}

main "$@"
