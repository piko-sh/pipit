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

	"github.com/spf13/cast"
)

func main() {
	fmt.Println("== string -> int / float / bool ==")
	fmt.Printf("  ToInt(\"42\")        = %d\n", cast.ToInt("42"))
	fmt.Printf("  ToFloat64(\"3.14\")  = %v\n", cast.ToFloat64("3.14"))
	fmt.Printf("  ToBool(\"true\")     = %v\n", cast.ToBool("true"))
	fmt.Printf("  ToBool(\"yes\")      = %v\n", cast.ToBool("yes"))
	fmt.Printf("  ToBool(\"0\")        = %v\n", cast.ToBool("0"))

	fmt.Println("\n== int -> string / bool ==")
	fmt.Printf("  ToString(42)       = %q\n", cast.ToString(42))
	fmt.Printf("  ToString(3.14)     = %q\n", cast.ToString(3.14))
	fmt.Printf("  ToBool(1)          = %v\n", cast.ToBool(1))
	fmt.Printf("  ToBool(0)          = %v\n", cast.ToBool(0))

	fmt.Println("\n== heterogeneous slices ==")
	mixed := []any{"1", 2, 3.0, "four"}
	ints := cast.ToIntSlice(mixed[:3])
	strs := cast.ToStringSlice(mixed)
	fmt.Printf("  ToIntSlice(%v)    = %v\n", mixed[:3], ints)
	fmt.Printf("  ToStringSlice(%v) = %v\n", mixed, strs)

	fmt.Println("\n== map[any]any -> map[string]any ==")
	raw := map[any]any{
		"host":  "localhost",
		"port":  8080,
		"debug": true,
	}
	config := cast.ToStringMap(raw)
	fmt.Printf("  host=%v port=%v debug=%v\n", config["host"], config["port"], config["debug"])

	fmt.Println("\n== time / duration ==")
	when, err := cast.ToTimeE("2024-01-15T09:30:00Z")
	if err != nil {
		fmt.Printf("  ToTimeE error: %v\n", err)
	} else {
		fmt.Printf("  ToTime(\"2024-01-15T09:30:00Z\") = %s\n", when.Format(time.RFC3339))
	}
	fmt.Printf("  ToDuration(\"500ms\") = %v\n", cast.ToDuration("500ms"))
}
