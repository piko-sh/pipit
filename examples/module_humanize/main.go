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

package main

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
)

func main() {
	sizes := []uint64{42, 1<<10 + 5, 1<<20 + 47, 1<<30 + 1, 7<<40 + 12}
	fmt.Println("Bytes (IEC):")
	for _, size := range sizes {
		fmt.Printf("  %14d  ->  %s\n", size, humanize.IBytes(size))
	}

	fmt.Println("\nOrdinals:")
	for _, n := range []int{1, 2, 3, 11, 21, 42, 101} {
		fmt.Printf("  %3d  ->  %s\n", n, humanize.Ordinal(n))
	}

	fmt.Println("\nTime relative to now:")
	now := time.Now()
	for _, offset := range []time.Duration{
		-30 * time.Second,
		-5 * time.Minute,
		-2 * time.Hour,
		-3 * 24 * time.Hour,
		-365 * 24 * time.Hour,
	} {
		when := now.Add(offset)
		fmt.Printf("  %s  (offset %v)\n", humanize.Time(when), offset)
	}

	fmt.Println("\nComma-separated big numbers:")
	for _, value := range []int64{42, 1234, 1_234_567, 1_000_000_000} {
		fmt.Printf("  %14d  ->  %s\n", value, humanize.Comma(value))
	}
}
