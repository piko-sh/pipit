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

# hack/lint/vet.sh - Run go vet over the workspace, on one or every build configuration
#
# Usage:
#   ./hack/lint/vet.sh            Vet the default build configuration
#   ./hack/lint/vet.sh tags       Vet every build tag and cross-compile target
#   ./hack/lint/vet.sh licence    Run the licensed-reuse structural checks over every build

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Build tags that gate real code and so need their own vet pass.
readonly BUILD_TAGS=(safe fuzz bench pipit_arena_paranoid pipit_bce_paranoid pipit_opcode_census
    pipit_ranked_dispatch pipit_reuse_audit pipit_store_census pipit_testhooks)

# Builds the licensed-reuse structural checks run over. Each compiles a different set of the
# engine's store paths, so a write that bypasses the barriers in one of them is invisible
# to the others.
readonly LICENCE_CHECK_TAGS=("" safe pipit_store_census pipit_reuse_audit pipit_arena_paranoid
    pipit_testhooks)

# GOOS/GOARCH pairs the project supports.
readonly CROSS_TARGETS=(linux/arm64 linux/riscv64 darwin/arm64 windows/amd64 js/wasm)

# vet_default vets the default build configuration of both modules.
# Globals:
#   PIPIT_ROOT - Read
vet_default() {
    pipit::log::info "vet ./internal/engine/... (unsafeptr off)"
    "${GO}" vet -unsafeptr=false ./internal/engine/...

    pipit::log::info "vet the rest of the root module"
    local packages
    packages=$("${GO}" list ./... | grep -v '/internal/engine')
    # shellcheck disable=SC2086
    "${GO}" vet $packages

    pipit::log::info "vet cmd/pipit"
    ( cd "${PIPIT_ROOT}/cmd/pipit" && "${GO}" vet ./... )
}

# vet_tags type-checks every build configuration the project actually uses.
#
# A file excluded from the default configuration is compiled by nothing else, and
# both axes have already gone dark and broken: the microbenchmarks sat behind
# `bench` and no target built it, and the js/wasm stub in internal/adapters
# referenced a type that had moved packages. Neither failed anything, because
# nothing compiled them.
#
# Globals:
#   BUILD_TAGS - Read
#   CROSS_TARGETS - Read
#   PIPIT_ROOT - Read
vet_tags() {
    for tag in "${BUILD_TAGS[@]}"; do
        pipit::log::info "vet -tags $tag"
        "${GO}" vet -unsafeptr=false -tags "$tag" ./...
    done

    for target in "${CROSS_TARGETS[@]}"; do
        pipit::log::info "build ${target}"
        GOOS="${target%/*}" GOARCH="${target#*/}" "${GO}" build ./...
    done

    pipit::log::info "vet -tags crosslang (tests/bench)"
    ( cd "${PIPIT_ROOT}/tests/bench" && "${GO}" vet -unsafeptr=false -tags crosslang ./... )

    pipit::log::info "vet -tags bench (tests/bench/micro)"
    ( cd "${PIPIT_ROOT}/tests/bench" && "${GO}" vet -unsafeptr=false -tags bench ./micro/... )

    pipit::log::info "vet -tags pipit_opcode_census (tests/bench/cross_language/cmd)"
    ( cd "${PIPIT_ROOT}/tests/bench" && "${GO}" vet -unsafeptr=false -tags pipit_opcode_census ./cross_language/cmd/... )

    pipit::log::info "vet js/wasm (internal/engine, internal/leakcheck)"
    GOOS=js GOARCH=wasm "${GO}" vet -unsafeptr=false ./internal/engine/... ./internal/leakcheck/...

    while IFS= read -r dir; do
        pipit::log::info "vet -tags integration ($dir)"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" vet -tags integration ./... )
    done < <(pipit::go::integration_dirs)

    pipit::log::info "vet -tags integration,crossarch (docker cross-arch lane)"
    for dir in "${PIPIT_ROOT}"/tests/integration/{language,snippets}/; do
        ( cd "$dir" && "${GO}" vet -tags integration,crossarch ./... )
    done
}

# vet_licence runs hack/licencecheck, the licensed-reuse structural checks, as a vet tool.
# Globals:
#   LICENCE_CHECK_TAGS - Read
#   PIPIT_ROOT - Read
vet_licence() {
    local tool_dir
    tool_dir=$(mktemp -d)
    # shellcheck disable=SC2064
    trap "rm -rf -- '${tool_dir}'" EXIT

    pipit::log::info "build hack/licencecheck"
    "${GO}" build -o "${tool_dir}/licencecheck" ./hack/licencecheck

    local tag
    for tag in "${LICENCE_CHECK_TAGS[@]}"; do
        pipit::log::info "licencecheck (${tag:-default build})"
        "${GO}" vet ${tag:+-tags "$tag"} -vettool="${tool_dir}/licencecheck" ./...
    done

    # Outside the engine js/wasm compiles the safe twins, which the safe pass covers. The
    # engine alone compiles a mix no other pass sees, so only it is checked a second time.
    pipit::log::info "licencecheck (js/wasm)"
    GOOS=js GOARCH=wasm "${GO}" vet -vettool="${tool_dir}/licencecheck" ./internal/engine/...
}

# main dispatches to the requested vet mode.
# Arguments:
#   $1 - Mode to run: default (the default), tags or licence
main() {
    local command="${1:-default}"

    case "$command" in
        default)
            pipit::log::header "go vet (default build configuration)"
            vet_default
            pipit::log::footer
            pipit::log::success "go vet passed"
            ;;

        tags)
            pipit::log::header "go vet (every build tag and cross-compile target)"
            vet_tags
            pipit::log::footer
            pipit::log::success "every build configuration type-checks"
            ;;

        licence)
            pipit::log::header "licencecheck (licensed-reuse checks, every build)"
            vet_licence
            pipit::log::footer
            pipit::log::success "licencecheck passed"
            ;;

        help|--help|-h)
            pipit::log::info "Usage: hack/lint/vet.sh [default|tags|licence]"
            ;;

        *)
            pipit::log::fatal "Unknown command: $command (expected: default, tags, licence)"
            ;;
    esac
}

main "$@"
