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

// Package debug_tui hosts the Bubble Tea step-debugger TUI.
package debug_tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/styles"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/sdk/stdlib"
)

// model holds the debugger's UI state.
type model struct {
	// theme supplies the colour and style settings for rendering.
	theme *styles.Theme

	// ctx threads cancellation from the parent context.
	ctx context.Context

	// interpreter runs the script under debug.
	interpreter *pipit.Interpreter

	// debugger controls stepping, breakpoints, and variable inspection.
	debugger *pipit.Debugger

	// panicText describes the panic the program paused on, empty otherwise.
	panicText string

	// scriptPath is the filesystem path of the source file being debugged.
	scriptPath string

	// runStatus describes the script's lifecycle.
	runStatus string

	// commandInput is the bottom-line text being entered for "b <line>".
	commandInput string

	// source holds the script text split into lines for display.
	source []string

	// location is where the current goroutine paused; valid while paused is true.
	location pipit.Location

	// width is the terminal width in columns.
	width int

	// height is the terminal height in rows.
	height int

	// threadID is the paused goroutine's thread id.
	threadID uint64

	// threadCount is the number of live goroutine threads.
	threadCount int

	// paused is true while the program is stopped in the debugger.
	paused bool

	// quitting signals tea.Quit on the next Update.
	quitting bool
}

// newModel builds the Bubble Tea model with its default state.
//
// Takes interpreter (*pipit.Interpreter) which runs the script.
// Takes debugger (*pipit.Debugger) which controls stepping.
// Takes scriptPath (string) which is the path of the source file.
// Takes source (string) which is the raw script text.
// Takes theme (*Theme) which supplies the render styles.
//
// Returns *model which holds the initialised UI state.
func newModel(ctx context.Context, interpreter *pipit.Interpreter, debugger *pipit.Debugger, scriptPath, source string, theme *styles.Theme) *model {
	return &model{
		ctx:         ctx,
		interpreter: interpreter,
		debugger:    debugger,
		scriptPath:  scriptPath,
		source:      strings.Split(source, "\n"),
		theme:       theme,
		runStatus:   "not started - press [r] to run, [b NN] for breakpoint",
		location:    pipit.Location{Function: "", File: "", Line: 0, Column: 0},
		panicText:   "", commandInput: "", width: 0, height: 0, threadID: 0, threadCount: 0, paused: false, quitting: false}
}

// Init returns the startup command for the Bubble Tea runtime.
//
// Returns Cmd which is nil; execution starts on the run key.
func (*model) Init() tea.Cmd {
	return nil
}

// Update handles a Bubble Tea message and advances the model.
//
// Takes message (tea.Msg) which is the event to process.
//
// Returns Model which is the updated model.
// Returns Cmd which is the next command to run, or nil.
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		return m, nil
	case eventMsg:
		return m.applyEvent(message)
	case tea.KeyPressMsg:
		return m.handleKey(message)
	}
	return m, nil
}

// View renders the debugger layout for the current model state.
//
// Returns View which is the composed terminal view.
func (m *model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Border).
		Padding(0, 1)

	left := border.Render(m.renderSource())
	right := border.Render(m.renderStackAndVariables())
	statusBar := m.theme.Subtle.Render(m.statusLine())

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := m.theme.Subtle.Render("r:run · c:continue · n:step · s:step-in · o:step-out · p:pause · b NN:breakpoint · q:quit")
	view := tea.NewView(strings.Join([]string{statusBar, body, footer}, "\n"))
	view.AltScreen = true
	return view
}

// waitForEvent returns a Cmd that blocks until the debugger reports its next event.
//
// Returns Cmd which delivers the event, or the wait error when the context ended.
func (m *model) waitForEvent() tea.Cmd {
	debugger := m.debugger
	ctx := m.ctx
	return func() tea.Msg {
		event, err := debugger.WaitForEvent(ctx)
		return eventMsg{event: event, err: err}
	}
}

// startExecution runs EvalFile in a goroutine and starts listening for events.
//
// Returns Cmd which waits for the first debugger event.
func (m *model) startExecution() tea.Cmd {
	interpreter := m.interpreter
	source := strings.Join(m.source, "\n")
	ctx := m.ctx
	go func() {
		_, _ = interpreter.EvalFile(ctx, source, "main")
	}()
	return m.waitForEvent()
}

