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

# hack/build/build.sh - Build or install the pipit CLI
#
# Usage:
#   ./hack/build/build.sh current [output]   Build for the host platform
#   ./hack/build/build.sh all [output dir]   Build for every release platform
#   ./hack/build/build.sh install            go install into GOBIN

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Platforms the release workflow publishes.
readonly PIPIT_PLATFORMS=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)

# The package that builds the CLI.
readonly CLI_PACKAGE="./cmd/pipit"

# build_version reports the version to stamp into the binary.
# Outputs:
#   Writes the version string to stdout
build_version() {
    echo "${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
}

# build_ldflags reports the linker flags that stamp the version.
# Outputs:
#   Writes the ldflags string to stdout
build_ldflags() {
    echo "-X pipit.sh/pipit/cmd/pipit/internal/buildinfo.Version=$(build_version)"
}

# build_current builds the CLI for the host platform.
# Arguments:
#   $1 - Output file (defaults to bin/pipit)
build_current() {
    local output="${1:-${PIPIT_ROOT}/bin/pipit}"

    pipit::go::build "$output" "$CLI_PACKAGE" "" "" "$(build_ldflags)"
}

# build_all builds the CLI for every release platform.
# Globals:
#   PIPIT_PLATFORMS - Read
# Arguments:
#   $1 - Output directory (defaults to bin/cli)
build_all() {
    local output_dir="${1:-${PIPIT_ROOT}/bin/cli}"
    local ldflags
    ldflags=$(build_ldflags)

    local total=${#PIPIT_PLATFORMS[@]}
    local current=0

    for platform in "${PIPIT_PLATFORMS[@]}"; do
        current=$((current + 1))
        local goos="${platform%/*}"
        local goarch="${platform#*/}"
        local extension=""

        if [[ "$goos" == "windows" ]]; then
            extension=".exe"
        fi

        pipit::log::step "$current" "$total" "Building ${goos}/${goarch}"
        pipit::go::build "${output_dir}/${goos}-${goarch}/pipit${extension}" \
            "$CLI_PACKAGE" "$goos" "$goarch" "$ldflags"
    done
}

# install_cli installs the CLI into GOBIN.
install_cli() {
    "${GO}" install -ldflags "$(build_ldflags)" "$CLI_PACKAGE"
    pipit::log::success "installed pipit $(build_version)"
}

# main dispatches to the requested build mode.
# Arguments:
#   $1 - Mode to run: current (the default), all or install
#   $2 - Output file or directory, where the mode takes one
main() {
    local command="${1:-current}"

    case "$command" in
        current)
            pipit::log::header "Building pipit for the host platform"
            build_current "${2:-}"
            ;;

        all)
            pipit::log::header "Building pipit for every release platform"
            build_all "${2:-}"
            ;;

        install)
            pipit::log::header "Installing pipit"
            install_cli
            ;;

        help|--help|-h)
            pipit::log::info "Usage: hack/build/build.sh [current|all|install] [output]"
            ;;

        *)
            pipit::log::fatal "Unknown command: $command (expected: current, all, install)"
            ;;
    esac

    pipit::log::footer
}

main "$@"
