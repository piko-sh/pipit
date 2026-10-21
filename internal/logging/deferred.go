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

package logging

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// maxDeferredRecords bounds the records a Deferred keeps between replays. Later ones are
// counted and reported as dropped.
const maxDeferredRecords = 256

// Deferred keeps the records logged where no context is at hand, such as while a service
// is constructed, until a call that has a context replays them through its logger.
type Deferred struct {
	// records holds the kept records in the order they were logged.
	records []slog.Record

	// dropped counts the records past maxDeferredRecords since the last replay.
	dropped int

	// mu guards records and dropped.
	mu sync.Mutex
}

// NewDeferred returns an empty Deferred.
//
// Returns *Deferred which keeps nothing yet.
func NewDeferred() *Deferred {
	return &Deferred{records: nil, dropped: 0, mu: sync.Mutex{}}
}

// Context returns a root context whose logger keeps every record in d. It is for work
// done where no caller's context exists, such as construction.
//
// Returns context.Context which carries the keeping logger.
func (d *Deferred) Context() context.Context {
	return WithLogger(context.Background(), slog.New(&deferredHandler{deferred: d, attrs: nil, groups: nil}))
}

// Replay logs the kept records through the logger ctx carries, oldest first, and forgets
// them, so each record is reported once.
//
// Takes ctx (context.Context) which carries the logger to report through.
//
// Safe for concurrent use. The mutex guards taking and clearing records. Logging runs
// after the mutex is released.
func (d *Deferred) Replay(ctx context.Context) {
	d.mu.Lock()
	records, dropped := d.records, d.dropped
	d.records, d.dropped = nil, 0
	d.mu.Unlock()
	if len(records) == 0 && dropped == 0 {
		return
	}
	ctx, logger := From(ctx)
	handler := logger.Handler()
	for index := range records {
		if handler.Enabled(ctx, records[index].Level) {
			_ = handler.Handle(ctx, records[index])
		}
	}
	if dropped > 0 {
		logger.WarnContext(ctx, "Further records logged before a context was available were dropped", slog.Int("dropped", dropped))
	}
}

// keep stores record, or counts it as dropped once the bound is reached.
//
// Takes record (*slog.Record) which is owned by d from now on.
//
// Safe for concurrent use. The mutex guards the records and dropped count.
func (d *Deferred) keep(record *slog.Record) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.records) >= maxDeferredRecords {
		d.dropped++
		return
	}
	d.records = append(d.records, *record)
}

// deferredHandler is the slog.Handler of a Deferred's logger.
type deferredHandler struct {
	// deferred receives the records.
	deferred *Deferred

	// attrs holds the attributes added through WithAttrs, already inside groups.
	attrs []slog.Attr

	// groups holds the group names opened through WithGroup, outermost first.
	groups []string
}

// Enabled keeps every level. The replaying logger's handler filters.
//
// Takes level (slog.Level) which is accepted without filtering.
//
// Returns bool which is always true.
func (*deferredHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle keeps a copy of record with the handler's attributes and groups applied.
//
// Takes record (slog.Record) which is the record to keep.
//
// Returns error which is always nil.
func (h *deferredHandler) Handle(_ context.Context, record slog.Record) error {
	kept := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	attrs := append([]slog.Attr(nil), h.attrs...)
	var own []slog.Attr
	record.Attrs(func(attr slog.Attr) bool {
		own = append(own, attr)
		return true
	})
	attrs = append(attrs, h.grouped(own)...)
	kept.AddAttrs(attrs...)
	h.deferred.keep(&kept)
	return nil
}

// WithAttrs returns a handler that adds attrs, inside the open groups, to every record.
//
// Takes attrs ([]slog.Attr) which are added.
//
// Returns slog.Handler which is the extended handler.
func (h *deferredHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &deferredHandler{deferred: h.deferred, attrs: append([]slog.Attr(nil), h.attrs...), groups: h.groups}
	next.attrs = append(next.attrs, h.grouped(attrs)...)
	return next
}

// WithGroup returns a handler that nests later attributes under name.
//
// Takes name (string) which is the group. Empty returns h.
//
// Returns slog.Handler which is the nested handler.
func (h *deferredHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := append(append([]string(nil), h.groups...), name)
	return &deferredHandler{deferred: h.deferred, attrs: h.attrs, groups: groups}
}

// grouped nests attrs inside the open groups, innermost last.
//
// Takes attrs ([]slog.Attr) which are the attributes to nest.
//
// Returns []slog.Attr which holds attrs, or one group attribute holding them.
func (h *deferredHandler) grouped(attrs []slog.Attr) []slog.Attr {
	if len(attrs) == 0 {
		return nil
	}
	for _, group := range slices.Backward(h.groups) {
		values := make([]any, len(attrs))
		for at := range attrs {
			values[at] = attrs[at]
		}
		attrs = []slog.Attr{slog.Group(group, values...)}
	}
	return attrs
}
