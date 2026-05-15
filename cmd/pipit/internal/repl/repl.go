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

// Package repl is the Bubble Tea Read-Eval-Print Loop for pipit.
package repl

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"pipit.sh/pipit/cmd/pipit/internal/disasm"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/styles"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	// historyDirMode is the permission bits pipit creates the history directory with.
	historyDirMode = 0o750

	// inputCharLimit caps how much text the input textarea will hold.
	inputCharLimit = 1 << 16

	// inputDefaultWidth is the textarea width used before the first window-size message.
	inputDefaultWidth = 40

	// transcriptDefaultWidth is the transcript viewport width used before the first
	// window-size message.
	transcriptDefaultWidth = 80

	// transcriptDefaultHeight is the transcript viewport height used before the first
	// window-size message.
	transcriptDefaultHeight = 20

	// inputMinHeight is the minimum textarea rows.
	inputMinHeight = 1

	// inputMaxHeight is the most rows the textarea may grow to before the transcript takes
	// the remainder.
	inputMaxHeight = 8

	// chromeRows counts non-transcript rows: header (1), 2 transcript borders, 2 input
	// borders, and footer (1).
	chromeRows = 6
)

// viewMode selects which content the transcript viewport displays.
type viewMode int

const (
	// viewSource shows the rolling source transcript: echoed input, captured output, and
	// evaluated results. This is the default mode.
	viewSource viewMode = iota

	// viewBytecode shows the disassembly of every CompiledFunction the session has produced.
	// Input is still Go and runs normally; the view re-renders from session state on each
	// redraw.
	viewBytecode
)

// model holds the REPL's UI and interpreter state. Must not be copied by value.
type model struct {
	// input is the bottom-pane text area where the operator types code.
	input textarea.Model

	// theme supplies the colour and style settings for rendering.
	theme *styles.Theme

	// ctx threads cancellation from the parent context.
	ctx context.Context

	// session evaluates submitted input against the interpreter.
	session *pipit.Session

	// extras registers extra host symbols into the session.
	extras pipit.SymbolExports

	// historyPath is the filesystem path to the persistent history file.
	historyPath string

	// transcriptText accumulates the rendered transcript output.
	transcriptText strings.Builder

	// history holds prior input lines loaded from the history file.
	history []string

	// transcript is the scrollable upper pane showing evaluation results.
	transcript viewport.Model

	// view selects which pane the model is currently rendering.
	view viewMode

	// width is the terminal width in columns.
	width int

	// height is the terminal height in rows.
	height int

	// historyAt is the cursor position within the history slice.
	historyAt int

	// quitting signals tea.Quit on the next Update.
	quitting bool
}

// newModel constructs the initial Bubble Tea model.
//
// Takes session (*pipit.Session) which evaluates submitted input.
// Takes theme (*Theme) which supplies the colour styles.
// Takes extras (pipit.SymbolExports) which registers extra host symbols.
//
// Returns *model which is the initialised REPL model.
func newModel(ctx context.Context, session *pipit.Session, theme *styles.Theme, extras pipit.SymbolExports) *model {
	input := textarea.New()
	input.Placeholder = "type Go code; submit with Enter, multi-line with Alt+Enter, :help for meta commands"
	input.Prompt = "> "
	input.ShowLineNumbers = false
	input.CharLimit = inputCharLimit
	input.MaxHeight = inputMaxHeight
	input.SetHeight(inputMinHeight)
	input.SetWidth(inputDefaultWidth)
	input.Focus()

	transcript := viewport.New(viewport.WithWidth(transcriptDefaultWidth), viewport.WithHeight(transcriptDefaultHeight))

	historyPath := defaultHistoryPath()
	historyEntries := loadHistory(historyPath)

	return &model{
		ctx:            ctx,
		session:        session,
		theme:          theme,
		input:          input,
		transcript:     transcript,
		historyPath:    historyPath,
		history:        historyEntries,
		historyAt:      len(historyEntries),
		extras:         extras,
		transcriptText: strings.Builder{}, view: 0, width: 0, height: 0, quitting: false}
}

