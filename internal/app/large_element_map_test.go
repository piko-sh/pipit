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
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLargeElementMapProgramDoesNotHang(t *testing.T) {
	t.Parallel()
	runCancellationChild(t, "TestLargeElementMapProgramDoesNotHangChild")
}

func TestLargeElementMapProgramDoesNotHangChild(t *testing.T) {
	cancellationChild(t, "TestLargeElementMapProgramDoesNotHangChild")
	var options []Option
	if os.Getenv(cancellationDispatchEnv) == "go" {
		options = append(options, WithForceGoDispatch())
	}
	service := NewService(options...)
	result, err := service.EvalFile(t.Context(), largeElementMapProgram, "run")
	require.NoError(t, err)
	require.Equal(t, "200 400", fmt.Sprint(result))
}

const largeElementMapProgram = `package main

type Post struct {
	Title, Description, Slug, Date, Story, Label, Summary, Updated string
	Author, Image, Series, ArticleDate, URL, DateLabel, Year, Byline string
	Tags, Related []string
	Order, Minutes int
	Draft, Retro bool
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func related(slug string, all []Post) []Post {
	visible := make(map[string]Post, len(all))
	for _, p := range all {
		visible[p.Slug] = p
	}
	var out []Post
	for _, target := range visible[slug].Related {
		if post, ok := visible[target]; ok {
			out = append(out, post)
		}
	}
	return out
}

func run() string {
	all := make([]Post, 0, 24)
	for i := range 24 {
		all = append(all, Post{Slug: "p" + itoa(i), Title: "T" + itoa(i),
			Related: []string{"p" + itoa((i+1)%24), "p" + itoa((i+2)%24)}})
	}
	rounds, found := 0, 0
	for round := range 200 {
		found += len(related(all[round%24].Slug, all))
		rounds++
	}
	return itoa(rounds) + " " + itoa(found)
}
`
