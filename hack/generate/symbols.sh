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

# This project stands against fascism, authoritarianism, and all forms of
# oppression. We built this to empower people, not to enable those who would
# strip others of their rights and dignity.

# hack/generate/symbols.sh - Generate the vendored symbol tables
#
# Usage:
#   ./hack/generate/symbols.sh interp                Regenerate the stdlib bundles
#   ./hack/generate/symbols.sh interp --validate     Check the stdlib bundles
#   ./hack/generate/symbols.sh selfhost              Regenerate the self-hosting tables
#   ./hack/generate/symbols.sh selfhost --validate   Check the self-hosting tables

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Manifest naming every stdlib package the interpreter can call.
readonly STDLIB_MANIFEST="pipit-symbols-stdlib.yaml"

# Manifest naming the packages the self-hosting lane serves natively.
readonly SELFHOST_MANIFEST="pipit-symbols-selfhost.yaml"

# Directory the self-hosting tables are written to.
readonly SELFHOST_OUTPUT="sdk/selfhost"

# Directory holding the bundles, one subdirectory each.
readonly BUNDLE_ROOT="sdk/stdlib"

# Directory holding the shared gcexportdata unit, which every bundle answers type queries
# from because the type checker resolves a signature in one bundle against another.
readonly TYPES_OUTPUT="sdk/stdlib/typeinfo"

# Package name of that unit.
readonly TYPES_PACKAGE="typeinfo"

# Directory holding the import-path index. It stays under internal/ because the isolated
# tier and the compiler's diagnostics consult it on the default path, where the tables
# themselves must not be linked.
readonly INDEX_OUTPUT="internal/stdlibindex"

# Package name of that index.
readonly INDEX_PACKAGE="stdlibindex"

# Bundles, as "name:prefix,prefix,...". A prefix matches an import path exactly or as a
# parent directory of it, so "crypto" takes crypto and crypto/aes.
readonly BUNDLES=(
    "core:bufio,bytes,cmp,container,context,embed,errors,expvar,flag,fmt,iter,log,maps,math,reflect,runtime,slices,sort,strconv,strings,structs,sync,time,unicode,unique,uuid,weak"
    "codec:archive,compress,encoding,hash,index"
    "crypto:crypto"
    "net:mime,net"
    "system:database,io,os,path"
    "text:html,regexp,text"
    "image:image"
    "gotool:debug,go,testing"
)

# Path of the pipit binary that runs the extractor, built once per run.
EXTRACTOR_BINARY=""

# Scratch directory removed on exit; holds the binary and any validation output.
SCRATCH_DIR=""

# build_extractor builds the pipit command once per run.
# Globals:
#   EXTRACTOR_BINARY, SCRATCH_DIR - Modified
#   PIPIT_ROOT - Read
build_extractor() {
    if [[ -n "$EXTRACTOR_BINARY" ]]; then
        return 0
    fi

    SCRATCH_DIR="$(mktemp -d "${PIPIT_ROOT}/.symbols.XXXXXX")"
    # shellcheck disable=SC2064
    trap "rm -rf '${SCRATCH_DIR}'" EXIT
    EXTRACTOR_BINARY="${SCRATCH_DIR}/pipit"
    ( cd "$PIPIT_ROOT" && "${GO}" build -o "$EXTRACTOR_BINARY" ./cmd/pipit )
}

# manifest_package_count prints how many packages the stdlib manifest lists.
# Globals:
#   STDLIB_MANIFEST - Read
manifest_package_count() {
    "$EXTRACTOR_BINARY" extract generate \
        --manifest "$STDLIB_MANIFEST" --list | wc -l
}

# bundle_package_count prints how many packages one bundle's prefixes select.
# Arguments:
#   $1 - Comma-separated prefixes
bundle_package_count() {
    "$EXTRACTOR_BINARY" extract generate \
        --manifest "$STDLIB_MANIFEST" --select "$1" --list | wc -l
}

# verify_bundle_coverage fails when the bundles do not partition the manifest.
#
# A package the BUNDLES table forgot would otherwise vanish from the build with no error:
# nothing references it, so nothing notices.
# Globals:
#   BUNDLES - Read
verify_bundle_coverage() {
    local total selected=0 name prefixes count entry

    total="$(manifest_package_count)"

    for entry in "${BUNDLES[@]}"; do
        name="${entry%%:*}"
        prefixes="${entry#*:}"
        count="$(bundle_package_count "$prefixes")"
        selected=$((selected + count))
        pipit::log::detail "${name}: ${count} packages"
    done

    if [[ "$selected" != "$total" ]]; then
        pipit::log::fatal "bundles cover ${selected} of ${total} manifest packages; update BUNDLES in $0"
    fi

    pipit::log::detail "all ${total} manifest packages are bundled"
}

# generate_bundles writes every bundle's reflect tables plus the shared types unit below
# the given root.
# Globals:
#   BUNDLES, BUNDLE_ROOT, STDLIB_MANIFEST, TYPES_OUTPUT, TYPES_PACKAGE - Read
# Arguments:
#   $1 - Root directory to write below
generate_bundles() {
    local root="$1" entry name prefixes

    for entry in "${BUNDLES[@]}"; do
        name="${entry%%:*}"
        prefixes="${entry#*:}"
        pipit::util::ensure_dir "${root}/${BUNDLE_ROOT}/${name}"
        "$EXTRACTOR_BINARY" extract generate \
            --manifest "$STDLIB_MANIFEST" \
            --select "$prefixes" \
            --emit symbols \
            --package "$name" \
            --output "${root}/${BUNDLE_ROOT}/${name}" > /dev/null
    done

    pipit::util::ensure_dir "${root}/${TYPES_OUTPUT}"
    "$EXTRACTOR_BINARY" extract generate \
        --manifest "$STDLIB_MANIFEST" \
        --emit types \
        --package "$TYPES_PACKAGE" \
        --output "${root}/${TYPES_OUTPUT}" > /dev/null

    pipit::util::ensure_dir "${root}/${INDEX_OUTPUT}"
    "$EXTRACTOR_BINARY" extract generate \
        --manifest "$STDLIB_MANIFEST" \
        --emit index \
        --package "$INDEX_PACKAGE" \
        --output "${root}/${INDEX_OUTPUT}" > /dev/null
}

