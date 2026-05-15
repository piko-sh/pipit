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
	"sort"

	"github.com/hashicorp/go-version"
)

func main() {
	raw := []string{
		"1.10.0",
		"1.2.0",
		"1.2.0-rc1",
		"2.0.0-beta",
		"2.0.0",
		"0.99.99",
	}

	parsed := make([]*version.Version, 0, len(raw))
	for _, value := range raw {
		v, err := version.NewVersion(value)
		if err != nil {
			fmt.Printf("skip %q: %v\n", value, err)
			continue
		}
		parsed = append(parsed, v)
	}

	sort.Sort(version.Collection(parsed))
	fmt.Println("Sorted versions:")
	for _, v := range parsed {
		fmt.Printf("  %s\n", v.Original())
	}

	constraint, err := version.NewConstraint(">= 1.5.0, < 2.0.0")
	if err != nil {
		fmt.Println("constraint parse error:", err)
		return
	}
	fmt.Printf("\nConstraint %q matches:\n", constraint)
	for _, v := range parsed {
		mark := "x"
		if constraint.Check(v) {
			mark = "o"
		}
		fmt.Printf("  %s  %s\n", mark, v.Original())
	}
}
