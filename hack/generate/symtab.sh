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

# hack/generate/symtab.sh - Generate the named-scalar pool tag types
#
# Usage:
#   ./hack/generate/symtab.sh              Regenerate the tag types
#   ./hack/generate/symtab.sh --validate   Fail when the committed types are stale

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# The generator package.
readonly SYMTAB_GENERATOR="./internal/symtab/gen"

# The file the generator writes.
readonly SYMTAB_OUTPUT="internal/symtab/typemodel/named_scalar_pool_tags_gen.go"

# generate_symtab runs the generator, optionally in validate mode.
# Globals:
#   SYMTAB_GENERATOR - Read
#   SYMTAB_OUTPUT - Read
# Arguments:
#   $@ - Extra flags for the generator
generate_symtab() {
    "${GO}" run "$SYMTAB_GENERATOR" -out "$SYMTAB_OUTPUT" "$@"
}

# main generates or validates the named-scalar pool tags.
# Arguments:
#   $1 - --validate to check the committed file instead of rewriting it
main() {
    if [[ "${1:-}" == "--validate" ]]; then
        pipit::log::header "Validating the named-scalar pool tags"
        generate_symtab -validate
        pipit::log::footer
        pipit::log::success "named-scalar pool tags are up to date"
        return 0
    fi

    pipit::log::header "Generating the named-scalar pool tags"

    generate_symtab

    pipit::log::footer
    pipit::log::success "named-scalar pool tag generation complete!"
}

main "$@"
