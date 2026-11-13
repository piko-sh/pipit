package main

import (
	"fmt"
	"strings"
)

func hasDigit(text *string) bool {
	for _, r := range *text {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func checked(text *string) bool {
	if text == nil {
		return false
	}
	return hasDigit(text)
}

func split(text string) (string, int) {
	head, tail, _ := strings.Cut(text, "=")
	return head, len(tail)
}

func either(text *string, fallback bool) (string, int) {
	if fallback {
		return split("fallback=" + *text)
	}
	return split(*text)
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return n
}

func classify(n int) int {
	if n > 100 {
		return sign(n - 100)
	}
	return sign(n) * 2
}

type collector struct {
	answers []string
}

func (c *collector) note(text *string, fallback bool) {
	label := strings.ToUpper(*text)
	head, size := either(text, fallback)
	if checked(text) {
		c.answers = append(c.answers, fmt.Sprintf("%s:yes:%s:%d", label, head, size))
		return
	}
	c.answers = append(c.answers, fmt.Sprintf("%s:no:%s:%d", label, head, size))
}

func run() string {
	c := &collector{}
	for index, word := range []string{"a1=x", "bb", "c3=yy", "dd=", "e5"} {
		c.note(&word, index%2 == 1)
	}
	numbers := []string{}
	for _, n := range []int{-5, 0, 7, 150, 99, 101} {
		numbers = append(numbers, fmt.Sprint(classify(n)))
	}
	return strings.Join(c.answers, " ") + "\n" + strings.Join(numbers, " ")
}
