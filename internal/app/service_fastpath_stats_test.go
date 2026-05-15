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

package app

import (
	"context"
	"errors"
	"testing"

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/isa"
)

func TestRecordFastPathTypeMismatchIncrementsAndReturnsError(t *testing.T) {
	t.Parallel()
	service := NewService()
	vm := engine.NewVM(service.applyMaxExecutionTimeCtx(), service.globals, service.symbols)
	vm.Limits = service.limits

	err := engine.RecordFastPathTypeMismatch(vm, 7, "func(string) string", "func() int")
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if firstOf(vm.Limits.FastPathDiagnostics().Counts()) != 1 {
		t.Fatalf("typeMismatches counter not incremented")
	}
}

func TestRecordFastPathOOBWriteIncrements(t *testing.T) {
	t.Parallel()
	service := NewService()
	vm := engine.NewVM(service.applyMaxExecutionTimeCtx(), service.globals, service.symbols)
	vm.Limits = service.limits

	if err := engine.RecordFastPathOOBWrite(vm, isa.RegisterInt, 999, 256); err == nil {
		t.Fatalf("expected non-nil error")
	}
	if secondOf(vm.Limits.FastPathDiagnostics().Counts()) != 1 {
		t.Fatalf("outOfBoundsWrites counter not incremented")
	}
}

func TestFastPathStatsAggregatesAcrossIncrements(t *testing.T) {
	t.Parallel()
	service := NewService()
	vm := engine.NewVM(service.applyMaxExecutionTimeCtx(), service.globals, service.symbols)
	vm.Limits = service.limits

	_ = engine.RecordFastPathTypeMismatch(vm, 1, "a", "b")
	_ = engine.RecordFastPathTypeMismatch(vm, 2, "c", "d")
	_ = engine.RecordFastPathOOBWrite(vm, isa.RegisterString, 1024, 256)

	stats := service.FastPathStats()
	if stats.TypeMismatches < 2 {
		t.Errorf("TypeMismatches = %d, want >= 2", stats.TypeMismatches)
	}
	if stats.OutOfBoundsWrites < 1 {
		t.Errorf("OutOfBoundsWrites = %d, want >= 1", stats.OutOfBoundsWrites)
	}
}

func TestRecordFastPathTypeMismatchTolerantOfNilVM(t *testing.T) {
	t.Parallel()
	err := engine.RecordFastPathTypeMismatch(nil, 0, "x", "y")
	if err == nil {
		t.Fatalf("expected non-nil error for nil VM")
	}
	if errors.Is(err, nil) {
		t.Fatalf("error should be non-nil")
	}
}

func (s *Service) applyMaxExecutionTimeCtx() context.Context {
	return context.Background()
}

func firstOf(a, _ int64) int64 { return a }

func secondOf(_, b int64) int64 { return b }
