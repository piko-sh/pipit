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

//go:build integration

package golden_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	updateEnv          = "PIPIT_GOLDEN_UPDATE"
	goldenSourceName   = "main.go"
	snippetsDir        = "../snippets/testdata"
	snippetFileName    = "eval.go"
	compileErrorMarker = "compile-error"
	testdataDir        = "testdata"
	filePerm           = 0o644
	dirPerm            = 0o755
)

type corpusCase struct {
	name     string
	source   string
	assembly string
	keys     map[string]struct{}
	err      error
}

var (
	corpusOnce  sync.Once
	corpusCases []corpusCase
)

func updateMode() bool {
	return os.Getenv(updateEnv) != ""
}

func newGoldenService(opts ...app.Option) *app.Service {
	service := app.NewService(opts...)
	service.UseSymbolProviders(stdlib.Providers()...)
	return service
}

func compileGolden(ctx context.Context, service *app.Service, source string) (*program.CompiledFileSet, error) {
	return service.CompileFileSet(ctx, map[string]string{goldenSourceName: source})
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	corpusOnce.Do(func() {
		corpusCases = compileCorpus(t)
	})
	return corpusCases
}

func compileCorpus(t *testing.T) []corpusCase {
	t.Helper()

	entries, err := os.ReadDir(snippetsDir)
	require.NoError(t, err, "reading parity corpus")

	cases := make([]corpusCase, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(snippetsDir, entry.Name(), snippetFileName))
		require.NoError(t, readErr, "reading snippet %s", entry.Name())
		cases = append(cases, corpusCase{name: entry.Name(), source: string(data)})
	}

	workers := max(runtime.GOMAXPROCS(0), 1)
	indices := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for index := range indices {
				cases[index].compile(newGoldenService())
			}
		})
	}
	for index := range cases {
		indices <- index
	}
	close(indices)
	wg.Wait()

	return cases
}

func (c *corpusCase) compile(service *app.Service) {
	cfs, err := compileGolden(context.Background(), service, c.source)
	if err != nil {
		c.err = err
		c.keys = map[string]struct{}{}
		return
	}
	c.assembly = debug.DisassembleAssembly(cfs)
	c.keys = collectInstructionKeys(cfs)
}

func (c *corpusCase) hash() string {
	if c.err != nil {
		return compileErrorMarker
	}
	sum := sha256.Sum256([]byte(c.assembly))
	return hex.EncodeToString(sum[:])
}

func collectInstructionKeys(cfs *program.CompiledFileSet) map[string]struct{} {
	keys := make(map[string]struct{})
	var walk func(compiledFunction *program.CompiledFunction)
	walk = func(compiledFunction *program.CompiledFunction) {
		if compiledFunction == nil {
			return
		}
		for _, instr := range compiledFunction.Body {
			keys[isa.InstructionDisplayName(instr)] = struct{}{}
		}
		for _, child := range compiledFunction.Functions {
			walk(child)
		}
	}
	walk(cfs.Root())
	walk(cfs.VariableInitFunction())
	return keys
}

func readListFile(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err, "reading %s", path)

	var names []string
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		names = append(names, trimmed)
	}
	return names
}

func writeListFile(t *testing.T, path, header string, names []string) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString(header)
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(path, []byte(builder.String()), filePerm))
}
