// Package tui — embedded PTY terminal page.
//
// term.go runs a real shell inside the bubbletea program: a creack/pty
// pair gives us a controlling tty for the child, hinshun/vt10x parses
// the byte stream and maintains a screen buffer, and View() renders
// that buffer with lipgloss using the active cloud's theme. Keystrokes
// are translated to the byte sequences a real terminal would send and
// written to the PTY master.
//
// The page is opened by the `x` keybinding and pre-loaded with the
// row's cloud context as env vars (CLOUDNAV_SUB / AZURE_SUBSCRIPTION_ID
// for Azure, CLOUDPROJECT for GCP, AWS_PROFILE / AWS_ACCOUNT_ID for AWS,
// CLOUDNAV_RG when on a resource group). Closing the shell (`exit`,
// Ctrl-d) or pressing Ctrl-q returns to the navigator.
package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

// vt10x publicly exposes Glyph.Mode but keeps the bit constants private.
// Mirror them so we can render bold / underline / reverse correctly.
const (
	vtAttrReverse   int16 = 1 << 0
	vtAttrUnderline int16 = 1 << 1
	vtAttrBold      int16 = 1 << 2
	vtAttrItalic    int16 = 1 << 4
	vtAttrBlink     int16 = 1 << 5
)

// termSession owns the pty + virtual terminal + child process.
// Created once when the user presses `x`, torn down when the child
// exits or the user presses Ctrl-q. The `pty` here is the
// platform-agnostic go-pty interface — POSIX pty pair on Unix,
// ConPTY on Windows. Same Read/Write/Resize calls land on either.
type termSession struct {
	cmd     *pty.Cmd
	pty     pty.Pty
	vt      vt10x.Terminal
	cols    int
	rows    int
	closed  bool
	closeMu sync.Mutex
	exited  chan struct{}
	err     error
}

// termPaintMsg is a tick-style message sent by readPump after the PTY
// produces output. It carries no payload — the goroutine writes
// directly into vt10x state, this just nudges bubbletea to re-render.
type termPaintMsg struct{ at time.Time }

// termExitMsg fires once the child process exits. err is non-nil only
// when the child died for an unusual reason (signal, exec failure).
// A clean `exit 0` lands here with err == nil.
type termExitMsg struct{ err error }

// terminal model. Embedded inside the main *model when active.
type terminal struct {
	session *termSession
	theme   styles.Theme
	styles  styles.ThemeStyles
	cloud   string // "Azure" / "GCP" / "AWS" / "" for default
	context string // human-readable "sub=… rg=…" header line
	width   int
	height  int
	// paintEpoch lets us coalesce a flood of read-pump nudges into one
	// re-render per visible frame. Each readPump tick increments it;
	// View() always renders the latest snapshot anyway, so the queue
	// length doesn't matter — but we pass it along so bubbletea has a
	// stable identity for the message and can dedupe.
	paintEpoch uint64
	// notice is a one-line banner shown above the terminal frame —
	// e.g. "x is read-only at the cloud level — drill into a sub
	// first" or after an unexpected child exit. Cleared on first key.
	notice string
}

// startTerminal launches the user's $SHELL inside a freshly-allocated
// PTY, wires up the vt10x emulator, and returns a tea.Cmd that reads
// the first chunk. The session keeps running as long as the user
// stays on the terminal page; exiting the shell sends termExitMsg.
//
// env carries the cloud-context exports (CLOUDNAV_SUB, etc.) the
// caller built from the row under the cursor. The shell inherits all
// of os.Environ() plus those.
func startTerminal(cloud string, context string, env []string, width, height int) (*terminal, tea.Cmd, error) {
	cols, rows := termInnerSize(width, height)

	shellPath, shellArgs := defaultShell()
	ptmx, err := pty.New()
	if err != nil {
		return nil, nil, fmt.Errorf("open pty: %w", err)
	}
	if err := ptmx.Resize(cols, rows); err != nil {
		_ = ptmx.Close()
		return nil, nil, fmt.Errorf("size pty: %w", err)
	}

	cmd := ptmx.Command(shellPath, shellArgs...)
	cmd.Env = append(os.Environ(), env...)
	// Hint to apps inside the PTY that we're a 256-colour xterm — vt10x
	// only models that level of capability, so anything richer just
	// produces noise we'd have to strip.
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, nil, fmt.Errorf("start shell: %w", err)
	}

	vt := vt10x.New(vt10x.WithWriter(ptmx), vt10x.WithSize(cols, rows))

	session := &termSession{
		cmd:    cmd,
		pty:    ptmx,
		vt:     vt,
		cols:   cols,
		rows:   rows,
		exited: make(chan struct{}),
	}

	theme := styles.ThemeFor(cloud)

	t := &terminal{
		session: session,
		theme:   theme,
		styles:  theme.Build(),
		cloud:   cloud,
		context: context,
		width:   width,
		height:  height,
	}

	// Wait for the child process in the background so the exit message
	// is delivered as soon as the user types `exit`. The waiter must
	// also signal the read-pump to drain the pty so any final output
	// makes it to the screen.
	go func() {
		err := cmd.Wait()
		session.closeMu.Lock()
		session.err = err
		session.closed = true
		session.closeMu.Unlock()
		// Closing the pty here unblocks any blocked Read in the pump.
		_ = ptmx.Close()
		close(session.exited)
	}()

	return t, t.pump(), nil
}

