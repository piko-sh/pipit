package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"

	"testpkg/render"
)

type site struct{}

func (site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	page, ok := render.Page(strings.TrimPrefix(r.URL.Path, "/thoughts/"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "missing")
		return
	}
	io.WriteString(w, page)
}

func digest(body string) string {
	hash := fnv.New32a()
	io.WriteString(hash, body)
	return fmt.Sprintf("%08x", hash.Sum32())
}

func entrypoint() string {
	server := httptest.NewServer(site{})
	defer server.Close()

	get := func(slug string) string {
		response, err := http.Get(server.URL + "/thoughts/" + slug)
		if err != nil {
			return "error"
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return fmt.Sprint(response.StatusCode, " ", digest(string(body)))
	}

	var mu sync.Mutex
	counts := map[string]int{}
	record := func(result string) {
		mu.Lock()
		counts[result]++
		mu.Unlock()
	}
	for i := range 30 {
		record(get(fmt.Sprintf("post-%02d", i%15)))
	}
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 30 {
				record(get(fmt.Sprintf("post-%02d", (worker+i)%15)))
			}
		}()
	}
	wg.Wait()

	sample, _ := render.Page("post-03")
	results := make([]string, 0, len(counts))
	total := 0
	for result, count := range counts {
		results = append(results, fmt.Sprint(result, "x", count))
		total += count
	}
	sort.Strings(results)
	combined := fnv.New32a()
	io.WriteString(combined, strings.Join(results, ";"))
	return fmt.Sprintf("requests=%d distinct=%d combined=%08x sample=%s", total, len(results), combined.Sum32(), sample)
}

func main() {
	fmt.Println(entrypoint())
}
