package main

import (
	"fmt"
	"io"
)

type Pipe struct {
	buffer []byte
	closed bool
}

func (p *Pipe) Read(out []byte) (int, error) {
	if p.closed && len(p.buffer) == 0 {
		return 0, io.EOF
	}
	n := copy(out, p.buffer)
	p.buffer = p.buffer[n:]
	return n, nil
}

func (p *Pipe) Write(in []byte) (int, error) {
	if p.closed {
		return 0, fmt.Errorf("closed")
	}
	p.buffer = append(p.buffer, in...)
	return len(in), nil
}

func (p *Pipe) Close() error {
	p.closed = true
	return nil
}

func run() string {
	pipe := &Pipe{}
	var rwc io.ReadWriteCloser = pipe
	_, _ = rwc.Write([]byte("hello"))
	_, _ = rwc.Write([]byte(" world"))
	_ = rwc.Close()
	buffer := make([]byte, 32)
	n, _ := rwc.Read(buffer)
	return fmt.Sprintf("read=%s,closed=%t", string(buffer[:n]), pipe.closed)
}
