//go:build integration

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

// Package registry_test proves the engine's interface-adapter registration seam on a
// standard-library interface the closed set does not cover, in its own package so the
// process-wide registration cannot reach the interop tests that expect that interface to
// be refused.
package registry_test

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/sdk/stdlib"
)

type Visitor interface {
	Visit(depth int) int
}

type seekerAdapter struct{ core *engine.AdapterCore }

func (a *seekerAdapter) Seek(offset int64, whence int) (int64, error) {
	results, err := a.core.Invoke("Seek", reflect.ValueOf(offset), reflect.ValueOf(whence))
	if err != nil {
		return 0, err
	}
	position, _ := reflect.TypeAssert[int64](results[0])
	return position, errorResult(results[1])
}

type visitorAdapter struct{ core *engine.AdapterCore }

func (a *visitorAdapter) Visit(depth int) int {
	results, err := a.core.Invoke("Visit", reflect.ValueOf(depth))
	if err != nil {
		return -1
	}
	return int(results[0].Int())
}

func errorResult(value reflect.Value) error {
	if !value.IsValid() || value.IsNil() {
		return nil
	}
	err, _ := reflect.TypeAssert[error](value)
	return err
}

func init() {
	engine.RegisterInterfaceAdapter(reflect.TypeFor[io.Seeker](), func(core *engine.AdapterCore) reflect.Value {
		return reflect.ValueOf(&seekerAdapter{core: core})
	})
	engine.RegisterInterfaceAdapter(reflect.TypeFor[Visitor](), func(core *engine.AdapterCore) reflect.Value {
		return reflect.ValueOf(&visitorAdapter{core: core})
	})
}

func newRegistryService(t *testing.T) *app.Service {
	t.Helper()
	service := app.NewService()
	service.UseSymbolProviders(stdlib.Providers()...)
	service.RegisterPackage("host", map[string]reflect.Value{
		"TakeSeeker": reflect.ValueOf(func(s io.Seeker) string {
			position, err := s.Seek(42, io.SeekStart)
			return fmt.Sprintf("%d|%v", position, err)
		}),
		"Walk": reflect.ValueOf(func(v Visitor, depth int) string {
			return fmt.Sprint(v.Visit(depth))
		}),
		"ReadByte": reflect.ValueOf(func(r io.ByteReader) string {
			b, err := r.ReadByte()
			return fmt.Sprintf("%c|%v", b, err)
		}),
	})
	return service
}

func TestRegisteredSeekerAdapter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "value receiver method with an error result",
			code: `
import "host"
type Cursor struct{ pos int64 }
func (c *Cursor) Seek(offset int64, whence int) (int64, error) { c.pos = offset + int64(whence); return c.pos, nil }
host.TakeSeeker(&Cursor{})`,
			want: "42|<nil>",
		},
		{
			name: "error result crosses back",
			code: `
import (
	"errors"
	"host"
)
type Broken struct{}
func (Broken) Seek(offset int64, whence int) (int64, error) { return -1, errors.New("no seeking") }
host.TakeSeeker(Broken{})`,
			want: "-1|no seeking",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := newRegistryService(t).Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, fmt.Sprint(result))
		})
	}
}

func TestRegisteredAdapterReentersTheHostOnTheSameReceiver(t *testing.T) {
	t.Parallel()
	result, err := newRegistryService(t).Eval(context.Background(), `
import "host"
type walker struct{ seen []int }
func (w *walker) Visit(depth int) int {
	w.seen = append(w.seen, depth)
	if depth > 0 {
		host.Walk(w, depth-1)
	}
	return len(w.seen)
}
host.Walk(&walker{}, 3)`)
	require.NoError(t, err)
	require.Equal(t, "4", fmt.Sprint(result))
}

func TestUnregisteredInterfaceIsStillRefused(t *testing.T) {
	t.Parallel()
	_, err := newRegistryService(t).Eval(context.Background(), `
import "host"
type Source struct{}
func (Source) ReadByte() (byte, error) { return 'x', nil }
host.ReadByte(Source{})`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported interface argument")
	require.Contains(t, err.Error(), "io.ByteReader")
}
