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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"slices"

	"piko.sh/safeconv"
)

// auditHeapDumpHeader is the heap dump format's header (runtime/heapdump.go).
const auditHeapDumpHeader = "go1.7 heap dump\n"

const (
	// auditDumpEOF marks the end of a runtime heap dump.
	auditDumpEOF = 0

	// auditDumpObject identifies object records in a runtime heap dump.
	auditDumpObject = 1

	// auditDumpOtherRoot identifies other root records in a runtime heap dump.
	auditDumpOtherRoot = 2

	// auditDumpType identifies type records in a runtime heap dump.
	auditDumpType = 3

	// auditDumpGoroutine identifies goroutine records in a runtime heap dump.
	auditDumpGoroutine = 4

	// auditDumpStackFrame identifies stack frame records in a runtime heap dump.
	auditDumpStackFrame = 5

	// auditDumpParams identifies runtime-parameter records in a heap dump.
	auditDumpParams = 6

	// auditDumpFinaliser identifies finaliser records in a runtime heap dump.
	auditDumpFinaliser = 7

	// auditDumpItab identifies itab records in a runtime heap dump.
	auditDumpItab = 8

	// auditDumpOSThread identifies operating-system thread records in a runtime heap dump.
	auditDumpOSThread = 9

	// auditDumpMemStats identifies memory-statistics records in a runtime heap dump.
	auditDumpMemStats = 10

	// auditDumpQueuedFinaliser identifies queued finaliser records in a runtime heap dump.
	auditDumpQueuedFinaliser = 11

	// auditDumpData identifies data records in a runtime heap dump.
	auditDumpData = 12

	// auditDumpBSS identifies zero-initialised global data in a runtime heap dump.
	auditDumpBSS = 13

	// auditDumpDefer identifies defer records in a runtime heap dump.
	auditDumpDefer = 14

	// auditDumpPanic identifies panic records in a runtime heap dump.
	auditDumpPanic = 15

	// auditDumpMemProf identifies memory-profile records in a runtime heap dump.
	auditDumpMemProf = 16

	// auditDumpAllocSample identifies allocation samples in a runtime heap dump.
	auditDumpAllocSample = 17

	// auditDumpFieldEol identifies the end of a field list in a runtime heap dump.
	auditDumpFieldEol = 0

	// auditDumpFieldIface identifies the interface field kind in a runtime heap dump.
	auditDumpFieldIface = 2

	// auditDumpFieldEface identifies the empty-interface field kind in a runtime heap dump.
	auditDumpFieldEface = 3

	// auditDumpMemStatsWords is the number of integers in a memstats record.
	auditDumpMemStatsWords = 24 + 256 + 1
)

// auditDumpOp is one step of reading a heap dump record.
type auditDumpOp uint8

const (
	// auditDumpSkipInts reads and discards count integers.
	auditDumpSkipInts auditDumpOp = iota

	// auditDumpSkipString reads and discards a length-prefixed string.
	auditDumpSkipString

	// auditDumpPointerInts reads count integers, each a pointer a root holds.
	auditDumpPointerInts

	// auditDumpContents reads a length-prefixed byte range for a following field list.
	auditDumpContents

	// auditDumpFields reads a field list over the byte range read last.
	auditDumpFields
)

// auditDumpWordBytes is the size of a pointer-sized word in a heap dump.
const auditDumpWordBytes = 8

// auditDumpStep is one step of a record layout.
type auditDumpStep struct {
	// op is the step's kind.
	op auditDumpOp

	// count is the number of integers for auditDumpSkipInts and auditDumpPointerInts.
	count int
}

// auditAddressRange is a run of a kept slab, [start, end).
type auditAddressRange struct {
	// start and end delimit the address range [start, end).
	start, end uintptr
}

// errAuditHeapDumpMalformed reports a heap dump the audit build cannot decode.
var errAuditHeapDumpMalformed = errors.New("engine: audit heap dump malformed")

