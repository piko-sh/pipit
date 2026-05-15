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

# hack/generate/asmgen.sh - Generate the Plan 9 dispatch assembly and its offset headers
#
# Usage:
#   ./hack/generate/asmgen.sh              Regenerate the .s and .h files
#   ./hack/generate/asmgen.sh --validate   Fail when the committed files are stale

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The module directory holding the generator.
readonly ASMGEN_DIR="${PIPIT_ROOT}/cmd/asmgen"

# generate_asmgen runs the generator, optionally in validate mode.
# Globals:
#   ASMGEN_DIR - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $@ - Extra flags for the generator
generate_asmgen() {
    ( cd "$ASMGEN_DIR" && "${GO}" run . -root "${PIPIT_ROOT}" "$@" )
}

# main generates or validates the dispatch assembly.
# Arguments:
#   $1 - --validate to check the committed files instead of rewriting them
main() {
    if [[ "${1:-}" == "--validate" ]]; then
        pipit::log::header "Validating the committed dispatch assembly"
        generate_asmgen -validate
        pipit::log::footer
        pipit::log::success "dispatch assembly is up to date"
        return 0
    fi

    pipit::log::header "Generating the dispatch assembly"

    generate_asmgen

    pipit::log::footer
    pipit::log::success "dispatch assembly generation complete!"
}

main "$@"
