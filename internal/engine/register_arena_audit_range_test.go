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

//go:build pipit_reuse_audit

package engine

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditDumpScannerRejectsAMalformedDump(t *testing.T) {
	cases := []struct {
		name string
		dump string
		want string
	}{
		{name: "short", dump: "go1", want: "bad header"},
		{name: "wrong header", dump: strings.Repeat("x", len(auditHeapDumpHeader)), want: "bad header"},
		{name: "unknown tag", dump: auditHeapDumpHeader + "\x7f", want: "unknown record tag 127"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scanner := &auditDumpScanner{reader: bufio.NewReader(strings.NewReader(testCase.dump)), ranges: nil, hits: map[auditAddressRange]bool{}}

			err := scanner.scan()

			require.ErrorIs(t, err, errAuditHeapDumpMalformed)
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func auditDumpStream(values ...any) *auditDumpScanner {
	var encoded []byte
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			encoded = binary.AppendUvarint(encoded, uint64(typed))
		case string:
			encoded = binary.AppendUvarint(encoded, uint64(len(typed)))
			encoded = append(encoded, typed...)
		}
	}
	ranges := []auditAddressRange{{start: 0x1000, end: 0x2000}}
	return &auditDumpScanner{reader: bufio.NewReader(bytes.NewReader(encoded)), ranges: ranges, hits: map[auditAddressRange]bool{}}
}

func TestAuditDumpScannerSkipsAMemoryProfileRecord(t *testing.T) {
	scanner := auditDumpStream(1, 2, 2, "function", "file.go", 10, "other", "other.go", 20, 3, 4)
	require.NoError(t, scanner.memProf())
	_, err := scanner.uvarint()
	require.ErrorIs(t, err, io.EOF, "the whole record was read")

	for name, truncated := range map[string]*auditDumpScanner{
		"in the header":       auditDumpStream(1),
		"in the frame count":  auditDumpStream(1, 2),
		"in a frame's name":   auditDumpStream(1, 2, 1),
		"in a frame's file":   auditDumpStream(1, 2, 1, "function"),
		"in a frame's line":   auditDumpStream(1, 2, 1, "function", "file.go"),
		"in the closing ints": auditDumpStream(1, 2, 0),
	} {
		require.Error(t, truncated.memProf(), name)
	}
}

func TestAuditDumpScannerNotesInterfaceFieldsTwoWords(t *testing.T) {
	contents := make([]byte, 16)
	binary.LittleEndian.PutUint64(contents[8:], 0x1800)
	scanner := auditDumpStream(auditDumpFieldIface, 0, auditDumpFieldEol)
	require.NoError(t, scanner.fields(contents))
	require.True(t, scanner.hits[auditAddressRange{start: 0x1000, end: 0x2000}], "the interface's data word points into the range")

	scanner = auditDumpStream(auditDumpFieldEface, 16, auditDumpFieldEol)
	require.NoError(t, scanner.fields(contents), "a field past the contents is ignored")
	require.Error(t, auditDumpStream(auditDumpFieldIface).fields(contents), "a truncated field list")
	require.Error(t, auditDumpStream().fields(contents))
}
