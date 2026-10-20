package main

import (
	"fmt"
	"io"
)

type source struct {
	data   string
	closed bool
}

func (s *source) Read(p []byte) (int, error) {
	if s.data == "" {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}

func (s *source) Close() error {
	s.closed = true
	return nil
}

func run() string {
	original := &source{data: "hello"}
	limited := io.LimitReader(original, 3).(*io.LimitedReader)
	closer, isCloser := limited.R.(io.Closer)
	back, isSource := limited.R.(*source)
	_, isWriter := limited.R.(io.Writer)
	if isCloser {
		closer.Close()
	}
	data, _ := io.ReadAll(limited)
	return fmt.Sprintf("closer=%t source=%t writer=%t same=%t closed=%t data=%s equal=%t",
		isCloser, isSource, isWriter, back == original, original.closed, data, limited.R == io.Reader(original))
}
