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

package interop_test

import (
	"container/heap"
	"context"
	"encoding"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib"
)

func newInteropService(t *testing.T) *app.Service {
	t.Helper()
	service := app.NewService()
	service.UseSymbolProviders(stdlib.Providers()...)
	service.RegisterPackage("host", map[string]reflect.Value{
		"TakeSeeker": reflect.ValueOf(func(s io.Seeker) string {
			_, err := s.Seek(0, io.SeekStart)
			return fmt.Sprint(err)
		}),
		"CloseIt": reflect.ValueOf(func(c io.Closer) string {
			return fmt.Sprint(c.Close())
		}),
		"Drain": reflect.ValueOf(func(rc io.ReadCloser) string {
			data, err := io.ReadAll(rc)
			return fmt.Sprintf("%s|%v|%v", data, err, rc.Close())
		}),
		"WriteAndClose": reflect.ValueOf(func(wc io.WriteCloser) string {
			n, err := wc.Write([]byte("payload"))
			return fmt.Sprintf("%d|%v|%v", n, err, wc.Close())
		}),
		"Echo": reflect.ValueOf(func(rw io.ReadWriter) string {
			_, _ = rw.Write([]byte("abc"))
			data, err := io.ReadAll(rw)
			return fmt.Sprintf("%s|%v", data, err)
		}),
		"FillFrom": reflect.ValueOf(func(rf io.ReaderFrom) string {
			n, err := rf.ReadFrom(strings.NewReader("from-native"))
			return fmt.Sprintf("%d|%v", n, err)
		}),
		"SpillTo": reflect.ValueOf(func(wt io.WriterTo) string {
			var sink strings.Builder
			n, err := wt.WriteTo(&sink)
			return fmt.Sprintf("%d|%s|%v", n, sink.String(), err)
		}),
		"MarshalText": reflect.ValueOf(func(m encoding.TextMarshaler) string {
			text, err := m.MarshalText()
			return fmt.Sprintf("%s|%v", text, err)
		}),
		"UnmarshalText": reflect.ValueOf(func(u encoding.TextUnmarshaler) string {
			return fmt.Sprint(u.UnmarshalText([]byte("blue")))
		}),
		"Serve": reflect.ValueOf(func(h http.Handler) string {
			recorder := httptest.NewRecorder()
			h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ping", nil))
			return fmt.Sprintf("%d|%s", recorder.Code, recorder.Body.String())
		}),
		"HeapDrain": reflect.ValueOf(func(h heap.Interface) string {
			heap.Init(h)
			heap.Push(h, 2)
			heap.Push(h, 9)
			heap.Push(h, 1)
			var out []string
			for h.Len() > 0 {
				out = append(out, fmt.Sprint(heap.Pop(h)))
			}
			return strings.Join(out, ",")
		}),
	})
	return service
}

func TestNativeCallUnsupportedInterfaceSurfacesNamedError(t *testing.T) {
	t.Parallel()
	service := newInteropService(t)

	_, err := service.Eval(context.Background(), `
import "host"
type Cursor struct{ pos int64 }
func (c *Cursor) Seek(offset int64, whence int) (int64, error) { c.pos = offset; return offset, nil }
host.TakeSeeker(&Cursor{})`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported interface argument")
	require.Contains(t, err.Error(), "io.Seeker")
}

func TestNativeInterfaceAdapterRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		imports string
		code    string
		expect  any
	}{
		{name: "io.Closer", imports: `"fmt"; "host"`, expect: "<nil>|true", code: `
type Res struct{ closed bool }
func (r *Res) Close() error { r.closed = true; return nil }
r := &Res{}
host.CloseIt(r) + "|" + fmt.Sprint(r.closed)`},
		{name: "io.ReadCloser", imports: `"fmt"; "host"; "io"`, expect: "hello|<nil>|<nil>|true", code: `
type Stream struct{ data []byte; pos int; closed bool }
func (s *Stream) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) { return 0, io.EOF }
	n := copy(p, s.data[s.pos:]); s.pos += n; return n, nil
}
func (s *Stream) Close() error { s.closed = true; return nil }
s := &Stream{data: []byte("hello")}
host.Drain(s) + "|" + fmt.Sprint(s.closed)`},
		{name: "io.WriteCloser", imports: `"fmt"; "host"`, expect: "7|<nil>|<nil>|payload|true", code: `
type Sink struct{ buffer []byte; closed bool }
func (s *Sink) Write(p []byte) (int, error) { s.buffer = append(s.buffer, p...); return len(p), nil }
func (s *Sink) Close() error { s.closed = true; return nil }
s := &Sink{}
host.WriteAndClose(s) + "|" + string(s.buffer) + "|" + fmt.Sprint(s.closed)`},
		{name: "io.ReadWriter", imports: `"host"; "io"`, expect: "abc|<nil>", code: `
type Pipe struct{ buffer []byte }
func (p *Pipe) Write(b []byte) (int, error) { p.buffer = append(p.buffer, b...); return len(b), nil }
func (p *Pipe) Read(b []byte) (int, error) {
	if len(p.buffer) == 0 { return 0, io.EOF }
	n := copy(b, p.buffer); p.buffer = p.buffer[n:]; return n, nil
}
host.Echo(&Pipe{})`},
		{name: "io.ReaderFrom", imports: `"host"; "io"`, expect: "11|<nil>|from-native", code: `
type Collector struct{ data []byte }
func (c *Collector) ReadFrom(r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	c.data = append(c.data, data...)
	return int64(len(data)), err
}
c := &Collector{}
host.FillFrom(c) + "|" + string(c.data)`},
		{name: "io.WriterTo", imports: `"host"; "io"`, expect: "6|spill!|<nil>", code: `
type Source struct{ data string }
func (s Source) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write([]byte(s.data))
	return int64(n), err
}
host.SpillTo(Source{data: "spill!"})`},
		{name: "encoding.TextMarshaler", imports: `"host"`, expect: "colour:red|<nil>", code: `
type Colour struct{ name string }
func (c Colour) MarshalText() ([]byte, error) { return []byte("colour:" + c.name), nil }
host.MarshalText(Colour{name: "red"})`},
		{name: "encoding.TextUnmarshaler", imports: `"host"`, expect: "<nil>|blue", code: `
type Colour struct{ name string }
func (c *Colour) UnmarshalText(text []byte) error { c.name = string(text); return nil }
c := &Colour{}
host.UnmarshalText(c) + "|" + c.name`},
		{name: "http.Handler", imports: `"fmt"; "host"; "net/http"`, expect: "202|pong:/ping|1", code: `
type Ping struct{ hits int }
func (p *Ping) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.hits++
	w.WriteHeader(202)
	fmt.Fprintf(w, "pong:%s", r.URL.Path)
}
p := &Ping{}
host.Serve(p) + "|" + fmt.Sprint(p.hits)`},
		{name: "heap.Interface", imports: `"host"`, expect: "1,2,9", code: `
type IntHeap struct{ items []int }
func (h *IntHeap) Len() int { return len(h.items) }
func (h *IntHeap) Less(i, j int) bool { return h.items[i] < h.items[j] }
func (h *IntHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *IntHeap) Push(x any) { v := x.(int); h.items = append(h.items, v) }
func (h *IntHeap) Pop() any {
	last := h.items[len(h.items)-1]
	h.items = h.items[:len(h.items)-1]
	return last
}
host.HeapDrain(&IntHeap{})`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := newInteropService(t)
			source := "import (" + tt.imports + ")\n" + tt.code
			result, err := service.Eval(context.Background(), source)
			require.NoError(t, err)
			require.Equal(t, tt.expect, result)
		})
	}
}
