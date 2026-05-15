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

# hack/lib/init.sh - Main initialisation script for pipit hack/ scripts

# Prevent double-sourcing
if [[ -n "${_PIPIT_INIT_LOADED:-}" ]]; then
    return 0
fi
readonly _PIPIT_INIT_LOADED=1

set -o errexit
set -o nounset
set -o pipefail

if [[ -z "${PIPIT_ROOT:-}" ]]; then
    if [[ -n "${BASH_SOURCE[0]:-}" ]]; then
        PIPIT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
    else
        PIPIT_ROOT="$(pwd)"
        while [[ "$PIPIT_ROOT" != "/" ]] && [[ ! -f "${PIPIT_ROOT}/go.mod" ]]; do
            PIPIT_ROOT="$(dirname "$PIPIT_ROOT")"
        done
        if [[ "$PIPIT_ROOT" == "/" ]]; then
            echo "Error: Could not determine PIPIT_ROOT. Set it manually." >&2
            if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
                exit 1
            else
                return 1
            fi
        fi
    fi
fi
export PIPIT_ROOT

PIPIT_LIB_DIR="${PIPIT_ROOT}/hack/lib"
export PIPIT_LIB_DIR

# shellcheck source=./logging.sh
source "${PIPIT_LIB_DIR}/logging.sh"
# shellcheck source=./util.sh
source "${PIPIT_LIB_DIR}/util.sh"
# shellcheck source=./go.sh
source "${PIPIT_LIB_DIR}/go.sh"

cd "${PIPIT_ROOT}"