// auditDumpLayouts holds each record's layout after its tag, indexed by tag
// (runtime/heapdump.go). A nil layout is a tag scan handles itself or does not know.
var auditDumpLayouts = [...][]auditDumpStep{
	auditDumpObject: {{op: auditDumpSkipInts, count: 1}, {op: auditDumpContents, count: 0}, {op: auditDumpFields, count: 0}},
	auditDumpData:   {{op: auditDumpSkipInts, count: 1}, {op: auditDumpContents, count: 0}, {op: auditDumpFields, count: 0}},
	auditDumpBSS:    {{op: auditDumpSkipInts, count: 1}, {op: auditDumpContents, count: 0}, {op: auditDumpFields, count: 0}},
	auditDumpStackFrame: {
		{op: auditDumpSkipInts, count: 3}, {op: auditDumpContents, count: 0}, {op: auditDumpSkipInts, count: 3},
		{op: auditDumpSkipString, count: 0}, {op: auditDumpFields, count: 0},
	},
	auditDumpOtherRoot:       {{op: auditDumpSkipString, count: 0}, {op: auditDumpPointerInts, count: 1}},
	auditDumpType:            {{op: auditDumpSkipInts, count: 2}, {op: auditDumpSkipString, count: 0}, {op: auditDumpSkipInts, count: 1}},
	auditDumpGoroutine:       {{op: auditDumpSkipInts, count: 8}, {op: auditDumpSkipString, count: 0}, {op: auditDumpPointerInts, count: 4}},
	auditDumpParams:          {{op: auditDumpSkipInts, count: 4}, {op: auditDumpSkipString, count: 0}, {op: auditDumpSkipString, count: 0}, {op: auditDumpSkipInts, count: 1}},
	auditDumpFinaliser:       {{op: auditDumpPointerInts, count: 5}},
	auditDumpQueuedFinaliser: {{op: auditDumpPointerInts, count: 5}},
	auditDumpItab:            {{op: auditDumpSkipInts, count: 2}},
	auditDumpAllocSample:     {{op: auditDumpSkipInts, count: 2}},
	auditDumpOSThread:        {{op: auditDumpSkipInts, count: 3}},
	auditDumpMemStats:        {{op: auditDumpSkipInts, count: auditDumpMemStatsWords}},
	auditDumpDefer:           {{op: auditDumpPointerInts, count: 7}},
	auditDumpPanic:           {{op: auditDumpPointerInts, count: 6}},
}

// auditDumpScanner reads a heap dump and notes the ranges its pointers fall into.
type auditDumpScanner struct {
	// reader supplies the encoded runtime heap dump.
	reader *bufio.Reader

	// ranges lists the arena address ranges being checked.
	ranges []auditAddressRange

	// hits records each range referenced by the heap dump.
	hits map[auditAddressRange]bool

	// buffer holds scratch bytes used while decoding the dump.
	buffer []byte
}

// auditRangesReferenced reports, for each range, whether a heap dump finds a pointer into
// it. The caller holds auditLog.runMu.
//
// Takes ranges ([]auditAddressRange) which are the runs to test.
//
// Returns []bool which is per range.
// Returns error when the dump could not be taken or read.
//
// Concurrency: Requires the caller to hold auditLog.runMu and protects the shared audit
// counters with auditLog.mu.
func auditRangesReferenced(ranges []auditAddressRange) ([]bool, error) {
	file, err := os.CreateTemp("", "pipit-audit-heapdump-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	runtime.GC()
	debug.WriteHeapDump(file.Fd())
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(left, right auditAddressRange) int {
		switch {
		case left.start < right.start:
			return -1
		case left.start > right.start:
			return 1
		default:
			return 0
		}
	})
	hits := make(map[auditAddressRange]bool, len(ranges))
	scanner := &auditDumpScanner{reader: bufio.NewReaderSize(file, 1<<20), ranges: sorted, hits: hits, buffer: nil}
	if err := scanner.scan(); err != nil {
		return nil, err
	}
	referenced := make([]bool, len(ranges))
	for index, r := range ranges {
		referenced[index] = hits[r]
	}
	auditLog.mu.Lock()
	auditLog.counters.RangeHeapDumps++
	auditLog.mu.Unlock()
	return referenced, nil
}

// scan reads the whole dump.
//
// Returns error when the dump is malformed.
func (s *auditDumpScanner) scan() error {
	header := make([]byte, len(auditHeapDumpHeader))
	if _, err := io.ReadFull(s.reader, header); err != nil {
		return fmt.Errorf("%w: bad header: %w", errAuditHeapDumpMalformed, err)
	}
	if string(header) != auditHeapDumpHeader {
		return fmt.Errorf("%w: bad header", errAuditHeapDumpMalformed)
	}
	for {
		tag, err := s.uvarint()
		if err != nil {
			return err
		}
		if tag == auditDumpEOF {
			return nil
		}
		if err := s.record(tag); err != nil {
			return err
		}
	}
}

