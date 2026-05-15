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

package app_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

const lruProgram = `package main

type node struct {
	key      int
	value    int
	next     *node
	previous *node
}

type cache struct {
	head     *node
	tail     *node
	capacity int
	size     int
	index    map[int]*node
}

func newCache(capacity int) *cache {
	return &cache{capacity: capacity, index: make(map[int]*node)}
}

func (c *cache) moveToFront(n *node) {
	if c.head == n {
		return
	}
	if n.previous != nil {
		n.previous.next = n.next
	}
	if n.next != nil {
		n.next.previous = n.previous
	}
	if c.tail == n {
		c.tail = n.previous
	}
	n.previous = nil
	n.next = c.head
	if c.head != nil {
		c.head.previous = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *cache) evict() {
	victim := c.tail
	if victim == nil {
		return
	}
	c.tail = victim.previous
	if c.tail != nil {
		c.tail.next = nil
	} else {
		c.head = nil
	}
	victim.previous = nil
	victim.next = nil
	delete(c.index, victim.key)
	c.size--
}

func (c *cache) put(key, value int) {
	if n, ok := c.index[key]; ok {
		n.value = value
		c.moveToFront(n)
		return
	}
	if c.size == c.capacity {
		c.evict()
	}
	n := &node{key: key, value: value}
	c.index[key] = n
	c.size++
	c.moveToFront(n)
}

func (c *cache) get(key int) (int, bool) {
	n, ok := c.index[key]
	if !ok {
		return 0, false
	}
	c.moveToFront(n)
	return n.value, true
}

func entrypoint() int {
	c := newCache(64)
	hits, checksum := 0, 0
	for i := 0; i < 20000; i++ {
		key := (i * 7919) % 200
		if v, ok := c.get(key); ok {
			hits++
			checksum += v
		} else {
			c.put(key, i)
		}
	}
	walk := 0
	for n := c.head; n != nil; n = n.next {
		walk = walk*31 + n.key
	}
	back := 0
	for n := c.tail; n != nil; n = n.previous {
		back = back*31 + n.key
	}
	return hits*1000003 + checksum%1000003 + walk%977 + back%983 + c.size
}

func main() {}
`

func runLRUProgram(t *testing.T, options ...app.Option) string {
	t.Helper()
	service := app.NewService(options...)
	compiled, err := service.CompileProgram(context.Background(), "main", map[string]map[string]string{"": {"main.go": lruProgram}})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "entrypoint")
	require.NoError(t, err)
	return fmt.Sprint(result)
}

func TestPointerFieldStoresAgreeOnBothDispatchers(t *testing.T) {
	t.Parallel()
	goDispatch := runLRUProgram(t, app.WithForceGoDispatch())
	assembly := runLRUProgram(t)
	require.Equal(t, goDispatch, assembly, "the assembly store handler must agree with the Go handler")
	require.NotEqual(t, "0", goDispatch, "the program did real work")
}
