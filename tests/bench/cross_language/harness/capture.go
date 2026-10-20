// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build crosslang

package harness

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// processCaptureMu serialises the in-process runners' swaps of the process-wide standard
// streams, os.Args, flag.CommandLine and working directory.
var processCaptureMu sync.Mutex

// interpretedRun is what one in-process interpreter run produced.
type interpretedRun struct {
	// stdout is what the program wrote to standard output.
	stdout []byte

	// stderr is what the program wrote to standard error.
	stderr []byte

	// compileNanos is the compilation time.
	compileNanos int64

	// wallNanos is the execution time.
	wallNanos int64
}

// processCapture is a redirection of the process's standard streams and arguments for an
// in-process interpreter that reads them; finish undoes it.
type processCapture struct {
	// savedFlags is flag.CommandLine before the swap.
	savedFlags *flag.FlagSet

	// savedStdout is os.Stdout before the swap.
	savedStdout *os.File

	// savedStderr is os.Stderr before the swap.
	savedStderr *os.File

	// stdoutWriter is the pipe end os.Stdout points at.
	stdoutWriter *os.File

	// stderrWriter is the pipe end os.Stderr points at.
	stderrWriter *os.File

	// stdoutDone receives everything read from the stdout pipe.
	stdoutDone chan []byte

	// stderrDone receives everything read from the stderr pipe.
	stderrDone chan []byte

	// savedDirectory is the working directory to return to, empty when it was not changed.
	savedDirectory string

	// savedArgs is os.Args before the swap.
	savedArgs []string
}

// startProcessCapture locks processCaptureMu, points os.Stdout and os.Stderr at pipes
// that are read in the background, and swaps in argv and a fresh flag.CommandLine. When
// directory is not empty it also changes into it. The caller must call finish.
//
// Takes argv ([]string) which becomes os.Args.
// Takes directory (string) which is the working directory for the run, or empty.
//
// Returns *processCapture which undoes the redirection.
// Returns error when a pipe cannot be created; nothing is changed then.
func startProcessCapture(argv []string, directory string) (*processCapture, error) {
	processCaptureMu.Lock()
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		processCaptureMu.Unlock()
		return nil, fmt.Errorf("pipe stdout: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutWriter.Close()
		_ = stdoutReader.Close()
		processCaptureMu.Unlock()
		return nil, fmt.Errorf("pipe stderr: %w", err)
	}
	capture := &processCapture{
		savedArgs:      os.Args,
		savedFlags:     flag.CommandLine,
		savedStdout:    os.Stdout,
		savedStderr:    os.Stderr,
		stdoutWriter:   stdoutWriter,
		stderrWriter:   stderrWriter,
		stdoutDone:     make(chan []byte, 1),
		stderrDone:     make(chan []byte, 1),
		savedDirectory: "",
	}
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	os.Args = argv
	flag.CommandLine = flag.NewFlagSet(argv[0], flag.ContinueOnError)
	if directory != "" {
		capture.changeDirectory(directory)
	}
	go drainStream(stdoutReader, capture.stdoutDone)
	go drainStream(stderrReader, capture.stderrDone)
	return capture, nil
}

// changeDirectory changes into directory and remembers where to return to. A failure
// leaves the working directory as it was.
//
// Takes directory (string) which is the directory to change into.
func (capture *processCapture) changeDirectory(directory string) {
	saved, err := os.Getwd()
	if err != nil {
		return
	}
	absolute, err := filepath.Abs(directory)
	if err != nil || os.Chdir(absolute) != nil {
		return
	}
	capture.savedDirectory = saved
}

// finish closes the pipes, restores everything startProcessCapture changed and unlocks
// processCaptureMu.
//
// Returns stdout ([]byte) which is what the run wrote to standard output.
// Returns stderr ([]byte) which is what it wrote to standard error.
func (capture *processCapture) finish() (stdout []byte, stderr []byte) {
	_ = capture.stdoutWriter.Close()
	_ = capture.stderrWriter.Close()
	os.Stdout, os.Stderr = capture.savedStdout, capture.savedStderr
	os.Args = capture.savedArgs
	flag.CommandLine = capture.savedFlags
	if capture.savedDirectory != "" {
		_ = os.Chdir(capture.savedDirectory)
	}
	stdout = <-capture.stdoutDone
	stderr = <-capture.stderrDone
	processCaptureMu.Unlock()
	return stdout, stderr
}

// drainStream reads reader to its end, closes it and sends what it read. A read error
// keeps what was read before it.
//
// Takes reader (*os.File) which is the pipe's read end.
// Takes done (chan<- []byte) which receives the bytes.
func drainStream(reader *os.File, done chan<- []byte) {
	data, _ := io.ReadAll(reader)
	_ = reader.Close()
	done <- data
}

// runRecovering calls run and turns a panic into an error that names the interpreter.
//
// Takes name (string) which names the interpreter in the error.
// Takes run (func() error) which runs the program.
//
// Returns error which is run's error, or the recovered panic.
func runRecovering(name string, run func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%s VM panic: %v", name, recovered)
		}
	}()
	return run()
}
