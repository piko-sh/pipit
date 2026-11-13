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

//go:build pipit_opcode_census

package main

import (
	"encoding/json"
	"fmt"

	"pipit.sh/pipit/internal/engine"
)

// censusBuilt says the binary was built with the opcode census.
const censusBuilt = true

// resetCensus discards the dispatch counts gathered so far.
func resetCensus() {
	engine.OpcodeCensusReset()
}

// captureCensus takes the dispatch counts gathered since the last reset.
//
// Takes withRaw (bool) which asks for the census report in its JSON form too.
//
// Returns censusCapture which holds the total, the per-function counts and, when asked
// for, the raw report.
// Returns error when the raw report cannot be encoded.
func captureCensus(withRaw bool) (censusCapture, error) {
	report := engine.OpcodeCensusSnapshot()
	capture := censusCapture{
		raw:       nil,
		functions: make([]countedFunction, 0, len(report.Functions)),
		total:     report.Total,
	}
	if withRaw {
		raw, err := json.Marshal(report)
		if err != nil {
			return censusCapture{}, fmt.Errorf("encoding the census: %w", err)
		}
		capture.raw = raw
	}
	for _, function := range report.Functions {
		capture.functions = append(capture.functions, countedFunction{function: function.Function, counts: function.Counts})
	}
	return capture, nil
}
