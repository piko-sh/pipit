package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
)

type Post struct {
	Title, Description, Slug, Date, Story, Summary, Author, Image string
	Series, URL, DateLabel, Year, Byline, Label, Updated, Extra   string
	Related                                                       []string
	Order                                                         int
}

var all = func() []Post {
	out := make([]Post, 0, 20)
	for i := range 20 {
		out = append(out, Post{Title: fmt.Sprint("Title ", i), Slug: fmt.Sprint("p", i),
			Related: []string{fmt.Sprint("p", (i+1)%20), fmt.Sprint("p", (i+3)%20)}, Order: i})
	}
	return out
}()

func page(slug string) string {
	visible := make(map[string]Post, len(all))
	for _, p := range all {
		visible[p.Slug] = p
	}
	self, ok := visible[slug]
	if !ok {
		return "missing"
	}
	var titles []string
	for _, r := range self.Related {
		if p, ok := visible[r]; ok {
			titles = append(titles, p.Title)
		}
	}
	return slug + "=" + strings.Join(titles, ",")
}

type pages struct{}

func (pages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, page(strings.TrimPrefix(r.URL.Path, "/")))
}

func run() string {
	server := httptest.NewServer(pages{})
	defer server.Close()

	get := func(slug string) string {
		response, err := http.Get(server.URL + "/" + slug)
		if err != nil {
			return "error"
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return string(body)
	}

	var bodies []string
	for i := range 30 {
		bodies = append(bodies, get(fmt.Sprint("p", i%20)))
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := get(fmt.Sprint("p", i))
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(bodies)
	return fmt.Sprint(len(bodies), bodies[0], bodies[len(bodies)-1])
}
