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

package compile

import (
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerPositionString_UnknownAndValid(t *testing.T) {
	t.Parallel()

	c := &Compiler{programContext: &programContext{}}
	require.Equal(t, "<unknown>", c.positionString(token.NoPos))
	require.Equal(t, "<unknown>", c.positionString(token.Pos(42)),
		"non-zero pos with nil fileSet should still return unknown")

	fileSet := token.NewFileSet()
	file := fileSet.AddFile("test.go", -1, 100)
	file.AddLine(0)
	file.AddLine(20)
	c2 := &Compiler{programContext: &programContext{FileSet: fileSet}}

	require.Contains(t, c2.positionString(file.Pos(25)), "test.go",
		"valid position should include the file name")
}
