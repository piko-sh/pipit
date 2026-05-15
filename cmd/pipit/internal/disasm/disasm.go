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

// Package disasm renders a CompiledFunction's disassembly as styled terminal output.
package disasm

import (
	"strings"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/styles"
)

// Render produces a coloured disassembly of compiledFunction under the given theme.
//
// Takes compiledFunction (*pipit.CompiledFunction) which supplies the disassembly text.
// Takes theme (*Theme) which provides the colour styles for each token.
//
// Returns string which is the tinted disassembly, terminated by a newline.
func Render(compiledFunction *pipit.CompiledFunction, theme *styles.Theme) string {
	raw := compiledFunction.Disassemble()
	if raw == "" {
		return ""
	}

	var output strings.Builder
	for line := range strings.SplitSeq(raw, "\n") {
		output.WriteString(renderLine(line, theme))
		output.WriteByte('\n')
	}
	return strings.TrimRight(output.String(), "\n") + "\n"
}

// renderLine tints a single disassembly line.
//
// Takes line (string) which is one raw disassembly line.
// Takes theme (*Theme) which provides the colour styles for each token.
//
// Returns string which is the tinted line, or the input unchanged when empty.
func renderLine(line string, theme *styles.Theme) string {
	if line == "" {
		return ""
	}
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return line
	}

	var output strings.Builder

	for index, token := range tokens {
		if index > 0 {
			output.WriteByte(' ')
		}
		switch {
		case isInstructionIndex(token):
			output.WriteString(theme.SourceLine.Render(token))
		case isRegister(token):
			output.WriteString(theme.Register.Render(token))
		case isConstantPrefix(token):
			output.WriteString(theme.Constant.Render(token))
		case isOpcode(token, index, tokens):
			output.WriteString(theme.Opcode.Render(token))
		case strings.HasPrefix(token, ";"):
			output.WriteString(theme.SourceLine.Render(strings.Join(tokens[index:], " ")))
			return output.String()
		default:
			output.WriteString(token)
		}
	}
	return output.String()
}

// isInstructionIndex matches the leading "NNN:" instruction-index gutter.
//
// Takes token (string) which is a single whitespace-separated token.
//
// Returns bool which reports whether the token is an instruction index.
func isInstructionIndex(token string) bool {
	if !strings.HasSuffix(token, ":") {
		return false
	}
	stripped := strings.TrimSuffix(token, ":")
	if stripped == "" {
		return false
	}
	for _, character := range stripped {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// isRegister matches register names like "R0" or "R12".
//
// Takes token (string) which is a single whitespace-separated token.
//
// Returns bool which reports whether the token names a register.
func isRegister(token string) bool {
	cleaned := strings.TrimSuffix(strings.TrimSuffix(token, ","), ":")
	if len(cleaned) < 2 || (cleaned[0] != 'R' && cleaned[0] != 'r') {
		return false
	}
	for _, character := range cleaned[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// isConstantPrefix matches "K<n>" or "C<n>" constant-pool references.
//
// Takes token (string) which is a single whitespace-separated token.
//
// Returns bool which reports whether the token is a constant reference.
func isConstantPrefix(token string) bool {
	cleaned := strings.TrimSuffix(token, ",")
	if len(cleaned) < 2 {
		return false
	}
	if cleaned[0] != 'K' && cleaned[0] != 'C' {
		return false
	}
	for _, character := range cleaned[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// isOpcode treats the first non-gutter token as a mnemonic.
//
// Takes token (string) which is the token under inspection.
// Takes index (int) which is the token's position on the line.
// Takes tokens ([]string) which are all tokens on the line.
//
// Returns bool which reports whether the token is an opcode mnemonic.
func isOpcode(token string, index int, tokens []string) bool {
	if token == "" {
		return false
	}
	if index == 0 {
		return false
	}
	if index == 1 && isInstructionIndex(tokens[0]) {
		return !strings.ContainsAny(token, " ,;")
	}
	return false
}
