package main

import "fmt"

type Slug string

type Post struct {
	Title, Description, Summary, Image, Series, URL, Byline, Label, Extra string
	Order                                                                 int
}

func run() string {
	m := map[Slug]Post{}
	for i := range 40 {
		m[Slug(fmt.Sprint("s", i))] = Post{Title: fmt.Sprint("T", i), Order: i}
	}
	total := 0
	for i := range 45 {
		if p, ok := m[Slug(fmt.Sprint("s", i))]; ok {
			total += p.Order
		}
	}
	p := m["s7"]
	return fmt.Sprint(len(m), total, p.Title)
}
