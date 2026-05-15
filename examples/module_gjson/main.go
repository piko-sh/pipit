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

	"github.com/tidwall/gjson"
)

const sample = `{
  "service": "api-gateway",
  "version": "2.4.1",
  "endpoints": [
    {"path": "/users",    "method": "GET",    "auth": true},
    {"path": "/users",    "method": "POST",   "auth": true},
    {"path": "/health",   "method": "GET",    "auth": false},
    {"path": "/metrics",  "method": "GET",    "auth": false}
  ],
  "limits": {
    "max_body_kb": 256,
    "rate_per_min": 600
  }
}`

func main() {
	fmt.Println("== Field accessors ==")
	fmt.Printf("service        = %s\n", gjson.Get(sample, "service").String())
	fmt.Printf("version        = %s\n", gjson.Get(sample, "version").String())
	fmt.Printf("max_body_kb    = %d\n", gjson.Get(sample, "limits.max_body_kb").Int())
	fmt.Printf("rate_per_min   = %d\n", gjson.Get(sample, "limits.rate_per_min").Int())

	fmt.Println("\n== Array indexing ==")
	fmt.Printf("first method   = %s\n", gjson.Get(sample, "endpoints.0.method").String())
	fmt.Printf("first path     = %s\n", gjson.Get(sample, "endpoints.0.path").String())

	fmt.Println("\n== Wildcards via tidwall/match ==")
	fmt.Println("All public (auth=false) endpoints:")
	gjson.Get(sample, `endpoints.#(auth==false)#.path`).ForEach(func(_, value gjson.Result) bool {
		fmt.Printf("  - %s\n", value.String())
		return true
	})

	fmt.Println("\n== Count + total via aggregation ==")
	total := 0
	gjson.Get(sample, "endpoints").ForEach(func(_, _ gjson.Result) bool {
		total++
		return true
	})
	fmt.Printf("endpoint count = %d\n", total)
}
