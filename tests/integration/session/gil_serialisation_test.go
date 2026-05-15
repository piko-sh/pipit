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

package session_test

import (
	"context"
	"testing"
	"time"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib"
)

const (
	gilWatchdogTimeout = 8 * time.Second
)

func runSafeModeFileWithWatchdog(t *testing.T, source string) any {
	t.Helper()

	type outcome struct {
		result any
		err    error
	}
	resultCh := make(chan outcome, 1)

	ctx, cancel := context.WithTimeout(context.Background(), gilWatchdogTimeout)
	defer cancel()

	go func() {
		service := app.NewService(app.WithSafeMode())
		service.UseSymbolProviders(stdlib.Providers()...)
		result, err := service.EvalFile(ctx, source, "run")
		resultCh <- outcome{result: result, err: err}
	}()

	select {
	case got := <-resultCh:
		require.NoError(t, got.err, "safe-mode EvalFile failed")
		return got.result
	case <-time.After(gilWatchdogTimeout + time.Second):
		t.Fatalf("safe-mode program deadlocked: watchdog fired after %s", gilWatchdogTimeout+time.Second)
		return nil
	}
}

func TestNativeCallbackDoesNotDeadlockUnderTheGIL(t *testing.T) {
	t.Parallel()

	t.Run("once.Do blocking callback", func(t *testing.T) {
		t.Parallel()
		const source = `
package main

import "sync"

func run() int {
	var once sync.Once
	feed := make(chan int)
	result := make(chan int)
	fin := make(chan int)
	go func() { feed <- 99 }()
	fn := func() {
		v := <-feed
		result <- v
	}
	go func() { once.Do(fn); fin <- 1 }()
	go func() { once.Do(fn); fin <- 1 }()
	r := <-result
	<-fin
	<-fin
	return r
}
`
		require.Equal(t, 99, runSafeModeFileWithWatchdog(t, source))
	})

	t.Run("sort.Slice blocking comparator", func(t *testing.T) {
		t.Parallel()
		const source = `
package main

import "sort"

func run() int {
	gate := make(chan int)
	stop := make(chan int)
	go func() {
		for {
			select {
			case gate <- 1:
			case <-stop:
				return
			}
		}
	}()
	s := []int{3, 1, 2}
	sort.Slice(s, func(i, j int) bool {
		<-gate
		return s[i] < s[j]
	})
	close(stop)
	return s[0]
}
`
		require.Equal(t, 1, runSafeModeFileWithWatchdog(t, source))
	})

	t.Run("strings.Map blocking mapper", func(t *testing.T) {
		t.Parallel()
		const source = `
package main

import "strings"

func run() string {
	gate := make(chan int)
	stop := make(chan int)
	go func() {
		for {
			select {
			case gate <- 1:
			case <-stop:
				return
			}
		}
	}()
	out := strings.Map(func(r rune) rune {
		<-gate
		return r + 1
	}, "abc")
	close(stop)
	return out
}
`
		require.Equal(t, "bcd", runSafeModeFileWithWatchdog(t, source))
	})
}

func TestGilCallbackStaysSerialised(t *testing.T) {
	t.Parallel()

	const source = `
package main

import "sort"

func run() int {
	shared := make(map[int]int)
	done := make(chan int)
	go func() {
		for k := 0; k < 2000; k++ {
			shared[k % 16] = k
		}
		done <- 1
	}()
	s := []int{9, 3, 7, 1, 8, 2, 6, 4, 5, 0}
	sort.Slice(s, func(i, j int) bool {
		for k := 0; k < 2000; k++ {
			shared[k % 16] = i * j
		}
		return s[i] < s[j]
	})
	<-done
	return len(shared)
}
`
	require.Equal(t, 16, runSafeModeFileWithWatchdog(t, source))
}
