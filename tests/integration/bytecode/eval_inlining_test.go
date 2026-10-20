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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestInlining(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
		want any
	}{
		{
			name: "void no-op call",
			code: `func noop() {}
noop()
42`,
			want: 42,
		},
		{
			name: "small leaf produces correct result",
			code: `func double(x int) int { return x + x }
double(21)`,
			want: 42,
		},
		{
			name: "multiple calls to same function",
			code: `func add(a, b int) int { return a + b }
x := add(1, 2)
y := add(x, 3)
add(y, 4)`,
			want: 10,
		},
		{
			name: "conditional returns",
			code: `func absVal(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
a := absVal(-5)
b := absVal(7)
a + b`,
			want: 12,
		},
		{
			name: "recursive function refused",
			code: `func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
fact(5)`,
			want: 120,
		},
		{
			name: "refused closure still correct",
			code: `func makeAdder(n int) func(int) int {
	return func(x int) int { return x + n }
}
add5 := makeAdder(5)
add5(10)`,
			want: 15,
		},
		{
			name: "multi return same bank",
			code: `func swap(a, b int) (int, int) { return b, a }
x, y := swap(3, 7)
x * 10 + y`,
			want: 73,
		},
		{
			name: "multi return mixed banks",
			code: `func split(x int) (int, string) { return x * 2, "ok" }
n, s := split(21)
n + len(s)`,
			want: 44,
		},
		{
			name: "value return from temp register",
			code: `func f(a int) int {
	x := a + 1
	y := a * 2
	_ = x
	return y
}
f(5)`,
			want: 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := app.NewService().Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

const (
	lruSourceForInliner = `type lruNode struct {
	key      int64
	value    int64
	previous *lruNode
	next     *lruNode
}
type lruCache struct {
	capacity int
	size     int
	lookup   map[int64]*lruNode
	head     *lruNode
	tail     *lruNode
}
func newCache(capacity int) *lruCache {
	return &lruCache{capacity: capacity, lookup: map[int64]*lruNode{}}
}
func (c *lruCache) detach(node *lruNode) {
	if node.previous != nil {
		node.previous.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.previous = node.previous
	} else {
		c.tail = node.previous
	}
}
func (c *lruCache) attachAtFront(node *lruNode) {
	node.previous = nil
	node.next = c.head
	if c.head != nil {
		c.head.previous = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}
func (c *lruCache) moveToFront(node *lruNode) {
	if node == c.head {
		return
	}
	c.detach(node)
	c.attachAtFront(node)
}
func (c *lruCache) put(key, value int64) {
	if existing, found := c.lookup[key]; found {
		existing.value = value
		c.moveToFront(existing)
		return
	}
	node := &lruNode{key: key, value: value}
	c.lookup[key] = node
	c.attachAtFront(node)
	c.size++
}
c := newCache(8)
c.put(1, 100)
c.put(2, 200)
c.put(3, 300)
c.size`
)

func TestInlining_LRUDiagnostic(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(t.Context(), lruSourceForInliner)
	require.NoError(t, err)
	require.Equal(t, 3, result, "the cache holds three entries after three puts")
}
