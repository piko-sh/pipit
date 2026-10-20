package main

import "fmt"

type Slug string

type Small struct {
	Title string
	Order int
}

type Large struct {
	Title, Description, Summary, Image, Series, URL, Byline, Label, Extra string
	Order                                                                 int
}

func run() string {
	small := map[Slug]Small{}
	large := map[Slug]Large{}
	for i := range 20 {
		key := Slug(fmt.Sprint("s", i))
		small[key] = Small{Title: fmt.Sprint("t", i), Order: i}
		large[key] = Large{Title: fmt.Sprint("T", i), Extra: fmt.Sprint("x", i), Order: i}
	}

	var out []string

	out = append(out, small["s3"].Title, large["s4"].Extra)

	s, okSmall := small["s5"]
	l, okLarge := large["s6"]
	out = append(out, fmt.Sprint(s.Order, okSmall, l.Order, okLarge))

	total := 0
	for i := range 25 {
		key := Slug(fmt.Sprint("s", i))
		if v, ok := small[key]; ok {
			total += v.Order
		}
		if v, ok := large[key]; ok {
			total += v.Order * 100
		}
	}
	out = append(out, fmt.Sprint(total))

	missingSmall, okMissingSmall := small["absent"]
	missingLarge, okMissingLarge := large["absent"]
	out = append(out, fmt.Sprintf("%q %v %q %v", missingSmall.Title, okMissingSmall, missingLarge.Extra, okMissingLarge))
	out = append(out, fmt.Sprint(small["absent"].Order, large["absent"].Order))

	return fmt.Sprint(out)
}
