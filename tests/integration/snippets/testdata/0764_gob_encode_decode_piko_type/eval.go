package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

type Book struct {
	Title  string
	Author string
	Pages  int
}

func run() string {
	original := Book{Title: "Go", Author: "Donovan", Pages: 380}
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	if err := enc.Encode(original); err != nil {
		return fmt.Sprintf("encode_err=%v", err)
	}

	var decoded Book
	dec := gob.NewDecoder(&buffer)
	if err := dec.Decode(&decoded); err != nil {
		return fmt.Sprintf("decode_err=%v", err)
	}

	return fmt.Sprintf("title=%s,author=%s,pages=%d",
		decoded.Title, decoded.Author, decoded.Pages)
}
