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

package engine

import (
	"fmt"
	"io"
	"reflect"
	"sync/atomic"
)

var (
	// errReadCallLimit reports that a script reader exhausted its call limit. It must
	// differ from io.EOF to distinguish truncation from complete data.
	errReadCallLimit = fmt.Errorf("pipit: a script reader served more than %d Read calls", pipitReaderMaxCalls)

	// errReaderStalled ends a script reader whose last pipitReaderMaxNoProgressCalls reads
	// all returned no data and no error. It wraps io.ErrNoProgress.
	errReaderStalled = fmt.Errorf("%w: a script reader returned no data %d times in a row",
		io.ErrNoProgress, pipitReaderMaxNoProgressCalls)
)

// readGuard limits script reads and detects repeated calls without progress. Atomic
// counters support concurrent io.ReaderAt calls.
type readGuard struct {
	// calls counts the Read calls so far.
	calls atomic.Int64

	// noProgress counts the calls in a row that returned no data and no error.
	noProgress atomic.Int64
}

// admit counts a Read call and reports whether it may run.
//
// Returns error which is errReadCallLimit after pipitReaderMaxCalls calls, or
// errReaderStalled after pipitReaderMaxNoProgressCalls reads in a row made no progress;
// the caller returns it without calling the script.
func (g *readGuard) admit() error {
	if g.calls.Add(1) > pipitReaderMaxCalls {
		return errReadCallLimit
	}
	if g.noProgress.Load() >= pipitReaderMaxNoProgressCalls {
		return errReaderStalled
	}
	return nil
}

// settle clamps the script's read count to the buffer and reports io.ErrNoProgress when
// a non-empty read returns neither data nor an error.
//
// Takes n (int64) which is the count the script method returned.
// Takes size (int) which is the buffer's length.
// Takes err (error) which is the error the script method returned.
//
// Returns int which is the count native code sees.
// Returns error which is the error native code sees.
func (g *readGuard) settle(n int64, size int, err error) (int, error) {
	count := int(min(max(n, 0), int64(size)))
	if count == 0 && err == nil && size > 0 {
		g.noProgress.Add(1)
		return 0, io.ErrNoProgress
	}
	if count > 0 {
		g.noProgress.Store(0)
	}
	return count, err
}

// resultError returns one result of a script method as the error native code sees.
//
// Takes vm (*VM) which builds adapters.
// Takes results ([]reflect.Value) which are the method's results.
// Takes index (int) which selects the error result.
//
// Returns error which is the result, or an error saying the result is not an error.
func resultError(vm *VM, results []reflect.Value, index int) error {
	value, present := presentResult(results, index)
	if !present {
		return nil
	}
	if err, ok := reflect.TypeAssert[error](value); ok {
		return err
	}
	if adapted, ok := adaptForInterfaceSlot(vm, value, errorReflectType); ok {
		if err, isError := reflect.TypeAssert[error](adapted); isError {
			return err
		}
	}
	return fmt.Errorf("pipit: result %d of type %s is not an error", index, scriptTypeString(value.Type()))
}
