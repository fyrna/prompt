package prompt

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// keys
const (
	keyArrowUp   = "\x1b[A"
	keyArrowDown = "\x1b[B"
	keyEnter     = "\r"
	keySpace     = " "
	keyBackspace = "\x7f"
	keyCtrlC     = 3
	keyCtrlQ     = 17
)

// terminal wrapper
type terminal struct {
	fd       int
	oldState *term.State
}

func newTerminal() *terminal {
	return &terminal{fd: int(os.Stdin.Fd())}
}

func (t *terminal) enterRaw() (err error) {
	if !term.IsTerminal(t.fd) {
		return errors.New("stdin is not a terminal")
	}

	t.oldState, err = term.MakeRaw(t.fd)
	return
}

func (t *terminal) restore() error {
	if t.oldState == nil {
		return nil
	}
	return term.Restore(t.fd, t.oldState)
}

func (t *terminal) readKey() ([]byte, error) {
	buf := make([]byte, 8)
	n, err := os.Stdin.Read(buf)
	return buf[:n], err
}

func (t *terminal) clearScreenAndTop() {
	fmt.Print("\x1b[2J\x1b[H")
}

func (t *terminal) clearLine() {
	fmt.Print("\x1b[2K\r")
}

func (t *terminal) moveCursorRight(cols int) {
	fmt.Printf("\r\x1b[%dC", cols)
}

func (t *terminal) moveCursorUp() {
	fmt.Printf("\x1b[A")
}

type screenCfg struct {
	clear bool
}

func (s screenCfg) shouldClear() bool {
	return s.clear
}

type Theme struct {
	Prompt   string // e.g. "❯ "
	Selected string // e.g. "● "
	Cursor   string // e.g. "█ "
	Error    string // e.g. "\033[31m" red
}

var defaultTheme = Theme{
	Prompt:   "\x1b[32m❯\x1b[0m ",
	Selected: "\x1b[34m✓\x1b[0m ",
	Cursor:   "█ ",
}

func chooseTheme(t *Theme) Theme {
	if t == nil {
		return defaultTheme
	}
	return *t
}

// input
type InputPrompt struct {
	title, placeholder string
	valuePtr           *string
	mask               bool
	validate           func(string) error
	theme              *Theme

	screenCfg
}

func NewInput() *InputPrompt {
	return &InputPrompt{}
}

func (ip InputPrompt) Title(s string) *InputPrompt {
	ip.title = s
	return &ip
}

func (ip InputPrompt) Placeholder(s string) *InputPrompt {
	ip.placeholder = s
	return &ip
}

func (ip InputPrompt) Value(p *string) *InputPrompt {
	ip.valuePtr = p
	return &ip
}

func (ip InputPrompt) Theme(t *Theme) *InputPrompt {
	ip.theme = t
	return &ip
}

func (ip InputPrompt) ClearScreen(on bool) *InputPrompt {
	ip.clear = on
	return &ip
}

