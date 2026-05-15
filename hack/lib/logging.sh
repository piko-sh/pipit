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

# hack/lib/logging.sh - Standardised logging utilities for pipit scripts

# Prevent double-sourcing
if [[ -n "${_PIPIT_LOGGING_LOADED:-}" ]]; then
    return 0
fi
readonly _PIPIT_LOGGING_LOADED=1

# Colour codes (only if terminal supports it)
if [[ -t 2 ]]; then
    readonly PIPIT_RED='\033[0;31m'
    readonly PIPIT_GREEN='\033[0;32m'
    readonly PIPIT_YELLOW='\033[1;33m'
    readonly PIPIT_BLUE='\033[0;34m'
    readonly PIPIT_CYAN='\033[0;36m'
    readonly PIPIT_BOLD='\033[1m'
    readonly PIPIT_NC='\033[0m'
else
    readonly PIPIT_RED=''
    readonly PIPIT_GREEN=''
    readonly PIPIT_YELLOW=''
    readonly PIPIT_BLUE=''
    readonly PIPIT_CYAN=''
    readonly PIPIT_BOLD=''
    readonly PIPIT_NC=''
fi

# pipit::log::info prints an informational message
# Arguments:
#   $@ - Message to print
pipit::log::info() {
    echo -e "${PIPIT_BLUE}[INFO]${PIPIT_NC} $*" >&2
}

# pipit::log::success prints a success message
# Arguments:
#   $@ - Message to print
pipit::log::success() {
    echo -e "${PIPIT_GREEN}[OK]${PIPIT_NC} $*" >&2
}

# pipit::log::warn prints a warning message
# Arguments:
#   $@ - Message to print
pipit::log::warn() {
    echo -e "${PIPIT_YELLOW}[WARN]${PIPIT_NC} $*" >&2
}

# pipit::log::error prints an error message
# Arguments:
#   $@ - Message to print
pipit::log::error() {
    echo -e "${PIPIT_RED}[ERROR]${PIPIT_NC} $*" >&2
}

# pipit::log::fatal prints an error message and exits with code 1
# Arguments:
#   $@ - Message to print
pipit::log::fatal() {
    echo -e "${PIPIT_RED}[FATAL]${PIPIT_NC} $*" >&2
    exit 1
}

# pipit::log::header prints a prominent header
# Arguments:
#   $@ - Header text
pipit::log::header() {
    echo "========================================================================" >&2
    echo -e " ${PIPIT_BOLD}$*${PIPIT_NC}" >&2
    echo "========================================================================" >&2
}

# pipit::log::footer prints a footer separator
pipit::log::footer() {
    echo "------------------------------------------------------------------------" >&2
    echo >&2
}

# pipit::log::step prints a step indicator for multi-step processes
# Arguments:
#   $1 - Step number
#   $2 - Total steps
#   $3 - Step description
pipit::log::step() {
    local step="$1"
    local total="$2"
    shift 2
    echo -e "${PIPIT_CYAN}[${step}/${total}]${PIPIT_NC} $*" >&2
}

# pipit::log::detail prints supplementary detail without a prefix.
# Use for indented instructions or examples under a main log message.
# Arguments:
#   $@ - Detail text to print
pipit::log::detail() {
    echo -e "  $*" >&2
}

# pipit::log::blank prints a blank line for visual spacing.
pipit::log::blank() {
    echo >&2
}
