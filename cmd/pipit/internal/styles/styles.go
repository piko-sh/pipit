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

// Package styles centralises the lipgloss palette so every command shares a consistent
// visual identity.
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme bundles the styles used across the CLI.
type Theme struct {
	// Title is for command headers and section titles.
	Title lipgloss.Style

	// Subtle is for low-emphasis prose and gutters.
	Subtle lipgloss.Style

	// Strong is for emphasised values.
	Strong lipgloss.Style

	// Success colours positive results.
	Success lipgloss.Style

	// Error colours error messages.
	Error lipgloss.Style

	// Warning colours warnings.
	Warning lipgloss.Style

	// Opcode colours disassembled opcode mnemonics.
	Opcode lipgloss.Style

	// Register colours register references in disassembly.
	Register lipgloss.Style

	// Constant colours constants in disassembly.
	Constant lipgloss.Style

	// SourceLine colours source-line gutter numbers.
	SourceLine lipgloss.Style

	// Prompt is the REPL prompt indicator.
	Prompt lipgloss.Style

	// Border is the colour used for panel borders.
	Border color.Color
}

// Default returns the standard pipit theme.
//
// Returns Theme which carries the coloured lipgloss palette.
func Default() Theme {
	return Theme{
		Title:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		Subtle:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		Strong:     lipgloss.NewStyle().Bold(true),
		Success:    lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		Error:      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		Warning:    lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		Opcode:     lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		Register:   lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		Constant:   lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
		SourceLine: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Prompt:     lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true),
		Border:     lipgloss.Color("241"),
	}
}

// Plain returns a theme with no ANSI styling, for use when colour is disabled or stdout
// is not a TTY.
//
// Returns Theme which carries unstyled no-op styles.
func Plain() Theme {
	noop := lipgloss.NewStyle()
	return Theme{
		Title:      noop,
		Subtle:     noop,
		Strong:     noop,
		Success:    noop,
		Error:      noop,
		Warning:    noop,
		Opcode:     noop,
		Register:   noop,
		Constant:   noop,
		SourceLine: noop,
		Prompt:     noop,
		Border:     lipgloss.NoColor{},
	}
}

// For returns the appropriate theme for the given colour preference.
//
// Takes coloured (bool) which selects the coloured theme when true.
//
// Returns *Theme which is the coloured or plain theme to use.
func For(coloured bool) *Theme {
	if coloured {
		theme := Default()
		return &theme
	}
	theme := Plain()
	return &theme
}