// Init runs once on program start.
//
// Returns tea.Cmd which starts the textarea cursor blink.
func (*model) Init() tea.Cmd {
	return textarea.Blink
}

// Update is the Bubble Tea event handler.
//
// Takes message (tea.Msg) which is the event to process.
//
// Returns tea.Model which is the updated model.
// Returns tea.Cmd which batches any follow-up commands.
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 2)

	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.resize(message.Width, message.Height)
		return m, nil
	case tea.KeyPressMsg:
		if cmd, handled := m.handleKeyPress(message); handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	cmds = append(cmds, cmd)
	m.transcript, cmd = m.transcript.Update(message)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// View renders the model.
//
// Returns tea.View which is the composed full-screen frame.
func (m *model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Border)

	header := m.theme.Title.Render("pipit") + " " +
		m.theme.Subtle.Render("("+pipit.Version+", bytecode "+pipit.BytecodeVersion+") - :help")

	transcriptView := border.Render(m.transcript.View())
	inputView := border.Render(m.input.View())
	footer := m.theme.Subtle.Render("Enter to submit · Alt+Enter for multi-line · ↑/↓ history · Ctrl-D / :q to quit")

	view := tea.NewView(strings.Join([]string{header, transcriptView, inputView, footer}, "\n"))
	view.AltScreen = true
	return view
}

// recallHistory moves the history cursor to index and loads that entry into the input.
//
// Index len(history) is the "past the newest entry" slot, which clears the input.
//
// Takes index (int) which is the history slot to move to.
//
// Returns bool which is true when the index was in range and the input was replaced.
func (m *model) recallHistory(index int) bool {
	if index < 0 || index > len(m.history) {
		return false
	}
	m.historyAt = index
	if index == len(m.history) {
		m.input.SetValue("")
	} else {
		m.input.SetValue(m.history[index])
	}
	m.resizeInput()
	return true
}

// handleEnter submits the current input, or extends it to a new line when the Go source
// is not yet balanced.
//
// Returns tea.Cmd which is tea.Quit when the submission ended the session, else nil.
// Returns bool which is always true, since Enter is always consumed.
func (m *model) handleEnter() (tea.Cmd, bool) {
	submission := strings.TrimRight(m.input.Value(), "\n")
	if submission == "" {
		return nil, true
	}
	if !isBalanced(submission) {
		m.input.SetValue(submission + "\n")
		m.resizeInput()
		return nil, true
	}
	m.submit(submission)
	m.input.Reset()
	m.resizeInput()
	if m.quitting {
		return tea.Quit, true
	}
	return nil, true
}

// handleKeyPress runs the REPL's own key bindings.
//
// Keys it does not claim fall through to the textarea and viewport components.
//
// Takes key (tea.KeyPressMsg) which is the key press to handle.
//
// Returns tea.Cmd which is the follow-up command, nil when there is none.
// Returns bool which is true when the key was consumed.
func (m *model) handleKeyPress(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch key.String() {
	case "ctrl+c", "ctrl+d":
		m.appendTranscript(m.theme.Subtle.Render("(exit)"))
		m.quitting = true
		saveHistory(m.historyPath, m.history)
		return tea.Quit, true
	case "enter":
		return m.handleEnter()
	case "alt+enter":
		m.input.SetValue(m.input.Value() + "\n")
		m.resizeInput()
		return nil, true
	case "up":
		return nil, m.recallHistory(m.historyAt - 1)
	case "down":
		return nil, m.recallHistory(m.historyAt + 1)
	}
	return nil, false
}

// refreshViewport recomputes the viewport content from the current view mode and scrolls
// to the bottom.
//
// It is called whenever transcript content changes or the view mode toggles. Bytecode
// mode always re-renders from session state, so newly compiled functions appear without a
// manual refresh.
func (m *model) refreshViewport() {
	switch m.view {
	case viewBytecode:
		m.transcript.SetContent(renderBytecodeView(m.session, m.theme))
	default:
		m.transcript.SetContent(m.transcriptText.String())
	}
	m.transcript.GotoBottom()
}

