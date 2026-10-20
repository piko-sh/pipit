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
	"errors"
	"io"
	"io/fs"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadGuard(t *testing.T) {
	t.Parallel()

	var guard readGuard
	require.NoError(t, guard.admit())
	count, err := guard.settle(9, 4, nil)
	require.NoError(t, err)
	require.Equal(t, 4, count, "a count past the buffer is clamped")
	count, err = guard.settle(-1, 4, io.EOF)
	require.ErrorIs(t, err, io.EOF)
	require.Zero(t, count, "a negative count is clamped")
	count, err = guard.settle(1<<40, 4, nil)
	require.NoError(t, err)
	require.Equal(t, 4, count, "a count past the int range is clamped before it is converted")
	count, err = guard.settle(0, 0, nil)
	require.NoError(t, err, "an empty buffer reads nothing without breaking the contract")
	require.Zero(t, count)
}

func TestReadGuardStopsAReaderThatMakesNoProgress(t *testing.T) {
	t.Parallel()

	var guard readGuard
	for range pipitReaderMaxNoProgressCalls - 1 {
		require.NoError(t, guard.admit())
		_, err := guard.settle(0, 4, nil)
		require.ErrorIs(t, err, io.ErrNoProgress)
	}
	_, err := guard.settle(2, 4, nil)
	require.NoError(t, err, "a read that makes progress ends the run")

	for range pipitReaderMaxNoProgressCalls {
		require.NoError(t, guard.admit())
		_, err = guard.settle(0, 4, nil)
		require.ErrorIs(t, err, io.ErrNoProgress)
	}
	err = guard.admit()
	require.ErrorIs(t, err, errReaderStalled)
	require.ErrorIs(t, err, io.ErrNoProgress)
	require.NotErrorIs(t, err, io.EOF, "the reader is stopped, not ended")
}

func TestReadGuardLimitsTheCalls(t *testing.T) {
	t.Parallel()

	var guard readGuard
	guard.calls.Store(pipitReaderMaxCalls)
	err := guard.admit()
	require.ErrorIs(t, err, errReadCallLimit)
	require.NotErrorIs(t, err, io.EOF, "the cut is not mistaken for the end of the data")
}

func TestResultError(t *testing.T) {
	t.Parallel()

	failure := errors.New("failed")

	t.Run("the error comes from the selected result", func(t *testing.T) {
		t.Parallel()

		results := []reflect.Value{reflect.ValueOf([]byte("raw")), reflect.ValueOf(failure)}
		require.ErrorIs(t, resultError(nil, results, 1), failure)
		require.ErrorIs(t, resultError(nil, []reflect.Value{reflect.ValueOf(failure)}, 0), failure, "a single error result is read")
		require.NoError(t, resultError(nil, []reflect.Value{reflect.ValueOf(3)}, 1), "a missing result is no error")
		require.NoError(t, resultError(nil, []reflect.Value{reflect.ValueOf(3), reflect.Value{}}, 1))
		require.NoError(t, resultError(nil, []reflect.Value{reflect.Zero(errorReflectType)}, 0), "a nil interface is no error")
	})

	t.Run("a typed nil error stays an error", func(t *testing.T) {
		t.Parallel()

		var typedNil *fs.PathError
		err := resultError(nil, []reflect.Value{reflect.ValueOf(typedNil)}, 0)
		require.Error(t, err)
		pathErr, isPathErr := errors.AsType[*fs.PathError](err)
		require.True(t, isPathErr)
		require.Nil(t, pathErr, "the error holds the typed nil, as in Go")
	})

	t.Run("a result that is not an error is reported", func(t *testing.T) {
		t.Parallel()

		err := resultError(nil, []reflect.Value{reflect.ValueOf(3), reflect.ValueOf(4)}, 1)
		require.ErrorContains(t, err, "result 1 of type int is not an error")
	})
}
