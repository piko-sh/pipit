package main

import (
	"fmt"
	"strings"
)

type Post struct {
	Title, Description, Slug, Date, Story, Label, Summary, Updated string
	Author, Image, Series, ArticleDate, URL, DateLabel, Year, Byline string
	Tags, Related                                                    []string
	Order, Minutes                                                   int
	Draft, Retro                                                     bool
}

func posts() []Post {
	out := make([]Post, 0, 24)
	for i := range 24 {
		slug := fmt.Sprintf("post-%02d", i)
		out = append(out, Post{
			Title: "Title " + slug, Slug: slug, URL: "/thoughts/" + slug,
			Related: []string{fmt.Sprintf("post-%02d", (i+1)%24), fmt.Sprintf("post-%02d", (i+5)%24)},
			Order:   i, Draft: i%4 == 0,
		})
	}
	return out
}

func related(slug string, all []Post) []Post {
	visible := make(map[string]Post, len(all))
	for _, p := range all {
		if !p.Draft {
			visible[p.Slug] = p
		}
	}
	var targets []string
	for _, p := range all {
		if p.Slug == slug {
			targets = p.Related
		}
	}
	var out []Post
	for _, target := range targets {
		if post, ok := visible[target]; ok {
			out = append(out, post)
		}
	}
	return out
}

func run() string {
	all := posts()
	var lines []string
	for round := range 3 {
		for _, p := range all {
			var titles []string
			for _, r := range related(p.Slug, all) {
				titles = append(titles, r.Title)
			}
			if round == 0 {
				lines = append(lines, p.Slug+":"+strings.Join(titles, ","))
			}
		}
	}
	return strings.Join(lines, ";")
}