// appendTranscript adds a line to the rolling source transcript.
//
// In bytecode mode the transcript still grows but is only displayed after a `:code`
// switch.
//
// Takes line (string) which is the pre-rendered transcript line.
func (m *model) appendTranscript(line string) {
	if m.transcriptText.Len() > 0 {
		m.transcriptText.WriteByte('\n')
	}
	m.transcriptText.WriteString(line)
	m.refreshViewport()
}

// handleMeta runs a `:foo argument` meta command.
//
// Takes input (string) which is the raw meta command line.
func (m *model) handleMeta(input string) {
	tokens := strings.Fields(strings.TrimPrefix(input, ":"))
	if len(tokens) == 0 {
		return
	}
	switch tokens[0] {
	case "help":
		m.appendTranscript(helpText())
	case "reset":
		m.session.Reset()
		m.appendTranscript(m.theme.Subtle.Render("(state reset)"))
	case "inspect":
		m.appendTranscript(renderSessionState(m.session.Inspect(), m.theme))
	case "code":
		m.view = viewSource
		m.refreshViewport()
	case "bytecode":
		m.view = viewBytecode
		m.refreshViewport()
	case "exit", "quit", "q":
		m.quitting = true
		saveHistory(m.historyPath, m.history)
	case "load":
		if len(tokens) < 2 {
			m.appendTranscript(m.theme.Error.Render("usage: :load <file>"))
			return
		}
		body, err := os.ReadFile(tokens[1])
		if err != nil {
			m.appendTranscript(m.theme.Error.Render(err.Error()))
			return
		}
		result, err := m.session.Submit(m.ctx, string(body))
		if err != nil {
			m.appendTranscript(m.theme.Error.Render(err.Error()))
			return
		}
		if result != nil {
			m.appendTranscript(fmt.Sprintf("%v", result))
		}
	case "symbols":
		total := len(pipit.StandardLibraryPaths()) + len(m.extras)
		m.appendTranscript(m.theme.Subtle.Render(fmt.Sprintf("%d packages registered", total)))
	default:
		m.appendTranscript(m.theme.Error.Render("unknown meta command: :" + tokens[0]))
	}
}

// submit handles one input submission: meta command or eval.
//
// Takes input (string) which is the submitted source or meta command.
func (m *model) submit(input string) {
	m.history = append(m.history, input)
	m.historyAt = len(m.history)

	echo := m.theme.Prompt.Render("> ") + highlightGo(input)
	m.appendTranscript(echo)

	if strings.HasPrefix(input, ":") {
		m.handleMeta(input)
		return
	}

	captured, result, err := submitCapturing(m.ctx, m.session, input)
	if text := strings.TrimRight(captured, "\n"); text != "" {
		m.appendTranscript(text)
	}
	if err != nil {
		m.appendTranscript(m.theme.Error.Render("error: ") + err.Error())
		return
	}
	if result != nil {
		m.appendTranscript(fmt.Sprintf("%v", result))
	}
}

// resizeInput grows the textarea up to inputMaxHeight to match its content, then gives
// the transcript the remaining vertical space.
func (m *model) resizeInput() {
	if m.height == 0 {
		return
	}
	wanted := max(strings.Count(m.input.Value(), "\n")+1, inputMinHeight)
	wanted = min(wanted, inputMaxHeight)
	m.input.SetHeight(wanted)

	transcriptHeight := max(m.height-chromeRows-(wanted-1), 1)
	m.transcript.SetHeight(transcriptHeight)
	m.transcript.GotoBottom()
}

// resize fits the layout to a new terminal size.
//
// Takes width (int) which is the new terminal width in columns.
// Takes height (int) which is the new terminal height in rows.
func (m *model) resize(width, height int) {
	m.width = width
	m.height = height
	innerWidth := max(width-2, 1)
	m.input.SetWidth(innerWidth)
	m.transcript.SetWidth(innerWidth)
	m.resizeInput()
}

