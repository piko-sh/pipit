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

// Package codec converts compiled bytecode to and from serialisation-safe records.
//
// A CompiledFunction is full of pointers, reflect values and runtime caches that cannot
// be written to a byte stream. The codec projects it onto plain data structs the adapter
// layer can hand to FlatBuffers, and rebuilds the runtime form on the way back.
//
// It is separate from the adapter that owns the wire format so the projection can be read
// and reviewed without also reading a schema.
package codec
