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

package extract

import (
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"
)

// IndexFileName is the file the import-path index is written to.
const IndexFileName = "gen_paths.go"

// GenerateIndexFile calls Generator.GenerateIndexFile with the default tool name.
//
// Takes paths ([]string) which are the import paths to list.
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func GenerateIndexFile(paths []string, outputPackage string) ([]byte, error) {
	return defaultGenerator.GenerateIndexFile(paths, outputPackage)
}

// GenerateIndexFile produces the Go source listing the extracted import paths and nothing
// else. It carries no reflect.Value, so a caller that only needs to know which packages
// exist can link it without the symbol tables.
//
// Takes paths ([]string) which are the import paths to list.
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func (g Generator) GenerateIndexFile(paths []string, outputPackage string) ([]byte, error) {
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	var source strings.Builder
	source.WriteString(g.marker() + "\n\n")
	source.WriteString("package " + outputPackage + "\n\n")
	source.WriteString("// ImportPaths lists every standard-library package the symbol tables register,\n")
	source.WriteString("// sorted by import path.\n")
	source.WriteString("var ImportPaths = []string{\n")
	for _, path := range sorted {
		source.WriteString("\t" + strconv.Quote(path) + ",\n")
	}
	source.WriteString("}\n")

	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", IndexFileName, err)
	}
	return formatted, nil
}
