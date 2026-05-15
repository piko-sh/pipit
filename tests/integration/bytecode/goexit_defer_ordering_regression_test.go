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
	"fmt"
	"strings"
	"sync"
	"testing"

	"pipit.sh/pipit/internal/app"

	"pipit.sh/pipit/sdk/stdlib"
)

func TestGoexitDeferOrderingRegression(t *testing.T) {
	const source = `package main

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
)

func run() string {
	var wg sync.WaitGroup
	var defersRan []string
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			r := recover()
			mu.Lock()
			defersRan = append(defersRan, fmt.Sprintf("recover=%v", r))
			mu.Unlock()
		}()
		defer func() {
			mu.Lock()
			defersRan = append(defersRan, "inner-defer")
			mu.Unlock()
		}()
		runtime.Goexit()
	}()
	wg.Wait()

	sort.Strings(defersRan)
	result := fmt.Sprintf("count=%d;", len(defersRan))
	for _, d := range defersRan {
		result += d + ";"
	}
	return result
}`
	const want = "count=2;inner-defer;recover=<nil>;"

	iters := 400
	if testing.Short() {
		iters = 50
	}
	const parallel = 8

	var mu sync.Mutex
	var mismatches int
	var wg sync.WaitGroup
	for range parallel {
		wg.Go(func() {
			for range iters {
				service := app.NewService()
				service.UseSymbolProviders(stdlib.Providers()...)
				result, err := service.EvalFile(context.Background(), source, "run")
				got := strings.TrimSpace(fmt.Sprint(result))
				if err != nil || got != want {
					mu.Lock()
					mismatches++
					if mismatches <= 3 {
						t.Errorf("goexit defer ordering broke: got=%q err=%v (want %q)", got, err, want)
					}
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	if mismatches > 0 {
		t.Errorf("total mismatches: %d/%d", mismatches, parallel*iters)
	}
}
