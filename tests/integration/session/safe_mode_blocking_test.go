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

func evalSafeModeFile(t *testing.T, source string) (any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := app.NewService(app.WithSafeMode())
	s.UseSymbolProviders(stdlib.Providers()...)
	return s.EvalFile(ctx, source, "run")
}

func TestSafeModeWaitGroupDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	source := `package main

import "sync"

func run() int {
	var wg sync.WaitGroup
	done := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done <- 1
		}()
	}
	wg.Wait()
	close(done)
	total := 0
	for v := range done {
		total += v
	}
	return total
}`
	result, err := evalSafeModeFile(t, source)
	require.NoError(t, err)
	require.EqualValues(t, 8, result)
}

func TestSafeModeEmbeddedMutexDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	source := `package main

import "sync"

type Guarded struct {
	sync.Mutex
	value int
}

func run() int {
	g := &Guarded{}
	g.Lock()
	done := make(chan int, 1)
	go func() {
		g.Lock()
		g.value += 100
		g.Unlock()
		done <- g.value
	}()
	g.value = 5
	g.Unlock()
	return <-done
}`
	result, err := evalSafeModeFile(t, source)
	require.NoError(t, err)
	require.EqualValues(t, 105, result)
}

func TestSafeModeTimeSleepDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	source := `package main

import "time"

func run() int {
	ch := make(chan int)
	go func() {
		time.Sleep(time.Millisecond)
		ch <- 42
	}()
	time.Sleep(time.Millisecond / 2)
	return <-ch
}`
	result, err := evalSafeModeFile(t, source)
	require.NoError(t, err)
	require.EqualValues(t, 42, result)
}

func TestSafeModeSyncOnceConcurrentCallersAreSerialised(t *testing.T) {
	t.Parallel()
	source := `package main

import "sync"

var initialised int

func run() int {
	var once sync.Once
	ready := make(chan int, 8)
	initFunc := func() {
		accumulator := 0
		for i := 0; i < 200000; i++ {
			accumulator += i
		}
		initialised = accumulator
	}
	for worker := 0; worker < 8; worker++ {
		go func() {
			once.Do(initFunc)
			ready <- initialised
		}()
	}
	agreed := 0
	for worker := 0; worker < 8; worker++ {
		if <-ready == initialised {
			agreed++
		}
	}
	return agreed
}`
	result, err := evalSafeModeFile(t, source)
	require.NoError(t, err)
	require.EqualValues(t, 8, result)
}