func (ip InputPrompt) Run() (string, error) {
	term := newTerminal()

	if err := term.enterRaw(); err != nil {
		return "", err
	}

	defer func() {
		_ = term.restore()
		fmt.Println()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(sig)

	go func() {
		<-sig
		term.restore()
		os.Exit(1)
	}()

	var buf []rune

	if ip.valuePtr != nil && *ip.valuePtr != "" {
		buf = []rune(*ip.valuePtr)
	}

	cursor := len(buf)

	// render
	for {
		if ip.shouldClear() {
			term.clearScreenAndTop()
		}

		term.clearLine()

		if ip.title != "" {
			fmt.Print(ip.title + ": ")
		}

		if len(buf) == 0 && ip.placeholder != "" {
			fmt.Print("\x1b[38;5;241m" + ip.placeholder + "\x1b[0m")
		} else {
			fmt.Print(string(buf))
		}

		prefix := 0

		if ip.title != "" {
			prefix = runewidth.StringWidth(ip.title) + 2
		}

		term.moveCursorRight(prefix + runewidth.StringWidth(string(buf[:cursor])))

		key, err := term.readKey()
		if err != nil {
			return "", err
		}

		// arrow keys
		if len(key) >= 3 && key[0] == 0x1b && key[1] == '[' {
			switch key[2] {
			case 'D': // left
				if cursor > 0 {
					cursor--
				}
				continue
			case 'C': // right
				if cursor < len(buf) {
					cursor++
				}
				continue
			case 'A', 'B':
				continue
			}
		}

		b0 := key[0]

		switch {
		case b0 == '\r' || b0 == '\n':
			res := string(buf)

			if ip.valuePtr != nil {
				*ip.valuePtr = res
			}

			return res, nil
		case b0 == keyCtrlC, b0 == keyCtrlQ:
			return "", errors.New("canceled")
		case b0 == 127 || b0 == 8: // backspace
			if cursor > 0 {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
			}
		case b0 >= 32 && b0 <= 126: // printable
			r, _ := utf8.DecodeRune(key)

			if r != utf8.RuneError {
				buf = append(buf[:cursor], append([]rune{r}, buf[cursor:]...)...)
				cursor++
			}
		}
	}
}

// Confirm
type Confirm struct {
	question string
	def      bool

	screenCfg
}

func NewConfirm(q string) *Confirm {
	return &Confirm{question: q}
}

func (c Confirm) ClearScreen(on bool) *Confirm {
	c.clear = on
	return &c
}

func (c *Confirm) Run() (bool, error) {
	term := newTerminal()
	defer term.restore()

	df := "y/N"

	if c.def {
		df = "Y/n"
	}

	for {
		if c.shouldClear() {
			term.clearScreenAndTop()
		}

		term.clearLine()

		fmt.Printf("%s [%s]: ", c.question, df)

		key, err := term.readKey()
		if err != nil {
			return c.def, err
		}

		switch b := key[0]; b {
		case keyCtrlC, keyCtrlQ:
			return c.def, errors.New("canceled")
		case '\r', '\n':
			return c.def, nil
		case 'y', 'Y':
			return true, nil
		case 'n', 'N':
			return false, nil
		default:
			return c.def, errors.New("invalid input")
		}
	}
}

// select
type Select struct {
	title   string
	options []string
	theme   *Theme

	screenCfg
}

func NewSelect() *Select {
	return &Select{}
}

func (s Select) Title(t string) *Select {
	s.title = t
	return &s
}

func (s Select) Options(o []string) *Select {
	s.options = o
	return &s
}

func (s Select) ClearScreen(on bool) *Select {
	s.clear = on
	return &s
}

func (s *Select) Run() (string, error) {
	term := newTerminal()
	theme := chooseTheme(s.theme)

	if err := term.enterRaw(); err != nil {
		return "", err
	}

	defer func() {
		_ = term.restore()
		fmt.Println()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(sig)

	go func() {
		<-sig
		term.restore()
		os.Exit(1)
	}()

	cursor := 0

	if s.title != "" {
		fmt.Println(s.title)
	}

	for {
		// render
		if s.shouldClear() {
			term.clearScreenAndTop()
		} else {
			term.clearLine()
		}

		for i, opt := range s.options {
			fmt.Printf("\r") // force cursor to 0
			prefix := "  "

			if i == cursor {
				prefix = theme.Prompt
			}

			fmt.Printf("%s%s\n", prefix, opt)
		}

		key, err := term.readKey()
		if err != nil {
			return "", err
		}

		if len(key) == 1 && key[0] == keyCtrlC {
			return "", errors.New("canceled")
		}

		switch string(key) {
		case keyArrowUp:
			if cursor > 0 {
				cursor--
			}
		case keyArrowDown:
			if cursor < len(s.options)-1 {
				cursor++
			}
		case keyEnter:
			return s.options[cursor], nil
		}

		for range len(s.options) {
			term.moveCursorUp()
		}
	}
}

// Multi-select
type MultiSelect struct {
	title   string
	options []string
	theme   *Theme

	screenCfg
}

func NewMultiSelect() *MultiSelect {
	return &MultiSelect{}
}

func (m MultiSelect) Title(t string) *MultiSelect {
	m.title = t
	return &m
}

func (m MultiSelect) Options(o []string) *MultiSelect {
	m.options = o
	return &m
}

func (m MultiSelect) ClearScreen(on bool) *MultiSelect {
	m.clear = on
	return &m
}

func (m *MultiSelect) Run() ([]string, error) {
	term := newTerminal()
	theme := chooseTheme(m.theme)

	if err := term.enterRaw(); err != nil {
		return nil, err
	}

	defer func() {
		_ = term.restore()
		fmt.Println()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(sig)

	go func() {
		<-sig
		term.restore()
		os.Exit(1)
	}()

	cursor := 0
	selected := make([]bool, len(m.options))

	if m.title != "" {
		fmt.Println(m.title)
	}

	for {
		// render
		if m.shouldClear() {
			term.clearScreenAndTop()
		}

		term.clearLine()

		for i, opt := range m.options {
			fmt.Printf("\r") // force to 0
			mark := " "

			if selected[i] {
				mark = theme.Selected
			}

			prefix := "  "

			if i == cursor {
				prefix = theme.Prompt
			}

			fmt.Printf("%s%s %s\n", prefix, mark, opt)
		}

		key, err := term.readKey()
		if err != nil {
			return nil, err
		}

		if len(key) == 1 && key[0] == keyCtrlC {
			return nil, errors.New("canceled")
		}

		switch string(key) {
		case keyArrowUp:
			if cursor > 0 {
				cursor--
			}
		case keyArrowDown:
			if cursor < len(m.options)-1 {
				cursor++
			}
		case keySpace:
			selected[cursor] = !selected[cursor]
		case keyEnter:
			var chosen []string

			for i, v := range selected {
				if v {
					chosen = append(chosen, m.options[i])
				}
			}

			return chosen, nil
		}

		for range len(m.options) {
			term.moveCursorUp()
		}
	}
}
