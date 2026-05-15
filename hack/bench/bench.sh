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

# hack/bench/bench.sh - Run the interpreter benchmarks
#
# The benchmarks sit behind the `bench` build tag, so nothing else compiles them.
# Both modes need a quiet machine: on a loaded workstation the run-to-run spread
# swamps anything worth reading.
#
# Usage:
#   ./hack/bench/bench.sh run                     Run every benchmark
#   ./hack/bench/bench.sh run --filter Dispatch   Run the ones matching a regex
#   ./hack/bench/bench.sh run --count 10          Override the iteration count
#   ./hack/bench/bench.sh stability               Run at -count=10 and print the spread

# shellcheck source=../lib/init.sh
source "$(dirname "$0")/../lib/init.sh"

# Benchmarks live in two modules: the microbenchmark suite under tests/bench, and the
# engine's own benchmarks in the root module. Each entry is "module dir|package".
readonly BENCH_PACKAGES=("tests/bench|./micro/..." ".|./internal/engine/")

# Scratch directory for benchmark output. Git-ignored.
readonly BENCH_DIR="${PIPIT_ROOT}/.bench"

# Benchmark name regex.
BENCH_FILTER="."

# Number of iterations per benchmark.
BENCH_COUNT=6

# usage prints the flag reference.
usage() {
    cat <<'EOF'
Usage: hack/bench/bench.sh <run|stability> [options]

Options:
  --filter <regex>   Only benchmarks matching this regex (default: all).
  --count <n>        Iterations per benchmark (default: 6, stability forces 10).
  -h, --help         Show this help.
EOF
}

# parse_args reads the command line.
# Globals:
#   BENCH_FILTER - Set from --filter
#   BENCH_COUNT - Set from --count
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --filter)
                BENCH_FILTER="$2"
                shift 2
                ;;

            --count)
                BENCH_COUNT="$2"
                shift 2
                ;;

            -h|--help)
                usage
                exit 0
                ;;

            *)
                pipit::log::warn "Unknown argument: $1 (ignoring)"
                shift
                ;;
        esac
    done
}

# verify_benchstat checks that benchstat is installed.
# Returns:
#   Exits with code 1 if not found
verify_benchstat() {
    if ! pipit::util::verify_binary "benchstat" "go install golang.org/x/perf/cmd/benchstat@latest"; then
        exit 1
    fi
}

# run_benchmarks runs every benchmark package with the given go test flags.
# Globals:
#   BENCH_PACKAGES - Read
#   PIPIT_ROOT - Read
# Arguments:
#   $@ - Flags for go test, after -tags bench
run_benchmarks() {
    for entry in "${BENCH_PACKAGES[@]}"; do
        local dir="${entry%%|*}"
        local package="${entry##*|}"
        ( cd "${PIPIT_ROOT}/${dir}" && "${GO}" test -tags bench -run '^$' "$@" "$package" )
    done
}

# bench_run runs the benchmark set once and prints the results.
# Globals:
#   BENCH_FILTER - Read
#   BENCH_COUNT - Read
bench_run() {
    run_benchmarks -bench "$BENCH_FILTER" -benchmem -count="$BENCH_COUNT"
}

# bench_stability measures the run-to-run spread of every benchmark.
#
# benchstat prints a plus-or-minus column beside each result. A benchmark whose
# spread is wider than the change being measured cannot answer a question about
# that change, whatever its median says.
#
# Globals:
#   BENCH_DIR - Read
bench_stability() {
    verify_benchstat
    pipit::util::ensure_dir "$BENCH_DIR"

    run_benchmarks -bench . -benchmem -count=10 | tee "${BENCH_DIR}/stability.txt"

    benchstat "${BENCH_DIR}/stability.txt"
    pipit::log::info "The plus-or-minus column is the run-to-run spread on this machine"
}

# main dispatches to the requested benchmark mode.
# Arguments:
#   $1 - Mode to run: run (the default) or stability
#   $@ - Flags, see usage
main() {
    local command="${1:-run}"
    shift || true
    parse_args "$@"

    case "$command" in
        run)
            pipit::log::header "Running the interpreter benchmarks"
            bench_run
            ;;

        stability)
            pipit::log::header "Measuring benchmark stability"
            bench_stability
            ;;

        help|--help|-h)
            usage
            ;;

        *)
            pipit::log::fatal "Unknown command: $command (expected: run, stability)"
            ;;
    esac
}

main "$@"