// record reads one record after its tag.
//
// Takes tag (uint64) which is the record's tag.
//
// Returns error when the record is malformed or its tag unknown.
func (s *auditDumpScanner) record(tag uint64) error {
	if tag == auditDumpMemProf {
		return s.memProf()
	}
	if tag >= uint64(len(auditDumpLayouts)) || auditDumpLayouts[tag] == nil {
		return fmt.Errorf("%w: unknown record tag %d", errAuditHeapDumpMalformed, tag)
	}
	var contents []byte
	for _, step := range auditDumpLayouts[tag] {
		var err error
		switch step.op {
		case auditDumpSkipInts:
			err = s.skipInts(step.count)
		case auditDumpSkipString:
			err = s.skipString()
		case auditDumpPointerInts:
			err = s.pointerInts(step.count)
		case auditDumpContents:
			contents, err = s.memrange()
		case auditDumpFields:
			err = s.fields(contents)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// uvarint reads one integer.
//
// Returns uint64 which is the decoded integer.
// Returns error when the integer cannot be read or decoded; nil on success.
func (s *auditDumpScanner) uvarint() (uint64, error) {
	return binary.ReadUvarint(s.reader)
}

// skipInts reads and discards count integers.
//
// Takes count (int) which is the count.
//
// Returns error at the end of the input.
func (s *auditDumpScanner) skipInts(count int) error {
	for range count {
		if _, err := s.uvarint(); err != nil {
			return err
		}
	}
	return nil
}

// pointerInts reads count integers, each a pointer value a root holds.
//
// Takes count (int) which is the count.
//
// Returns error at the end of the input.
func (s *auditDumpScanner) pointerInts(count int) error {
	for range count {
		value, err := s.uvarint()
		if err != nil {
			return err
		}
		s.note(uintptr(value))
	}
	return nil
}

// memrange reads a length-prefixed byte range into the scanner's buffer.
//
// Returns []byte which is valid until the next read.
// Returns error when the length or byte range cannot be read; nil on success.
func (s *auditDumpScanner) memrange() ([]byte, error) {
	length, err := s.uvarint()
	if err != nil {
		return nil, err
	}
	if uint64(cap(s.buffer)) < length {
		s.buffer = make([]byte, length)
	}
	s.buffer = s.buffer[:length]
	if _, err := io.ReadFull(s.reader, s.buffer); err != nil {
		return nil, err
	}
	return s.buffer, nil
}

// skipString reads and discards a length-prefixed string.
//
// Returns error at the end of the input.
func (s *auditDumpScanner) skipString() error {
	length, err := s.uvarint()
	if err != nil {
		return err
	}
	_, err = s.reader.Discard(safeconv.Uint64ToInt(length))
	return err
}

// fields reads a field list and notes the pointer each field holds in contents.
//
// Takes contents ([]byte) which the field offsets index.
//
// Returns error at the end of the input.
func (s *auditDumpScanner) fields(contents []byte) error {
	for {
		kind, err := s.uvarint()
		if err != nil {
			return err
		}
		if kind == auditDumpFieldEol {
			return nil
		}
		offset, err := s.uvarint()
		if err != nil {
			return err
		}
		s.noteWord(contents, offset)
		if kind == auditDumpFieldIface || kind == auditDumpFieldEface {
			s.noteWord(contents, offset+auditDumpWordBytes)
		}
	}
}

// noteWord notes the pointer-sized little-endian word at offset in contents.
//
// Takes contents ([]byte) which holds the word.
// Takes offset (uint64) which is its offset.
func (s *auditDumpScanner) noteWord(contents []byte, offset uint64) {
	if offset+auditDumpWordBytes > uint64(len(contents)) {
		return
	}
	s.note(uintptr(binary.LittleEndian.Uint64(contents[offset : offset+auditDumpWordBytes])))
}

// note records the range address falls into, if any.
//
// Takes address (uintptr) which is a pointer value.
func (s *auditDumpScanner) note(address uintptr) {
	if address == 0 {
		return
	}
	index, _ := slices.BinarySearchFunc(s.ranges, address, func(candidate auditAddressRange, target uintptr) int {
		switch {
		case candidate.start > target:
			return 1
		case candidate.start < target:
			return -1
		default:
			return 0
		}
	})
	for _, candidate := range []int{index - 1, index} {
		if candidate >= 0 && candidate < len(s.ranges) && address >= s.ranges[candidate].start && address < s.ranges[candidate].end {
			s.hits[s.ranges[candidate]] = true
		}
	}
}

// memProf reads and discards a memory profile record.
//
// Returns error at the end of the input.
func (s *auditDumpScanner) memProf() error {
	if err := s.skipInts(2); err != nil {
		return err
	}
	frames, err := s.uvarint()
	if err != nil {
		return err
	}
	for range frames {
		if err := s.skipString(); err != nil {
			return err
		}
		if err := s.skipString(); err != nil {
			return err
		}
		if err := s.skipInts(1); err != nil {
			return err
		}
	}
	return s.skipInts(2)
}