// applyEvent updates the model from a debugger event and keeps listening while the
// program runs.
//
// Takes message (eventMsg) which carries the event or the wait error.
//
// Returns Model which is the updated model.
// Returns Cmd which waits for the next event.
func (m *model) applyEvent(message eventMsg) (tea.Model, tea.Cmd) {
	if message.err != nil {
		m.paused = false
		m.runStatus = "stopped: " + message.err.Error()
		return m, nil
	}
	m.threadCount = len(m.debugger.Threads())
	switch message.event.Kind {
	case debug.EventPaused:
		if message.event.Reason == debug.StopReasonOtherThread {
			next := m.waitForEvent()
			return m, next
		}
		m.paused = true
		m.threadID = message.event.ThreadID
		m.location = message.event.Location
		m.panicText = ""
		if message.event.Panic != nil {
			m.panicText = message.event.Panic.Text
		}
		m.runStatus = "paused (" + message.event.Reason.String() + ")"
		if message.event.Message != "" {
			m.runStatus += ": " + message.event.Message
		}
		next := m.waitForEvent()
		return m, next
	case debug.EventExited:
		m.paused = false
		m.runStatus = "finished"
		if message.event.Err != nil {
			m.runStatus = "failed: " + message.event.Err.Error()
		}
		return m, nil
	default:

		next := m.waitForEvent()
		return m, next
	}
}

// handleKey processes a key press and updates the model.
//
// Takes key (tea.KeyPressMsg) which is the key press event.
//
// Returns Model which is the updated model.
// Returns Cmd which is the next command to run, or nil.
func (m *model) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pressed := key.String()
	switch pressed {
	case "q", "ctrl+c":
		m.quitting = true
		m.debugger.Stop()
		return m, tea.Quit
	case "p":
		if err := m.debugger.Pause(); err != nil {
			m.runStatus = err.Error()
		}
		return m, nil
	case "r":
		if m.runStatus == "running" {
			return m, nil
		}
		m.runStatus = "running"
		cmd := m.startExecution()
		return m, cmd
	case "c", "n", "s", "o":
		cmd := m.stepCommand(pressed)
		return m, cmd
	case "b":
		m.commandInput = "b "
		return m, nil
	case "enter":
		m.applyCommandInput()
		return m, nil
	case "esc":
		m.commandInput = ""
		return m, nil
	}
	m.editCommandInput(pressed)
	return m, nil
}

// stepCommand advances the debugger for one of the stepping keys. The event listener
// started with the run reports the next pause.
//
// Takes pressed (string) which is the stepping key: "c", "n", "s", or "o".
//
// Returns tea.Cmd which is nil; events arrive through the running listener.
func (m *model) stepCommand(pressed string) tea.Cmd {
	if !m.paused {
		return nil
	}
	var err error
	status := "stepping"
	switch pressed {
	case "c":
		err = m.debugger.Continue()
		status = "running"
	case "n":
		err = m.debugger.StepOver(m.threadID)
	case "s":
		err = m.debugger.StepIn(m.threadID)
	case "o":
		err = m.debugger.StepOut(m.threadID)
	}
	if err != nil {
		m.runStatus = err.Error()
		return nil
	}
	m.paused = false
	m.runStatus = status
	return nil
}

// applyCommandInput runs the pending "b <line>" command and clears the input line.
func (m *model) applyCommandInput() {
	if after, ok := strings.CutPrefix(m.commandInput, "b "); ok {
		line, err := strconv.Atoi(strings.TrimSpace(after))
		if err == nil {
			m.debugger.SetBreakpoint(m.scriptPath, line)
			m.runStatus = fmt.Sprintf("breakpoint set at %s:%d", m.scriptPath, line)
		}
	}
	m.commandInput = ""
}

// editCommandInput appends to or backspaces the pending command line, ignoring keys that
// arrive while no command is being typed.
//
// Takes pressed (string) which is the key that was pressed.
func (m *model) editCommandInput(pressed string) {
	if m.commandInput == "" {
		return
	}
	if pressed == "backspace" {
		m.commandInput = m.commandInput[:len(m.commandInput)-1]
		return
	}
	if len(pressed) == 1 {
		m.commandInput += pressed
	}
}

// renderSource returns the source view with the current line marked.
//
// Returns string which is the rendered source panel.
func (m *model) renderSource() string {
	currentLine := -1
	if m.paused {
		currentLine = m.location.Line
	}
	var builder strings.Builder
	for index, line := range m.source {
		lineNo := index + 1
		marker := " "
		gutterStyle := m.theme.SourceLine
		if lineNo == currentLine {
			marker = "▶"
			gutterStyle = m.theme.Title
		}
		fmt.Fprintf(&builder, "%s %s  %s\n",
			marker,
			gutterStyle.Render(fmt.Sprintf("%4d", lineNo)),
			line,
		)
	}
	return builder.String()
}

