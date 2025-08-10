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
	keyArrowUp    = "\x1b[A"
	keyArrowDown  = "\x1b[B"
	keyEnter      = "\r"
	keySpace      = " "
	keyBackspace1 = 0x7f
	keyBackspace2 = 0x08
	keyCtrlC      = 0x03
	keyCtrlQ      = 0x11
)

// terminal wrapper
type terminal struct {
	fd, width, height int
	oldState          *term.State
}

func newTerminal() (*terminal, error) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		return nil, errors.New("stdin is not a terminal")
	}

	w, h, err := term.GetSize(fd)
	if err != nil {
		return nil, err
	}

	return &terminal{fd: fd, width: w, height: h}, nil
}

func (t *terminal) enterRaw() (err error) {
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

func (t *terminal) moveCursorUp(times int) {
	for range times {
		t.printf("\x1b[A")
	}
}

func (t *terminal) println(a ...any) {
	fmt.Println(a...)
}

func (t *terminal) printf(format string, a ...any) {
	fmt.Printf(format, a...)
}

// helpers
func runRaw(fn func(*terminal) error) error {
	term, err := newTerminal()
	if err != nil {
		return err
	}

	if err := term.enterRaw(); err != nil {
		return err
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

	return fn(term)
}

// simple theming
type Theme struct {
	Prompt   string // e.g. "❯ "
	Selected string // e.g. "● "
	Cursor   string // e.g. "█ "
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
	validate           func(string) error
	theme              *Theme
	clearScreen        bool
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
	ip.clearScreen = on
	return &ip
}

func (ip InputPrompt) Validate(fn func(string) error) *InputPrompt {
	ip.validate = fn
	return &ip
}

func (ip *InputPrompt) Run() (string, error) {
	var res string

	err := runRaw(func(term *terminal) error {
		var buf []rune

		if ip.valuePtr != nil && *ip.valuePtr != "" {
			buf = []rune(*ip.valuePtr)
		}

		cursor := len(buf)

		// render
		for {
			if ip.clearScreen {
				term.clearScreenAndTop()
			}

			term.clearLine()

			if ip.title != "" {
				term.printf("%s", ip.title)
			}

			if len(buf) == 0 && ip.placeholder != "" {
				term.printf("\x1b[38;5;241m%s\x1b[0m", ip.placeholder)
			} else {
				term.printf("%s", string(buf))
			}

			prefix := 0

			if ip.title != "" {
				prefix = runewidth.StringWidth(ip.title)
			}

			term.moveCursorRight(prefix + runewidth.StringWidth(string(buf[:cursor])))

			key, err := term.readKey()
			if err != nil {
				return err
			}

			// arrow keys
			if len(key) >= 3 && key[0] == '\x1b' && key[1] == '[' {
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

				if ip.validate != nil {
					if err := ip.validate(res); err != nil {
						term.printf("\n%s\n", err.Error())
						continue
					}
				}

				if ip.valuePtr != nil {
					*ip.valuePtr = res
				}

				return nil
			case b0 == keyCtrlC, b0 == keyCtrlQ:
				return errors.New("canceled")
			case b0 == keyBackspace1 || b0 == keyBackspace2:
				if cursor > 0 {
					buf = append(buf[:cursor-1], buf[cursor:]...)
					cursor--
				}
			case b0 >= 32 && b0 <= 126: // printable
				r, size := utf8.DecodeRune(key)
				if r == utf8.RuneError || size == 0 {
					continue
				}

				buf = append(buf[:cursor], append([]rune{r}, buf[cursor:]...)...)
				cursor++
			}
		}
	})

	if err != nil {
		return "", err
	}

	return res, nil
}

// Confirm
type Confirm struct {
	question         string
	def, clearScreen bool
}

func NewConfirm() *Confirm {
	return &Confirm{}
}

func (c Confirm) Question(q string) *Confirm {
	c.question = q
	return &c
}

func (c Confirm) ClearScreen(on bool) *Confirm {
	c.clearScreen = on
	return &c
}

func (c *Confirm) Run() (bool, error) {
	var res bool

	err := runRaw(func(t *terminal) error {
		df := "y/N"

		if c.def {
			df = "Y/n"
		}

		for {
			if c.clearScreen {
				t.clearScreenAndTop()
			}

			t.clearLine()
			t.printf("%s [%s]", c.question, df)

			key, err := t.readKey()
			if err != nil {
				return err
			}

			switch b := key[0]; b {
			case keyCtrlC, keyCtrlQ:
				return errors.New("canceled")
			case '\r', '\n':
				res = c.def
				return nil
			case 'y', 'Y':
				res = true
				return nil
			case 'n', 'N':
				res = false
				return nil
			default:
				return errors.New("invalid input")
			}
		}
	})

	if err != nil {
		return c.def, err
	}

	return res, nil
}

// select
type Select struct {
	title      string
	options    []string
	theme      *Theme
	clearSreen bool
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

func (s Select) Theme(t *Theme) *Select {
	s.theme = t
	return &s
}

func (s Select) ClearScreen(on bool) *Select {
	s.clearSreen = on
	return &s
}

func (s *Select) Run() (string, error) {
	if len(s.options) == 0 {
		return "", errors.New("no options")
	}

	var res string

	err := runRaw(func(t *terminal) error {
		cursor := 0
		theme := chooseTheme(s.theme)

		if s.title != "" {
			t.println(s.title)
		}

		for {
			if s.clearSreen {
				t.clearScreenAndTop()
			} else {
				t.clearLine()
			}

			for i, opt := range s.options {
				prefix := "  "

				if i == cursor {
					prefix = theme.Prompt
				}

				t.printf("\r%s%s\n", prefix, opt)
			}

			key, err := t.readKey()
			if err != nil {
				return err
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
				res = s.options[cursor]
				return nil
			case string([]byte{keyCtrlC}):
				return errors.New("canceled")
			}

			t.moveCursorUp(len(s.options))
		}
	})

	if err != nil {
		return "", err
	}

	return res, nil
}

// Multi-select
type MultiSelect struct {
	title       string
	options     []string
	theme       *Theme
	clearScreen bool
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

func (m MultiSelect) Theme(t *Theme) *MultiSelect {
	m.theme = t
	return &m
}

func (m MultiSelect) ClearScreen(on bool) *MultiSelect {
	m.clearScreen = on
	return &m
}

func (m *MultiSelect) Run() ([]string, error) {
	if len(m.options) == 0 {
		return nil, errors.New("no options")
	}

	var chosen []string

	err := runRaw(func(t *terminal) error {
		cursor := 0
		selected := make([]bool, len(m.options))
		theme := chooseTheme(m.theme)

		if m.title != "" {
			t.println(m.title)
		}

		for {
			if m.clearScreen {
				t.clearScreenAndTop()
			} else {
				t.clearLine()
			}

			for i, opt := range m.options {
				t.printf("\r")
				mark := "● "

				if selected[i] {
					mark = theme.Selected
				}

				prefix := "  "

				if i == cursor {
					prefix = theme.Prompt
				}

				t.printf("\r%s%s %s\n", prefix, mark, opt)
			}

			key, err := t.readKey()
			if err != nil {
				return err
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
				for i, v := range selected {
					if v {
						chosen = append(chosen, m.options[i])
					}
				}
				return nil
			case string([]byte{keyCtrlC}):
				return errors.New("canceled")
			}

			t.moveCursorUp(len(m.options))
		}
	})

	if err != nil {
		return nil, err
	}

	return chosen, nil
}
