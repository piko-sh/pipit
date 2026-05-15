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

# hack/wasm/build.sh - Build, optimise, and compress the pipit WASM playground binary
#
# Usage:
#   ./hack/wasm/build.sh build   Build, optimise, and compress (the default)
#   ./hack/wasm/build.sh clean   Remove build artefacts

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# WASM_OUT_DIR is the output directory for WASM build artefacts.
WASM_OUT_DIR="${PIPIT_ROOT}/bin/wasm"

# WASM_RAW is the raw (unoptimised) WASM binary.
WASM_RAW="${WASM_OUT_DIR}/pipit.wasm"

# WASM_OPT is the wasm-opt optimised binary.
WASM_OPT="${WASM_OUT_DIR}/pipit.opt.wasm"

# WASM_FINAL is the final binary (optimised if wasm-opt is available, raw copy otherwise).
WASM_FINAL="${WASM_OUT_DIR}/pipit.final.wasm"

# report_size prints the on-disk size of a build artefact when it exists.
# Arguments:
#   $1 - Label to display
#   $2 - Path to the file
report_size() {
    local label="$1" file="$2"
    if [[ -f "$file" ]]; then
        pipit::log::detail "$(printf '%-18s %s' "$label" "$(du -h "$file" | cut -f1)")"
    fi
}

# compile_wasm builds the WASM binary with trimmed paths and stripped debug info.
compile_wasm() {
    pipit::log::header "Compiling WASM binary"
    mkdir -p "$WASM_OUT_DIR"
    pipit::log::detail "GOOS=js GOARCH=wasm ${GO} build -trimpath -ldflags=\"-s -w\" ./cmd/wasm"
    ( cd "$PIPIT_ROOT/cmd/wasm" && GOOS=js GOARCH=wasm "$GO" build -trimpath -ldflags="-s -w" -o "$WASM_RAW" . )
    report_size "Raw WASM" "$WASM_RAW"
}

# optimise_wasm shrinks the binary with wasm-opt, falling back to a plain copy when the tool
# is unavailable.
optimise_wasm() {
    pipit::log::header "Optimising WASM binary"
    if ! command -v wasm-opt &>/dev/null; then
        pipit::log::detail "wasm-opt not found - copying raw binary (install binaryen for smaller output)"
        cp "$WASM_RAW" "$WASM_FINAL"
        return
    fi
    pipit::log::detail "wasm-opt -Oz"
    if wasm-opt -Oz \
        --enable-bulk-memory \
        --enable-nontrapping-float-to-int \
        --enable-sign-ext \
        --enable-mutable-globals \
        -o "$WASM_OPT" "$WASM_RAW"; then
        report_size "Optimised WASM" "$WASM_OPT"
        cp "$WASM_OPT" "$WASM_FINAL"
    else
        pipit::log::detail "wasm-opt failed - using unoptimised binary"
        cp "$WASM_RAW" "$WASM_FINAL"
    fi
}

# compress creates gzip and (when available) brotli variants of the given file.
# Arguments:
#   $1 - Path to the file to compress
compress() {
    local file="$1"
    if gzip -9 -k -f "$file"; then
        report_size "gzip" "${file}.gz"
    fi
    if command -v brotli &>/dev/null; then
        if brotli -9 -k -f "$file"; then
            report_size "brotli" "${file}.br"
        fi
    else
        pipit::log::detail "brotli not found - skipping brotli for $(basename "$file")"
    fi
}

# compress_wasm creates compressed variants of the final WASM binary.
compress_wasm() {
    pipit::log::header "Compressing WASM binary"
    compress "$WASM_FINAL"
}

# copy_wasm_exec copies wasm_exec.js from GOROOT and compresses it.
copy_wasm_exec() {
    pipit::log::header "Copying wasm_exec.js"
    local goroot
    goroot="$("$GO" env GOROOT)"

    local wasm_exec=""
    if [[ -f "$goroot/lib/wasm/wasm_exec.js" ]]; then
        wasm_exec="$goroot/lib/wasm/wasm_exec.js"
    elif [[ -f "$goroot/misc/wasm/wasm_exec.js" ]]; then
        wasm_exec="$goroot/misc/wasm/wasm_exec.js"
    fi

    if [[ -z "$wasm_exec" ]]; then
        pipit::log::detail "wasm_exec.js not found in GOROOT ($goroot)"
        return
    fi

    rm -f "$WASM_OUT_DIR/wasm_exec.js" "$WASM_OUT_DIR/wasm_exec.js.gz" "$WASM_OUT_DIR/wasm_exec.js.br"
    cp "$wasm_exec" "$WASM_OUT_DIR/"
    pipit::log::detail "source: $wasm_exec"
    report_size "wasm_exec.js" "$WASM_OUT_DIR/wasm_exec.js"
    compress "$WASM_OUT_DIR/wasm_exec.js"
}

# print_summary lists the final artefact sizes.
print_summary() {
    pipit::log::header "Build summary"
    report_size "Raw WASM" "$WASM_RAW"
    report_size "Optimised WASM" "$WASM_OPT"
    report_size "Final WASM" "$WASM_FINAL"
    report_size "Final gzip" "${WASM_FINAL}.gz"
    report_size "Final brotli" "${WASM_FINAL}.br"
    report_size "wasm_exec.js" "$WASM_OUT_DIR/wasm_exec.js"
    pipit::log::detail "Output directory: $WASM_OUT_DIR"
}

# clean_build removes all WASM build artefacts.
clean_build() {
    pipit::log::header "Cleaning WASM build artefacts"
    if [[ -d "$WASM_OUT_DIR" ]]; then
        rm -rf "${WASM_OUT_DIR:?}"
        pipit::log::detail "Removed $WASM_OUT_DIR"
    else
        pipit::log::detail "Nothing to clean"
    fi
}

# main dispatches build (default) or clean.
# Arguments:
#   $1 - Command to execute: build (default) or clean
main() {
    local command="${1:-build}"

    case "$command" in
        build)
            compile_wasm
            optimise_wasm
            compress_wasm
            copy_wasm_exec
            print_summary
            pipit::log::blank
            pipit::log::success "WASM build complete"
            ;;

        clean)
            clean_build
            ;;

        help|--help|-h)
            pipit::log::info "Usage: hack/wasm/build.sh [build|clean]"
            ;;

        *)
            pipit::log::fatal "Unknown command: $command (expected: build, clean)"
            ;;
    esac
}

main "$@"
