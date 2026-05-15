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

// Package passes rewrites compiled bytecode into faster equivalent bytecode.
//
// Each pass implements the Pass interface and rewrites a CompiledFunction body in place.
// RunPipeline builds one FunctionAnalysis per function and hands it to every pass through
// the PassContext. Every optimisation that can change observable behaviour is switched by
// a field of Options.
//
// # Pass contract
//
// No pass removes or moves an instruction: a dead slot becomes isa.OpNop so every
// instruction index and jump offset stays valid. The body only changes length through the
// inliner's splice. Register liveness is not computed; a pass may only rely on local
// proofs bounded by the next jump target, call or return, or the dominator table. Passes
// are idempotent.
package passes
