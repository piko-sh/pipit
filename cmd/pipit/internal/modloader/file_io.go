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

package modloader

import "os"

// osReadFileReal is the production implementation of file reads.
//
// Split out so tests can replace osReadFile without dragging the rest of the package into
// test scaffolding.
//
// Takes path (string) which is the filesystem path to read.
//
// Returns []byte which holds the file contents.
// Returns error when the file cannot be read.
func osReadFileReal(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // operator-supplied path
}
