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

package asm

import (
	"piko.sh/asmgen"
)

// tier1MathUnaryHandlers returns the tier-1 ASM handlers for pure-FPU math intrinsics
// (Sqrt, Abs, Floor, Ceil, Trunc, Round) that map directly to single FPU instructions.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1MathUnaryHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		tier1FloatUnaryHandler("handlerSubOpMathSqrt", "SQRT",
			"handlerSubOpMathSqrt sets floats[B] = math.Sqrt(floats[C])."),
		tier1FloatUnaryHandler("handlerSubOpMathAbs", "ABS",
			"handlerSubOpMathAbs sets floats[B] = math.Abs(floats[C])."),
		tier1FloatUnaryHandler("handlerSubOpMathFloor", "FLOOR",
			"handlerSubOpMathFloor sets floats[B] = math.Floor(floats[C])."),
		tier1FloatUnaryHandler("handlerSubOpMathCeil", "CEIL",
			"handlerSubOpMathCeil sets floats[B] = math.Ceil(floats[C])."),
		tier1FloatUnaryHandler("handlerSubOpMathTrunc", "TRUNC",
			"handlerSubOpMathTrunc sets floats[B] = math.Trunc(floats[C])."),
		tier1FloatUnaryHandler("handlerSubOpMathRound", "ROUND",
			"handlerSubOpMathRound sets floats[B] = math.Round(floats[C])."),
	}
}
