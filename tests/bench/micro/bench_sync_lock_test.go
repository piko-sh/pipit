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

//go:build bench

package bench

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/sdk/stdlib"
)

const syncLockSrc = `package main

import "sync"

var mu sync.Mutex
var rw sync.RWMutex
var count int

func LockLoop() int {
	for i := 0; i < 1000; i++ {
		mu.Lock()
		count++
		mu.Unlock()
	}
	return count
}

func bump() {
	mu.Lock()
	defer mu.Unlock()
	count++
}

func DeferLoop() int {
	for i := 0; i < 1000; i++ {
		bump()
	}
	return count
}

func ReadLoop() int {
	n := 0
	for i := 0; i < 1000; i++ {
		rw.RLock()
		n += count
		rw.RUnlock()
	}
	return n
}

func LocalLoop() int {
	var local sync.Mutex
	n := 0
	for i := 0; i < 1000; i++ {
		local.Lock()
		n++
		local.Unlock()
	}
	return n
}
`

func benchSyncLock(b *testing.B, function string, options ...app.Option) {
	b.Helper()
	service := app.NewService(options...)
	require.NoError(b, service.UseSymbolProviders(stdlib.Providers()...))
	ctx := context.WithoutCancel(b.Context())
	compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": syncLockSrc})
	require.NoError(b, err)
	require.NoError(b, service.ExecuteInits(ctx, compiled))
	b.ReportAllocs()
	for b.Loop() {
		_, err = service.CallFunction(ctx, compiled, function)
		require.NoError(b, err)
	}
}

func BenchmarkSyncLock(b *testing.B) {
	for _, function := range []string{"LockLoop", "DeferLoop", "ReadLoop", "LocalLoop"} {
		b.Run(function, func(b *testing.B) {
			benchSyncLock(b, function)
		})
	}
}
