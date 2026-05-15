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

# hack/go/mod-tidy.sh - Tidy the go.mod files that can be tidied
#
# Usage:
#   ./hack/go/mod-tidy.sh

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# tidy_module tidies one module, behind temporary replaces when it needs them.
# Globals:
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Step number
#   $2 - Total steps
#   $3 - Module directory, relative to PIPIT_ROOT
tidy_module() {
    local step="$1" total="$2" dir="$3"

    pipit::log::step "$step" "$total" "go mod tidy ${dir}"

    if [[ "$dir" == "." ]]; then
        ( cd "${PIPIT_ROOT}" && "${GO}" mod tidy )
    else
        pipit::go::with_local_replace "$dir" "$(pipit::go::path_to_root "$dir")" "${GO}" mod tidy
    fi

    pipit::log::success "$dir"
}

# main tidies every module in the repository.
# Arguments:
#   $@ - Unused
main() {
    local directories=() directory step=0 total

    while IFS= read -r directory; do
        directories+=("$directory")
    done < <(pipit::go::module_dirs)

    total="${#directories[@]}"

    pipit::log::header "Tidying go.mod files"

    for directory in "${directories[@]}"; do
        step=$((step + 1))
        tidy_module "$step" "$total" "$directory"
    done

    pipit::log::footer
    pipit::log::success "go.mod files tidied"
}

main "$@"
