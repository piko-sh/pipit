// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build integration && pipit_store_census && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/engine"
)

const storeCensusOn = true

func censusGate(code int) int {
	totals := map[string]int64{}
	var gating []string
	for key, count := range engine.StoreCensusViolations() {
		kind, _, _ := strings.Cut(key, "\t")
		totals[kind] += count
		if engine.StoreCensusGates(kind) {
			gating = append(gating, fmt.Sprintf("%s x%d", strings.ReplaceAll(key, "\t", " | "), count))
		}
	}
	kinds := make([]string, 0, len(totals))
	for kind := range totals {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	for _, kind := range kinds {
		_, _ = fmt.Fprintf(os.Stderr, "pipit-census: %d %s\n", totals[kind], kind)
	}
	if len(gating) == 0 {
		return code
	}
	slices.Sort(gating)
	_, _ = fmt.Fprintf(os.Stderr, "pipit-census: FAIL: %d gating violations (kind | op | class | detail):\n%s\n",
		len(gating), strings.Join(gating, "\n"))
	return 1
}