// pump returns the tea.Cmd that drains a chunk of bytes from the PTY
// into the vt10x emulator and re-arms itself. Bubbletea calls a Cmd
// once per message; we keep returning a fresh pump until the read
// fails (EOF / closed pty) at which point we surface termExitMsg.
//
// We read in 4 KB chunks. vt10x writes are cheap (parser pushes into
// an in-memory buffer), so latency is bounded by the pump tick budget,
// not the read size.
func (t *terminal) pump() tea.Cmd {
	session := t.session
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := session.pty.Read(buf)
		if n > 0 {
			session.vt.Write(buf[:n])
		}
		if err != nil {
			// io.EOF / "input/output error" both mean the master
			// closed — pair this with the cmd.Wait goroutine to
			// surface the actual exit code.
			<-session.exited
			session.closeMu.Lock()
			exitErr := session.err
			session.closeMu.Unlock()
			if exitErr == io.EOF {
				exitErr = nil
			}
			return termExitMsg{err: exitErr}
		}
		return termPaintMsg{at: time.Now()}
	}
}

// Update handles key/resize/paint/exit messages. Returns the (possibly
// updated) terminal plus the next tea.Cmd. A nil terminal in the
// returned pair means the session has ended and the caller should
// drop the page.
func (t *terminal) Update(msg tea.Msg) (*terminal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height = msg.Width, msg.Height
		cols, rows := termInnerSize(msg.Width, msg.Height)
		if cols != t.session.cols || rows != t.session.rows {
			t.session.cols, t.session.rows = cols, rows
			t.session.vt.Resize(cols, rows)
			_ = t.session.pty.Resize(cols, rows)
		}
		return t, nil

	case tea.KeyMsg:
		t.notice = ""
		// Ctrl-q is the cloudnav-side escape hatch — leaves the
		// terminal page without killing the shell underneath, except
		// that we always tear it down so the session doesn't leak.
		if msg.String() == "ctrl+q" {
			return nil, t.terminate()
		}
		bytes := keyToBytes(msg)
		if len(bytes) == 0 {
			return t, nil
		}
		if _, err := t.session.pty.Write(bytes); err != nil {
			t.notice = "write to terminal failed: " + err.Error()
		}
		return t, nil

	case termPaintMsg:
		t.paintEpoch++
		// Re-arm the pump for the next chunk.
		return t, t.pump()

	case termExitMsg:
		// Child exited (typed `exit`, Ctrl-d, etc.). Drop the page.
		return nil, nil
	}
	return t, nil
}

// terminate sends SIGHUP to the child, closes the PTY master, and
// returns a no-op tea.Cmd. Used by Ctrl-q.
func (t *terminal) terminate() tea.Cmd {
	if t.session != nil {
		if t.session.cmd != nil && t.session.cmd.Process != nil {
			_ = t.session.cmd.Process.Signal(os.Interrupt)
		}
		_ = t.session.pty.Close()
	}
	return nil
}

// View renders the chrome (header + frame + status bar) plus the
// vt10x screen buffer styled with the active theme.
func (t *terminal) View() string {
	if t.width <= 0 || t.height <= 0 {
		return ""
	}
	header := t.headerBar()
	footer := t.statusBar()
	chromeH := lipgloss.Height(header) + lipgloss.Height(footer)
	if t.notice != "" {
		chromeH++
	}
	frameBodyH := t.height - chromeH - 2 // -2 for the rounded border lines
	if frameBodyH < 3 {
		frameBodyH = 3
	}
	cols, rows := t.session.cols, t.session.rows
	if rows > frameBodyH {
		rows = frameBodyH
	}

	body := t.renderScreen(cols, rows)
	frame := t.styles.Frame.
		Width(t.width - 2).
		Render(body)

	parts := []string{header}
	if t.notice != "" {
		notice := lipgloss.NewStyle().
			Foreground(t.theme.Accent).
			Italic(true).
			Padding(0, 1).
			Render("⚠ " + t.notice)
		parts = append(parts, notice)
	}
	parts = append(parts, frame, footer)
	return strings.Join(parts, "\n")
}

