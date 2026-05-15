//go:build bench

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

package bench

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
)

const (
	dotProductSize = 1024
)

func BenchmarkDotProductNative(b *testing.B) {
	a := make([]float64, dotProductSize)
	c := make([]float64, dotProductSize)
	for i := range a {
		a[i] = float64(i) * 0.5
		c[i] = float64(i) * 0.25
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sum := 0.0
		for i := range a {
			sum += a[i] * c[i]
		}
		_ = sum
	}
}

func BenchmarkDotProductPipit(b *testing.B) {
	const source = `package main
const dotProductSize = 1024
func EntrypointRun() float64 {
	a := make([]float64, dotProductSize)
	c := make([]float64, dotProductSize)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i) * 0.5
		c[i] = float64(i) * 0.25
	}
	sum := 0.0
	for i := 0; i < len(a); i++ {
		sum += a[i] * c[i]
	}
	return sum
}`
	service := app.NewService(app.WithMaxCallDepth(200000))
	compiled, err := service.CompileFileSet(context.Background(),
		map[string]string{"main.go": source})
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	if _, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun"); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
		if err != nil {
			b.Fatalf("exec: %v", err)
		}
	}
}

func BenchmarkDotProductPipitLoopOnly(b *testing.B) {
	const source = `package main
const dotProductSize = 1024
var a []float64
var c []float64
func init() {
	a = make([]float64, dotProductSize)
	c = make([]float64, dotProductSize)
	for i := 0; i < len(a); i++ {
		a[i] = float64(i) * 0.5
		c[i] = float64(i) * 0.25
	}
}
func EntrypointRun() float64 {
	sum := 0.0
	for i := 0; i < len(a); i++ {
		sum += a[i] * c[i]
	}
	return sum
}`
	service := app.NewService(app.WithMaxCallDepth(200000))
	compiled, err := service.CompileFileSet(context.Background(),
		map[string]string{"main.go": source})
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	if _, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun"); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
		if err != nil {
			b.Fatalf("exec: %v", err)
		}
	}
}
