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

package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/muesli/cancelreader"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// newIsolatedReplInput accepts only bounded memory or cancellable terminal and pipe
// input. Regular files are read with a byte bound before any worker starts.
//
// Takes reader (io.Reader) which is supplied by the trusted CLI host.
//
// Returns cancelreader.CancelReader which owns cancellation resources.
// Returns error which reports unsupported, oversized or unreadable input.
func newIsolatedReplInput(reader io.Reader) (cancelreader.CancelReader, error) {
	switch source := reader.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		return cancelreader.NewReader(reader)
	case *os.File:
		info, err := source.Stat()
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			data, err := io.ReadAll(io.LimitReader(source, isolatedReplInputLimit+1))
			if err != nil {
				return nil, err
			}
			if len(data) > isolatedReplInputLimit {
				return nil, errors.New("isolated REPL input limit exceeded")
			}
			return cancelreader.NewReader(bytes.NewReader(data))
		}
		if info.Mode()&os.ModeNamedPipe != 0 || output.IsTerminal(source) {
			return cancelreader.NewReader(source)
		}
	}
	return nil, errors.New("isolated REPL requires terminal, pipe, regular-file or bounded memory input")
}
