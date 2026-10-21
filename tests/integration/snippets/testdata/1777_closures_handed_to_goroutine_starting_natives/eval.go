package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type box struct {
	mu    sync.Mutex
	count *int
	names []string
}

func (b *box) add(name string) {
	b.mu.Lock()
	next := *b.count + 1
	b.count = &next
	b.names = append(b.names, name)
	b.mu.Unlock()
}

func run() string {
	b := &box{count: new(int)}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for step := range 50 {
				b.add(fmt.Sprint(worker, "-", step))
			}
		})
	}
	add := b.add
	for range 4 {
		wg.Go(func() { add("method") })
	}
	wg.Wait()
	names := append([]string(nil), b.names...)
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return fmt.Sprint(*b.count, " ", len(names), " ", names[0], " ", names[len(names)-1], " ", strings.Count(strings.Join(names, ","), "method"))
}
