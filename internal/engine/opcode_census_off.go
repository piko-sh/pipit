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

//go:build !pipit_opcode_census

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// opcodeCensusBuilt reports whether opcode census instrumentation is enabled.
const opcodeCensusBuilt = false

// opcodeCensusRecord does nothing outside census builds.
func opcodeCensusRecord(_ *program.CompiledFunction, _ []isa.Instruction, _ int) {}

// slowCallCensusRecord does nothing outside census builds.
func slowCallCensusRecord(_ string, _ reflect.Type) {}
