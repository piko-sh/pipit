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

# hack/go/mod-verify.sh - Check module checksums, and tidiness where it means anything
#
# Usage:
#   ./hack/go/mod-verify.sh

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Module directories whose downloaded dependencies are checksum-verified.
VERIFY_DIRS=(cmd/asmgen cmd/pipit sdk/extract sdk/link tests/bench)
while IFS= read -r suite; do
    VERIFY_DIRS+=("$suite")
done < <(pipit::go::integration_dirs)
readonly VERIFY_DIRS

# Module directories where `go mod tidy -diff` is a meaningful check.
readonly TIDY_DIRS=(. sdk/extract sdk/link)

# verify_checksums runs go mod verify in the root module and each sub-module.
# Globals:
#   VERIFY_DIRS - Read
#   PIPIT_ROOT - Read
verify_checksums() {
    pipit::log::info "go mod verify ."
    "${GO}" mod verify

    for dir in "${VERIFY_DIRS[@]}"; do
        pipit::log::info "go mod verify $dir"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" mod verify )
    done
}

# verify_tidy fails when a tidiable go.mod has drifted.
# Globals:
#   TIDY_DIRS - Read
#   PIPIT_ROOT - Read
verify_tidy() {
    for dir in "${TIDY_DIRS[@]}"; do
        pipit::log::info "go mod tidy -diff $dir"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" mod tidy -diff )
    done
}

# main verifies checksums and tidiness.
# Arguments:
#   $@ - Unused
main() {
    pipit::log::header "Verifying go.mod files"

    verify_checksums
    verify_tidy

    pipit::log::footer
    pipit::log::success "go.mod files verified"
}

main "$@"