// Run starts the REPL. When stdin is a TTY a Bubble Tea TUI runs; otherwise it falls back
// to a line-buffered batch loop for CI.
//
// Takes streams (output.IO) which supplies the input and output streams.
// Takes extras (pipit.SymbolExports) which registers extra host symbol exports beyond the
// stdlib and Pipit defaults; pass nil for the default symbol set.
// Takes options (...pipit.Option) which configure the interpreter this REPL builds, such
// as the host's logger.
//
// Returns int which is the exit code: 0 on clean exit, 1 on error.
func Run(ctx context.Context, streams output.IO, extras pipit.SymbolExports, options ...pipit.Option) int {
	options = append([]pipit.Option{stdlib.WithStandardLibrary()}, options...)
	var interpreter *pipit.Interpreter
	if len(extras) > 0 {
		interpreter = pipit.NewInterpreterWithSymbols(extras, options...)
	} else {
		interpreter = pipit.NewInterpreter(options...)
	}
	session := interpreter.NewSession()
	if streams.IsStdinTTY() {
		return runTUI(ctx, session, streams, extras)
	}
	return runBatch(ctx, session, streams)
}

// runBatch is the no-TUI fallback: read lines, submit them to the session, print each
// result, and loop. State accumulates across lines like a TTY REPL.
//
// Takes session (*pipit.Session) which evaluates each submitted line.
// Takes streams (output.IO) which supplies the input and output streams.
//
// Returns int which is the exit code: 0 on clean exit, 1 on scan error.
func runBatch(ctx context.Context, session *pipit.Session, streams output.IO) int {
	scanner := bufio.NewScanner(streams.Stdin)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		result, err := session.Submit(ctx, line)
		if err != nil {
			fmt.Fprintf(streams.Stderr, "error: %v\n", err)
			continue
		}
		if result != nil {
			fmt.Fprintln(streams.Stdout, result)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(streams.Stderr, "pipit repl: %v\n", err)
		return 1
	}
	return 0
}

// runTUI launches the Bubble Tea program.
//
// Takes session (*pipit.Session) which evaluates submitted input.
// Takes streams (output.IO) which supplies the input and output streams.
// Takes extras (pipit.SymbolExports) which registers extra host symbols.
//
// Returns int which is the exit code: 0 on clean exit, 1 on error.
func runTUI(ctx context.Context, session *pipit.Session, streams output.IO, extras pipit.SymbolExports) int {
	theme := styles.For(streams.ShouldColour())
	program := tea.NewProgram(
		newModel(ctx, session, theme, extras),
		tea.WithContext(ctx),
		tea.WithInput(streams.Stdin),
		tea.WithOutput(streams.Stdout),
	)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(streams.Stderr, "pipit repl: %v\n", err)
		return 1
	}
	return 0
}

// submitCapturing runs session.Submit while capturing output.
//
// Takes session (*pipit.Session) which the input is submitted against.
// Takes input (string) which is the user source to evaluate.
//
// Returns string which is the combined captured output.
// Returns any which is the evaluation result, or nil.
// Returns error when the submission fails.
//
// Concurrency: not safe for concurrent use; the os.Stdout swap is process-global and a
// copy goroutine drains the pipe for each call.
func submitCapturing(ctx context.Context, session *pipit.Session, input string) (string, any, error) {
	var buffer bytes.Buffer
	session.SetStderr(&buffer)
	defer session.SetStderr(nil)

	originalStdout := os.Stdout
	pipeReader, pipeWriter, pipeErr := os.Pipe()
	if pipeErr != nil {
		result, err := session.Submit(ctx, input)
		return buffer.String(), result, err
	}
	os.Stdout = pipeWriter

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buffer, pipeReader)
		close(done)
	}()

	result, err := session.Submit(ctx, input)

	os.Stdout = originalStdout
	_ = pipeWriter.Close()
	<-done
	_ = pipeReader.Close()

	return buffer.String(), result, err
}

