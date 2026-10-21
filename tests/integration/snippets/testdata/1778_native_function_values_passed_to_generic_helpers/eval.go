package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
)

type predicate func(rune) bool

func anyMatch[E any](items []E, match func(E) bool) bool {
	for _, item := range items {
		if match(item) {
			return true
		}
	}
	return false
}

func run() string {
	var out strings.Builder
	errs := []error{io.EOF, fs.ErrNotExist, nil}
	fmt.Fprintln(&out, slices.ContainsFunc(errs, os.IsNotExist), slices.IndexFunc(errs, os.IsNotExist))
	fmt.Fprintln(&out, slices.ContainsFunc(errs[:1], os.IsNotExist), anyMatch(errs, os.IsNotExist))
	fmt.Fprintln(&out, len(slices.DeleteFunc(slices.Clone(errs), os.IsNotExist)))

	runes := []rune("abC d")
	fmt.Fprintln(&out, slices.IndexFunc(runes, unicode.IsUpper), slices.ContainsFunc(runes, unicode.IsSpace))
	upper := predicate(unicode.IsUpper)
	fmt.Fprintln(&out, slices.IndexFunc(runes, upper), slices.ContainsFunc(runes, predicate(unicode.IsDigit)))
	isLetter := unicode.IsLetter
	fmt.Fprintln(&out, len(slices.DeleteFunc(slices.Clone(runes), isLetter)))

	words := []string{"pear", "Apple", "fig", "apple"}
	slices.SortFunc(words, strings.Compare)
	fmt.Fprintln(&out, words, slices.IsSortedFunc(words, strings.Compare))
	slices.SortStableFunc(words, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	fmt.Fprintln(&out, words, slices.CompactFunc(slices.Clone(words), strings.EqualFold))
	fmt.Fprintln(&out, slices.MaxFunc(words, strings.Compare), slices.MinFunc(words, strings.Compare))
	index, found := slices.BinarySearchFunc([]string{"a", "c", "e"}, "c", strings.Compare)
	fmt.Fprintln(&out, index, found, slices.EqualFunc(words, words, strings.EqualFold))

	startsWithA := regexp.MustCompile(`^[aA]`).MatchString
	fmt.Fprintln(&out, slices.IndexFunc(words, startsWithA), slices.ContainsFunc(words, regexp.MustCompile(`^z`).MatchString))

	pairs := map[string]string{"a": "A", "b": "c", "Go": "go"}
	maps.DeleteFunc(pairs, strings.EqualFold)
	fmt.Fprintln(&out, pairs, maps.EqualFunc(pairs, map[string]string{"b": "C"}, strings.EqualFold))

	pageSize := sync.OnceValue(os.Getpagesize)
	fmt.Fprintln(&out, pageSize() > 0, pageSize() == os.Getpagesize())
	_, statErr := os.Stat("/definitely/not/here")
	fmt.Fprintln(&out, slices.ContainsFunc([]error{errors.New("x"), statErr}, os.IsNotExist))
	return out.String()
}