// renderScreen walks the vt10x grid one cell at a time, mapping its
// glyph attributes to lipgloss styles. We lock the state for the
// entire pass so the read-pump can't shift cells underneath us.
//
// Performance: for an 80×24 grid that's ~1900 cells; lipgloss styles
// are reused so the per-cell cost is one .Render() and one rune
// append. Easily fits inside a 60 fps budget.
func (t *terminal) renderScreen(cols, rows int) string {
	t.session.vt.Lock()
	defer t.session.vt.Unlock()

	cur := t.session.vt.Cursor()
	cursorVisible := t.session.vt.CursorVisible()

	var b strings.Builder
	b.Grow(cols * rows * 4)

	for y := 0; y < rows; y++ {
		// Coalesce runs of cells with the same style into a single
		// .Render() call. Saves ~80% of the lipgloss work on a
		// typical text line.
		var run []rune
		var runStyle lipgloss.Style
		var runHasStyle bool
		flush := func() {
			if len(run) == 0 {
				return
			}
			if runHasStyle {
				b.WriteString(runStyle.Render(string(run)))
			} else {
				b.WriteString(string(run))
			}
			run = run[:0]
			runHasStyle = false
		}
		for x := 0; x < cols; x++ {
			g := t.session.vt.Cell(x, y)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			isCursor := cursorVisible && x == cur.X && y == cur.Y
			style, hasStyle := t.styleForGlyph(g, isCursor)
			if hasStyle != runHasStyle || style.GetForeground() != runStyle.GetForeground() ||
				style.GetBackground() != runStyle.GetBackground() ||
				style.GetBold() != runStyle.GetBold() ||
				style.GetUnderline() != runStyle.GetUnderline() ||
				style.GetItalic() != runStyle.GetItalic() ||
				style.GetReverse() != runStyle.GetReverse() {
				flush()
				runStyle = style
				runHasStyle = hasStyle
			}
			run = append(run, ch)
		}
		flush()
		if y < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// styleForGlyph maps vt10x cell attrs to a lipgloss style. The cursor
// cell wins over any cell-level styling — we paint it with the
// theme's Cursor block so it stays visible against any background.
func (t *terminal) styleForGlyph(g vt10x.Glyph, isCursor bool) (lipgloss.Style, bool) {
	if isCursor {
		return t.styles.Cursor, true
	}
	style := lipgloss.NewStyle()
	hasStyle := false
	if fg, ok := vtColor(g.FG); ok {
		style = style.Foreground(fg)
		hasStyle = true
	}
	if bg, ok := vtColor(g.BG); ok {
		style = style.Background(bg)
		hasStyle = true
	}
	if g.Mode&vtAttrBold != 0 {
		style = style.Bold(true)
		hasStyle = true
	}
	if g.Mode&vtAttrUnderline != 0 {
		style = style.Underline(true)
		hasStyle = true
	}
	if g.Mode&vtAttrItalic != 0 {
		style = style.Italic(true)
		hasStyle = true
	}
	if g.Mode&vtAttrReverse != 0 {
		style = style.Reverse(true)
		hasStyle = true
	}
	if g.Mode&vtAttrBlink != 0 {
		style = style.Blink(true)
		hasStyle = true
	}
	return style, hasStyle
}

// vtColor maps a vt10x.Color (ANSI 0-15, xterm 16-255, or default) to
// a lipgloss colour. Default colours render as no-style — the
// terminal frame already supplies a background, so leaving the cell
// unset lets the frame's background show through.
func vtColor(c vt10x.Color) (lipgloss.Color, bool) {
	if c == vt10x.DefaultFG || c == vt10x.DefaultBG || c == vt10x.DefaultCursor {
		return "", false
	}
	if uint32(c) < 256 {
		return lipgloss.Color(fmt.Sprintf("%d", uint32(c))), true
	}
	return "", false
}

// headerBar renders the strip above the frame. Brand pill on the
// left ("GCP" / "AWS" / "Azure"), context on the right (sub / rg).
func (t *terminal) headerBar() string {
	cloud := t.theme.Name
	if cloud == "" {
		cloud = "cloudnav"
	}
	pill := t.styles.Title.Render(" " + cloud + " terminal ")
	left := pill
	if t.context != "" {
		left = pill + "  " + t.styles.Context.Render(t.context)
	}
	clock := t.styles.HintText.Render(time.Now().Format("15:04:05"))
	width := t.width
	if width < 20 {
		width = 20
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(clock)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + clock
}

// statusBar renders the keybind strip at the bottom — short, dense,
// always visible so the user always knows how to get out.
func (t *terminal) statusBar() string {
	hint := func(key, desc string) string {
		return t.styles.KeyHint.Render(" "+key+" ") + " " + t.styles.HintText.Render(desc)
	}
	left := strings.Join([]string{
		hint("ctrl+d / exit", "close"),
		hint("ctrl+q", "back to navigator"),
		hint("ctrl+l", "clear"),
	}, "   ")
	right := t.styles.HintText.Render("PTY " +
		fmt.Sprintf("%dx%d", t.session.cols, t.session.rows))
	width := t.width
	if width < 20 {
		width = 20
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// termInnerSize converts the available bubbletea window into the cols
// and rows we should give the PTY. Reserves rows for header (1) +
// status bar (1) + frame border (2). Reserves cols for the frame
// border (2). Floors at 80×24 so vim and friends behave on tiny
// terminals — vt10x will scroll content that overflows.
func termInnerSize(width, height int) (cols, rows int) {
	cols = width - 2
	rows = height - 4
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	return cols, rows
}

// keyToBytes translates a bubbletea KeyMsg into the byte sequence a
// real xterm would send. We hand-roll the table rather than relying
// on tea.Key.Runes for the common cases because:
//   - bubbletea normalizes ctrl+letter as Type=KeyCtrlA etc, which is
//     the ASCII value we want to send anyway.
//   - special keys (arrows, F-keys) need explicit ESC sequences.
func keyToBytes(msg tea.KeyMsg) []byte {
	// Plain runes (typed text) — pass through as UTF-8.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		return []byte(string(msg.Runes))
	}
	switch msg.Type {
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyShiftTab:
		return []byte("\x1b[Z")
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyHome:
		return []byte("\x1bOH")
	case tea.KeyEnd:
		return []byte("\x1bOF")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")
	}
	// Ctrl combinations land as KeyCtrlA … KeyCtrlZ in bubbletea.
	// Their string form is "ctrl+a" etc, which is more readable than
	// the giant switch above — fall back to that for control keys.
	s := msg.String()
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		c := s[5]
		if c >= 'a' && c <= 'z' {
			return []byte{c - 'a' + 1}
		}
	}
	if strings.HasPrefix(s, "alt+") && len(s) >= 5 {
		// Alt-X sends ESC + X — what most terminals call a "meta"
		// prefix. Sufficient for shells that bind alt-bindings.
		return append([]byte{0x1b}, []byte(s[4:])...)
	}
	return nil
}

// buildTermContext composes the human-readable "sub=… rg=…" header
// line and the matching env var slice for the row under the cursor.
// Missing pieces are skipped — the line is empty at the cloud level.
func buildTermContext(cloud string, n provider.Node) (label string, env []string) {
	parts := []string{}
	switch cloud {
	case "Azure":
		if sub := contextSubID(n); sub != "" {
			parts = append(parts, "sub="+truncID(sub))
			env = append(env,
				"CLOUDNAV_SUB="+sub,
				"AZURE_SUBSCRIPTION_ID="+sub,
			)
		}
		if rg := contextRG(n); rg != "" {
			parts = append(parts, "rg="+rg)
			env = append(env, "CLOUDNAV_RG="+rg)
		}
	case "GCP":
		if proj := nodeMeta(n, "projectId"); proj != "" {
			parts = append(parts, "project="+proj)
			env = append(env,
				"CLOUDNAV_PROJECT="+proj,
				"CLOUDSDK_CORE_PROJECT="+proj,
				"GOOGLE_CLOUD_PROJECT="+proj,
			)
		}
	case "AWS":
		if acct := nodeMeta(n, "accountId"); acct != "" {
			parts = append(parts, "account="+acct)
			env = append(env, "CLOUDNAV_AWS_ACCOUNT="+acct)
		}
		if profile := nodeMeta(n, "profile"); profile != "" {
			parts = append(parts, "profile="+profile)
			env = append(env, "AWS_PROFILE="+profile)
		}
		if region := nodeMeta(n, "region"); region != "" {
			parts = append(parts, "region="+region)
			env = append(env, "AWS_REGION="+region)
		}
	}
	return strings.Join(parts, "  "), env
}

// nodeMeta is a nil-safe lookup into Node.Meta. Walks the parent
// chain so a leaf resource still picks up the project / account it
// inherits from a higher frame.
func nodeMeta(n provider.Node, key string) string {
	if v := n.Meta[key]; v != "" {
		return v
	}
	if n.Parent != nil {
		return nodeMeta(*n.Parent, key)
	}
	return ""
}

// defaultShell picks the shell to spawn inside the PTY. Honours the
// user's $SHELL on Unix; on Windows where $SHELL is rarely set, we
// fall back to %COMSPEC% (cmd) and prefer powershell when available
// — matching the conventions go-pty's ConPTY backend expects.
//
// `-l` makes the shell read login profiles so the prompt and aliases
// look like the user's normal terminal. We omit `-l` on Windows
// shells since cmd / powershell don't accept it.
func defaultShell() (path string, args []string) {
	if v := os.Getenv("SHELL"); v != "" {
		return v, []string{"-l"}
	}
	if v := os.Getenv("ComSpec"); v != "" {
		return v, nil
	}
	return "/bin/bash", []string{"-l"}
}
