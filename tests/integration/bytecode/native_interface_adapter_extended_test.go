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

//go:build integration

package bytecode_test

import (
	"context"
	"io"
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"

	"github.com/stretchr/testify/require"
)

type interopPositioner interface {
	Position() int64
}

type interopPositionHolder struct {
	Pos interopPositioner
}

type interopNativePosition struct {
	at int64
}

func (p interopNativePosition) Position() int64 {
	return p.at
}

func newInteropService(t *testing.T) *app.Service {
	t.Helper()
	return newTestServiceWithFunctions(t, "host", map[string]reflect.Value{
		"TakePositioner": reflect.ValueOf(func(p interopPositioner) int64 {
			return p.Position()
		}),
		"NewPositionHolder": reflect.ValueOf(func() *interopPositionHolder { return &interopPositionHolder{} }),
		"NewFilledHolder": reflect.ValueOf(func(at int64) *interopPositionHolder {
			return &interopPositionHolder{Pos: interopNativePosition{at: at}}
		}),
		"ReadHolder": reflect.ValueOf(func(h *interopPositionHolder) int64 {
			if h.Pos == nil {
				return -1
			}
			return h.Pos.Position()
		}),
	})
}

func TestNativeInterfaceFieldRoundTripsThroughRealFieldType(t *testing.T) {
	t.Parallel()
	service := newInteropService(t)

	result, err := service.Eval(context.Background(), `
import "host"
filled := host.NewFilledHolder(41)
value := filled.Pos
empty := host.NewPositionHolder()
empty.Pos = value
host.ReadHolder(empty) + host.ReadHolder(filled) + host.TakePositioner(empty.Pos)`)
	require.NoError(t, err)
	require.Equal(t, int64(123), result)
}

func TestNativeCallUnsupportedInterfaceReportsNamedError(t *testing.T) {
	t.Parallel()
	service := newInteropService(t)

	_, err := service.Eval(context.Background(), `
import "host"
type Cursor struct{ pos int64 }
func (c *Cursor) Position() int64 { return c.pos }
host.TakePositioner(&Cursor{pos: 3})`)
	require.Error(t, err)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceArgument)
	require.Contains(t, err.Error(), "argument 0")
	require.Contains(t, err.Error(), "*Cursor")
	require.Contains(t, err.Error(), "interopPositioner")
	require.NotContains(t, err.Error(), "reflect: Call using")
}

func TestStructFieldUnsupportedInterfaceReportsNamedError(t *testing.T) {
	t.Parallel()
	service := newInteropService(t)

	_, err := service.Eval(context.Background(), `
import "host"
type Cursor struct{ pos int64 }
func (c *Cursor) Position() int64 { return c.pos }
holder := host.NewPositionHolder()
holder.Pos = &Cursor{pos: 3}
holder.Pos != nil`)
	require.Error(t, err)
	require.ErrorIs(t, err, fault.ErrUnsupportedInterfaceField)
	require.Contains(t, err.Error(), "*Cursor")
	require.Contains(t, err.Error(), "interopPositioner")
}

func TestIsSupportedAdapterInterfaceCoversExtendedSet(t *testing.T) {
	t.Parallel()

	for _, interfaceType := range []reflect.Type{
		engine.IOCloserReflectType, engine.IOReadCloserReflectType, engine.IOWriteCloserReflectType, engine.IoReadWriterReflectType,
		engine.IoReaderFromReflectType, engine.IoWriterToReflectType, engine.TextMarshalerReflectType, engine.TextUnmarshalerReflectType,
		engine.HTTPHandlerReflectType, engine.HeapInterfaceReflectType,
	} {
		require.True(t, engine.IsSupportedAdapterInterface(interfaceType), interfaceType.String())
	}
	require.False(t, engine.IsSupportedAdapterInterface(reflect.TypeFor[io.Seeker]()))
}

func TestNativeParameterTypeUnfoldsVariadics(t *testing.T) {
	t.Parallel()

	variadic := reflect.TypeOf(func(io.Reader, ...io.Closer) {})
	first, ok := engine.NativeParameterType(variadic, 0, false)
	require.True(t, ok)
	require.Equal(t, engine.IOReaderReflectType, first)
	element, ok := engine.NativeParameterType(variadic, 3, false)
	require.True(t, ok)
	require.Equal(t, engine.IOCloserReflectType, element)
	spread, ok := engine.NativeParameterType(variadic, 1, true)
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[[]io.Closer](), spread)
	_, ok = engine.NativeParameterType(variadic, 2, true)
	require.False(t, ok)
	_, ok = engine.NativeParameterType(reflect.TypeOf(func(int) {}), 1, false)
	require.False(t, ok)
}