// renderBytecodeView walks the session's accumulated functions and produces a coloured
// disassembly with one section per function.
//
// Synthetic functions (closures, var-init blocks) appear under their internal
// angle-bracket names; user-named functions use the declared identifier.
//
// Takes session (*pipit.Session) which owns the function table.
// Takes theme (*Theme) for colouring section headers.
//
// Returns string which is the multi-line bytecode view content.
func renderBytecodeView(session *pipit.Session, theme *styles.Theme) string {
	funcs := session.CompiledFunctions()
	if len(funcs) == 0 {
		return theme.Subtle.Render("(no compiled functions yet - submit some Go code)")
	}
	var out strings.Builder
	for index, fn := range funcs {
		if index > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(theme.Title.Render(fn.Name))
		out.WriteByte('\n')
		body := disasm.Render(fn, theme)
		if strings.TrimSpace(body) == "" {
			out.WriteString(theme.Subtle.Render("  (no bytecode)"))
		} else {
			out.WriteString(strings.TrimRight(body, "\n"))
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

// helpText returns the body of `:help`.
//
// Returns string which is the multi-line meta command listing.
func helpText() string {
	return strings.Join([]string{
		"meta commands:",
		"  :help              show this help",
		"  :reset             clear session state",
		"  :inspect           show current session declarations and imports",
		"  :code              switch to source transcript view (default)",
		"  :bytecode          switch to compiled bytecode view",
		"  :load <file>       submit a Go source file to the session",
		"  :symbols           summarise registered host packages",
		"  :exit, :quit, :q   quit (Ctrl-D also works)",
	}, "\n")
}

// renderSessionState formats a [pipit.SessionState] snapshot for the `:inspect` meta
// command output.
//
// Takes state (pipit.SessionState) which is the snapshot to render.
// Takes theme (*Theme) for colouring.
//
// Returns string which is the multi-line rendered output.
func renderSessionState(state pipit.SessionState, theme *styles.Theme) string {
	lines := []string{theme.Title.Render("session state")}
	if len(state.Imports) == 0 {
		lines = append(lines, theme.Subtle.Render("  imports: (none)"))
	} else {
		lines = append(lines, theme.Subtle.Render("  imports:"))
		for _, path := range state.Imports {
			lines = append(lines, "    "+path)
		}
	}
	if len(state.Declarations) == 0 {
		lines = append(lines, theme.Subtle.Render("  decls: (none)"))
	} else {
		lines = append(lines, theme.Subtle.Render("  decls:"))
		for _, decl := range state.Declarations {
			lines = append(lines, fmt.Sprintf("    %s %s", decl.Kind, decl.Name))
		}
	}
	lines = append(lines, theme.Subtle.Render(fmt.Sprintf("  submits: %d", state.SubmitCount)))
	return strings.Join(lines, "\n")
}

// isBalanced is a heuristic that decides whether a submission's braces, parens, and
// brackets are closed.
//
// Takes source (string) which is the candidate submission text.
//
// Returns bool which is true when the delimiters appear balanced.
func isBalanced(source string) bool {
	depth := 0
	for _, character := range source {
		switch character {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
			if depth < 0 {
				return true
			}
		}
	}
	return depth == 0
}

// defaultHistoryPath returns the conventional path for REPL history.
//
// Returns string which is the history file path, or "" when no home directory can be
// resolved.
func defaultHistoryPath() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(root, "pipit", "history")
}

// loadHistory reads the persisted history.
//
// Takes path (string) which is the history file to read.
//
// Returns []string which holds the history entries, or nil on any I/O error.
func loadHistory(path string) []string {
	if path == "" {
		return nil
	}
	file, err := os.Open(path) //nolint:gosec // user-owned state path
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	var entries []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		entries = append(entries, scanner.Text())
	}
	return entries
}

// saveHistory persists the latest history to disk. Silent on failure.
//
// Takes path (string) which is the history file to write.
// Takes entries ([]string) which are the history lines to persist.
func saveHistory(path string, entries []string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), historyDirMode); err != nil {
		return
	}
	file, err := os.Create(path) //nolint:gosec // user-owned state path
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		_, _ = writer.WriteString(strings.ReplaceAll(entry, "\n", " ") + "\n")
	}
	_ = writer.Flush()
}
