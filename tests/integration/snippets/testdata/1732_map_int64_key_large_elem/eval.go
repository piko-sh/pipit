package main

import "fmt"

type wide [17]int64

type Post struct {
	Title, Slug, URL, Summary, Image, Series string
	Order                                    int
	Tags                                     []string
}

func run() string {
	byInt64 := map[int64]wide{}
	byInt := map[int]Post{}
	byPtr := map[int64]*Post{}
	for i := range 50 {
		var w wide
		w[0], w[16] = int64(i), int64(i*3)
		byInt64[int64(i)] = w
		p := Post{Title: fmt.Sprint("t", i), Slug: fmt.Sprint("s", i), Order: i}
		byInt[i] = p
		byPtr[int64(i)] = &p
	}
	var sum int64
	titles := 0
	for i := range 60 {
		if w, ok := byInt64[int64(i)]; ok {
			sum += w[0] + w[16]
		}
		if p, ok := byInt[i]; ok && p.Title != "" {
			titles++
		}
		if p, ok := byPtr[int64(i)]; ok {
			sum += int64(p.Order)
		}
	}
	return fmt.Sprint(len(byInt64), len(byInt), len(byPtr), sum, titles)
}
