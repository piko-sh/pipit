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
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"
)

func TestInlining_VoidNoop_NoOpCall(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func noop() {}
noop()
42`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 42, result)
	})
}

func TestInlining_SmallLeaf_ProducesCorrectResult(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func double(x int) int { return x + x }
double(21)`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 42, result)
	})
}

func TestInlining_MultipleCallsToSameFunction(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func add(a, b int) int { return a + b }
x := add(1, 2)
y := add(x, 3)
add(y, 4)`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 10, result)
	})
}

func TestInlining_ConditionalReturns(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func absVal(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
a := absVal(-5)
b := absVal(7)
a + b`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 12, result)
	})
}

func TestInlining_RecursiveFunctionRefused(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
fact(5)`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 120, result)
	})
}

func TestInlining_ClosureRefused_StillCorrect(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func makeAdder(n int) func(int) int {
	return func(x int) int { return x + n }
}
add5 := makeAdder(5)
add5(10)`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 15, result)
	})
}

func TestInlining_MultiReturn_SameBank(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func swap(a, b int) (int, int) { return b, a }
x, y := swap(3, 7)
x * 10 + y`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 73, result)
	})
}

func TestInlining_MultiReturn_MixedBanks(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()
		code := `func split(x int) (int, string) { return x * 2, "ok" }
n, s := split(21)
n + len(s)`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 44, result)
	})
}

func TestInlining_ValueReturnFromTempRegister(t *testing.T) {
	withInlineEnabled(t, func() {
		service := app.NewService()

		code := `func f(a int) int {
	x := a + 1
	y := a * 2
	_ = x
	return y
}
f(5)`
		result, err := service.Eval(context.Background(), code)
		require.NoError(t, err)
		require.Equal(t, 10, result)
	})
}

func withInlineEnabled(t *testing.T, fn func()) {
	t.Helper()
	fn()
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
	withInlineEnabled(t, func() {
		service := app.NewService()
		result, err := service.Eval(context.Background(), lruSourceForInliner)
		require.NoError(t, err)
		require.Equal(t, 3, result, "result should be size after 3 puts")
	})
}