// renderStackAndVariables returns the right-hand panel content.
//
// Returns string which is the rendered stack and variables panel.
func (m *model) renderStackAndVariables() string {
	var builder strings.Builder
	fmt.Fprintln(&builder, m.theme.Title.Render("stack"))
	frames, framesErr := m.pausedStack()
	if framesErr != nil {
		fmt.Fprintln(&builder, m.theme.Subtle.Render("(not paused)"))
	} else {
		for index, frame := range frames {
			fmt.Fprintf(&builder, "  %s%d %s:%d %s\n",
				m.theme.Subtle.Render("#"),
				index,
				frame.File,
				frame.Line,
				m.theme.Strong.Render(frame.Function),
			)
		}
	}
	fmt.Fprintln(&builder)
	fmt.Fprintln(&builder, m.theme.Title.Render("variables"))
	variables, variablesErr := m.pausedLocals()
	if variablesErr != nil {
		fmt.Fprintln(&builder, m.theme.Subtle.Render("(not paused)"))
	} else {
		for _, variable := range variables {
			fmt.Fprintf(&builder, "  %s %s\n",
				m.theme.Strong.Render(variable.Name),
				m.theme.Subtle.Render(fmt.Sprintf("%v", variable.Value)),
			)
		}
	}
	return builder.String()
}

// pausedStack returns the paused goroutine's call stack.
//
// Returns []StackFrame which is the call stack.
// Returns error which is set when nothing is paused.
func (m *model) pausedStack() ([]pipit.StackFrame, error) {
	if !m.paused {
		return nil, pipit.ErrDebugNotPaused
	}
	return m.debugger.StackTrace(m.threadID)
}

// pausedLocals returns the paused goroutine's innermost locals.
//
// Returns []VariableInfo which lists the local variables.
// Returns error which is set when nothing is paused.
func (m *model) pausedLocals() ([]pipit.VariableInfo, error) {
	if !m.paused {
		return nil, pipit.ErrDebugNotPaused
	}
	return m.debugger.Variables(m.threadID, 0, debug.ScopeLocals)
}

// statusLine renders the top status row.
//
// Returns string which is the formatted status line.
func (m *model) statusLine() string {
	status := fmt.Sprintf("pipit debug - %s - %s", m.scriptPath, m.runStatus)
	if m.threadCount > 0 {
		status += fmt.Sprintf(" · goroutine thread %d of %d", m.threadID, m.threadCount)
	}
	if m.panicText != "" {
		status += " · panic: " + m.panicText
	}
	if m.commandInput != "" {
		status += "  [" + m.commandInput + "]"
	}
	return status
}

// eventMsg carries a debugger event back to the model.
type eventMsg struct {
	// err is set when the wait ended without an event.
	err error

	// event is the debugger event.
	event pipit.DebugEvent
}

// Run launches the debug TUI against the given Go source file.
//
// Takes scriptPath (string) which names the Go source file to debug.
// Takes streams (output.IO) which carries the standard IO streams.
// Takes extras (pipit.SymbolExports) which registers extra host symbol exports; pass nil
// for the default symbol set.
// Takes options (...pipit.Option) which configure the interpreter this command builds,
// such as the host's logger.
//
// Returns int which is the exit code; 0 on clean exit, 1 on error.
func Run(ctx context.Context, scriptPath string, streams output.IO, extras pipit.SymbolExports, options ...pipit.Option) int {
	source, err := os.ReadFile(scriptPath) //nolint:gosec // operator-supplied script path
	if err != nil {
		fmt.Fprintf(streams.Stderr, "pipit debug: %v\n", err)
		return 1
	}

	debugger := pipit.NewDebugger()
	allOptions := append([]pipit.Option{stdlib.WithStandardLibrary(), pipit.WithDebugger(debugger)}, options...)
	var interpreter *pipit.Interpreter
	if len(extras) > 0 {
		interpreter = pipit.NewInterpreterWithSymbols(extras, allOptions...)
	} else {
		interpreter = pipit.NewInterpreter(allOptions...)
	}

	theme := styles.For(streams.ShouldColour())
	model := newModel(ctx, interpreter, debugger, scriptPath, string(source), theme)

	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(streams.Stdin),
		tea.WithOutput(streams.Stdout),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(streams.Stderr, "pipit debug: %v\n", err)
		return 1
	}
	return 0
}