# compare_generated reports every generated file that differs between the committed tree
# and a regenerated one, and every committed generated file the regeneration no longer
# produces.
# Globals:
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - Root of the regenerated tree
#   $2 - Directory to compare, relative to both roots
# Returns:
#   0 when the directory matches, 1 when it does not
compare_generated() {
    local scratch="$1" directory="$2" relative stale=0

    while IFS= read -r relative; do
        if ! diff -q "${PIPIT_ROOT}/${directory}/${relative}" \
            "${scratch}/${directory}/${relative}" > /dev/null 2>&1; then
            pipit::log::error "stale: ${directory}/${relative#./}"
            stale=1
        fi
    done < <(generated_files "${scratch}/${directory}")

    while IFS= read -r relative; do
        if [[ ! -f "${scratch}/${directory}/${relative}" ]]; then
            pipit::log::error "orphaned: ${directory}/${relative#./}"
            stale=1
        fi
    done < <(generated_files "${PIPIT_ROOT}/${directory}")

    return "$stale"
}

# generated_files prints the generated files below a directory, relative to it and sorted,
# so two output trees can be compared.
#
# Tests are never generated output, so a colocated gen_*_test.go is not drift.
# Arguments:
#   $1 - Directory to list
generated_files() {
    ( cd "$1" && find . -name 'gen_*' -type f ! -name '*_test.go' | sort )
}

# validate_bundles regenerates into a scratch tree and fails on any difference.
# Globals:
#   BUNDLE_ROOT, SCRATCH_DIR, TYPES_OUTPUT - Read
validate_bundles() {
    local scratch="${SCRATCH_DIR}/validate" stale=0 directory

    generate_bundles "$scratch"

    for directory in "${BUNDLE_ROOT}"/*/ "${TYPES_OUTPUT}" "${INDEX_OUTPUT}"; do
        directory="${directory%/}"
        [[ -d "${scratch}/${directory}" ]] || continue
        compare_generated "$scratch" "$directory" || stale=1
    done

    if (( stale )); then
        pipit::log::fatal "symbol tables are stale; run 'make generate-symbols'"
    fi

    pipit::log::success "symbol tables are up to date"
}

# generate_interp regenerates or validates the stdlib bundles.
# Globals:
#   PIPIT_ROOT - Read
# Arguments:
#   $1 - --validate to check instead of write
generate_interp() {
    build_extractor
    verify_bundle_coverage

    if [[ "${1:-}" == "--validate" ]]; then
        validate_bundles
        return 0
    fi

    generate_bundles "$PIPIT_ROOT"
    pipit::log::success "stdlib symbol tables regenerated"
}

# generate_selfhost regenerates or validates the symbol tables of pipit's own packages.
#
# The extractor runs from the repository root, so that it loads this repository's
# internal packages.
# Globals:
#   PIPIT_ROOT, SCRATCH_DIR, SELFHOST_MANIFEST, SELFHOST_OUTPUT - Read
# Arguments:
#   $1 - --validate to check instead of write
generate_selfhost() {
    build_extractor

    if [[ "${1:-}" == "--validate" ]]; then
        local scratch="${SCRATCH_DIR}/validate"
        pipit::util::ensure_dir "${scratch}/${SELFHOST_OUTPUT}"
        ( cd "$PIPIT_ROOT" && "$EXTRACTOR_BINARY" extract generate \
            --manifest "$SELFHOST_MANIFEST" --output "${scratch}/${SELFHOST_OUTPUT}" > /dev/null )
        if ! compare_generated "$scratch" "$SELFHOST_OUTPUT"; then
            pipit::log::fatal "self-hosting symbol tables are stale; run 'make generate-selfhost-symbols'"
        fi
        pipit::log::success "self-hosting symbol tables are up to date"
        return 0
    fi

    pipit::util::ensure_dir "$SELFHOST_OUTPUT"
    ( cd "$PIPIT_ROOT" && "$EXTRACTOR_BINARY" extract generate --manifest "$SELFHOST_MANIFEST" )
    pipit::log::success "self-hosting symbol tables regenerated"
}

# main dispatches to the requested symbol table.
# Arguments:
#   $1 - Which tables to generate: interp or selfhost
#   $2 - --validate, where the mode supports it
main() {
    local command="${1:-interp}"

    case "$command" in
        interp)
            pipit::log::header "Stdlib symbol tables"
            shift
            generate_interp "$@"
            pipit::log::footer
            ;;

        selfhost)
            pipit::log::header "Self-hosting symbol tables"
            shift
            generate_selfhost "$@"
            pipit::log::footer
            ;;

        help|--help|-h)
            pipit::log::info "Usage: hack/generate/symbols.sh [interp|selfhost] [--validate]"
            ;;

        *)
            pipit::log::fatal "Unknown command: $command (expected: interp, selfhost)"
            ;;
    esac
}

main "$@"
