package prompt

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// Option represents a selectable option
type Option struct {
	Text     string
	Value    interface{}
	selected bool
}

// NewOption creates a new Option
func NewOption(text string, value any) *Option {
	return &Option{
		Text:  text,
		Value: value,
	}
}

// Selected sets whether the option is selected by default
func (o *Option) Selected(selected bool) *Option {
	o.selected = selected
	return o
}

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
		fmt.Printf("\x1b[A")
	}
}

func (t *terminal) printf(m int, format string, a ...any) {
	fmt.Print(strings.Repeat(" ", m))
	fmt.Printf(format, a...)
}

func (t *terminal) println(m int, a ...any) {
	fmt.Print(strings.Repeat(" ", m))
	fmt.Println(a...)
}

func (t *terminal) marginTop(n int) {
	for range n {
		fmt.Println()
	}
}

func (t *terminal) marginBottom(n int) {
	for range n {
		fmt.Println()
	}
}

func runRaw(fn func(*terminal) error) error {
	t, err := newTerminal()
	if err != nil {
		return err
	}

	t.oldState, err = term.MakeRaw(t.fd)
	if err != nil {
		return err
	}

	defer func() {
		_ = t.restore()
		fmt.Println()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(sig)

	go func() {
		<-sig
		t.restore()
		os.Exit(1)
	}()

	return fn(t)
}

// Theme defines the styling for prompts
type Theme struct {
	Prompt, Cursor, Selected, Unselected string
	Error                                string
	MarginLeft, MarginTop, MarginBottom  int
}

var defaultTheme = Theme{
	Prompt:       "\x1b[32m❯\x1b[0m ",
	Cursor:       "█ ",
	Selected:     "\x1b[34m✓\x1b[0m ",
	Unselected:   "• ",
	Error:        "",
	MarginLeft:   1,
	MarginTop:    1,
	MarginBottom: 1,
}

func chooseTheme(t *Theme) Theme {
	if t == nil {
		return defaultTheme
	}
	return *t
}

// NewTheme creates a new theme with default values
func NewTheme() Theme {
	return defaultTheme
}

// Set modifies the theme with the given function
func (t Theme) Set(fn func(*Theme)) Theme {
	fn(&t)
	return t
}

// Confirm prompt
type Confirm struct {
	title       string
	def         bool
	clearScreen bool
	theme       *Theme
	valuePtr    *bool
}

// NewConfirm creates a new Confirm prompt
func NewConfirm() *Confirm {
	return &Confirm{}
}

// Question sets the question text
func (c Confirm) Title(title string) *Confirm {
	c.title = title
	return &c
}

// ClearScreen sets whether to clear screen before showing prompt
func (c Confirm) ClearScreen(on bool) *Confirm {
	c.clearScreen = on
	return &c
}

// Value sets the reference to store the result
func (c Confirm) Value(v *bool) *Confirm {
	c.valuePtr = v
	if v != nil {
		c.def = *v
	}
	return &c
}

// Run executes the prompt
func (c *Confirm) Run() error {
	var res bool

	err := runRaw(func(t *terminal) error {
		theme := chooseTheme(c.theme)
		margin := theme.MarginLeft
		df := "y/N"

		if c.def {
			df = "Y/n"
		}

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom)

		for {
			if c.clearScreen {
				t.clearScreenAndTop()
			}

			t.clearLine()
			t.printf(margin, "%s [%s]", c.title, df)

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
			}
		}
	})

	if err != nil {
		return err
	}

	if c.valuePtr != nil {
		*c.valuePtr = res
	}

	return nil
}

// InputPrompt for text input
type InputPrompt struct {
	title, placeholder string
	valuePtr           *string
	validate           func(string) error
	theme              *Theme
	clearScreen        bool
}

// NewInput creates a new Input prompt
func NewInput() *InputPrompt {
	return &InputPrompt{}
}

// Title sets the title text
func (ip InputPrompt) Title(s string) *InputPrompt {
	ip.title = s
	return &ip
}

// Placeholder sets the placeholder text
func (ip InputPrompt) Placeholder(s string) *InputPrompt {
	ip.placeholder = s
	return &ip
}

// Value sets the reference to store the result
func (ip InputPrompt) Value(v *string) *InputPrompt {
	ip.valuePtr = v
	return &ip
}

// Theme sets the theme
func (ip InputPrompt) Theme(t *Theme) *InputPrompt {
	ip.theme = t
	return &ip
}

// ClearScreen sets whether to clear screen before showing prompt
func (ip InputPrompt) ClearScreen(on bool) *InputPrompt {
	ip.clearScreen = on
	return &ip
}

// Validate sets the validation function
func (ip InputPrompt) Validate(fn func(string) error) *InputPrompt {
	ip.validate = fn
	return &ip
}

// Run executes the prompt
func (ip *InputPrompt) Run() error {
	var res string

	err := runRaw(func(t *terminal) error {
		var buf []rune

		if ip.valuePtr != nil && *ip.valuePtr != "" {
			buf = []rune(*ip.valuePtr)
		}

		theme := chooseTheme(ip.theme)
		cursor := len(buf)
		margin := theme.MarginLeft

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom)

		for {
			if ip.clearScreen {
				t.clearScreenAndTop()
			}

			t.clearLine()

			if ip.title != "" {
				t.printf(margin, "%s", ip.title)
			}

			if len(buf) == 0 && ip.placeholder != "" {
				t.printf(0, "\x1b[38;5;241m%s\x1b[0m", ip.placeholder)
			} else {
				t.printf(0, "%s", string(buf))
			}

			prefix := 0

			if ip.title != "" {
				prefix = runewidth.StringWidth(ip.title + " ")
			}

			t.moveCursorRight(prefix + runewidth.StringWidth(string(buf[:cursor])))

			key, err := t.readKey()
			if err != nil {
				return err
			}

			if len(key) >= 3 && key[0] == '\x1b' && key[1] == '[' {
				switch key[2] {
				case 'D':
					if cursor > 0 {
						cursor--
					}
					continue
				case 'C':
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
				res = string(buf)

				if ip.validate != nil {
					if err := ip.validate(res); err != nil {
						t.clearLine()
						t.printf(margin, "%s", theme.Error)
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
			case b0 >= 32 && b0 <= 126:
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
		return err
	}

	return nil
}

// Select prompt
type Select struct {
	title      string
	options    []*Option
	valuePtr   interface{}
	theme      *Theme
	clearSreen bool
}

// NewSelect creates a new Select prompt
func NewSelect() *Select {
	return &Select{}
}

// Title sets the title text
func (s Select) Title(t string) *Select {
	s.title = t
	return &s
}

// Options sets the available options
func (s Select) Options(opts ...[]*Option) *Select {
	for _, o := range opts {
		s.options = o
	}
	return &s
}

// Value sets the reference to store the result
func (s Select) Value(v interface{}) *Select {
	s.valuePtr = v
	return &s
}

// Theme sets the theme
func (s Select) Theme(t *Theme) *Select {
	s.theme = t
	return &s
}

// ClearScreen sets whether to clear screen before showing prompt
func (s Select) ClearScreen(on bool) *Select {
	s.clearSreen = on
	return &s
}

// Run executes the prompt
func (s *Select) Run() error {
	if len(s.options) == 0 {
		return errors.New("no options")
	}

	err := runRaw(func(t *terminal) error {
		cursor := 0
		theme := chooseTheme(s.theme)
		margin := theme.MarginLeft

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom)

		if s.title != "" {
			t.println(margin, s.title)
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

				t.printf(margin, "\r%s%s\n", prefix, opt.Text)
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
				if s.valuePtr != nil {
					selectedValue := s.options[cursor].Value
					ptrValue := reflect.ValueOf(s.valuePtr)
					if ptrValue.Kind() != reflect.Ptr {
						return errors.New("value must be a pointer")
					}
					ptrValue.Elem().Set(reflect.ValueOf(selectedValue))
				}
				return nil
			case string([]byte{keyCtrlC}):
				return errors.New("canceled")
			}

			t.moveCursorUp(len(s.options))
		}
	})

	return err
}

// MultiSelect prompt
type MultiSelect struct {
	title       string
	options     []*Option
	valuePtr    interface{}
	theme       *Theme
	clearScreen bool
}

// NewMultiSelect creates a new MultiSelect prompt
func NewMultiSelect() *MultiSelect {
	return &MultiSelect{}
}

// Title sets the title text
func (m MultiSelect) Title(t string) *MultiSelect {
	m.title = t
	return &m
}

// Options sets the available options
func (m MultiSelect) Options(o []*Option) *MultiSelect {
	m.options = o
	return &m
}

// Value sets the reference to store the result
func (m MultiSelect) Value(v interface{}) *MultiSelect {
	m.valuePtr = v
	return &m
}

// Theme sets the theme
func (m MultiSelect) Theme(t *Theme) *MultiSelect {
	m.theme = t
	return &m
}

// ClearScreen sets whether to clear screen before showing prompt
func (m MultiSelect) ClearScreen(on bool) *MultiSelect {
	m.clearScreen = on
	return &m
}

// Run executes the prompt
func (m *MultiSelect) Run() error {
	if len(m.options) == 0 {
		return errors.New("no options")
	}

	err := runRaw(func(t *terminal) error {
		cursor := 0
		theme := chooseTheme(m.theme)
		margin := theme.MarginLeft

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom - 1)

		if m.title != "" {
			t.println(margin, m.title)
		}

		for {
			if m.clearScreen {
				t.clearScreenAndTop()
			} else {
				t.clearLine()
			}

			for i, opt := range m.options {
				fmt.Printf("\r")
				mark := theme.Unselected

				if opt.selected {
					mark = theme.Selected
				}

				prefix := "  "

				if i == cursor {
					prefix = theme.Prompt
				}

				t.printf(margin, "\r%s%s %s\n", prefix, mark, opt.Text)
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
				m.options[cursor].selected = !m.options[cursor].selected
			case keyEnter:
				if m.valuePtr != nil {
					ptrValue := reflect.ValueOf(m.valuePtr)
					if ptrValue.Kind() != reflect.Ptr {
						return errors.New("value must be a pointer")
					}

					// Handle both []string and []interface{}
					elem := ptrValue.Elem()
					switch elem.Kind() {
					case reflect.Slice:
						if elem.Type().Elem().Kind() == reflect.String {
							// For []string
							selectedStrings := make([]string, 0)
							for _, opt := range m.options {
								if opt.selected {
									if str, ok := opt.Value.(string); ok {
										selectedStrings = append(selectedStrings, str)
									}
								}
							}
							elem.Set(reflect.ValueOf(selectedStrings))
						} else {
							// For other slice types
							selectedValues := make([]interface{}, 0)
							for _, opt := range m.options {
								if opt.selected {
									selectedValues = append(selectedValues, opt.Value)
								}
							}
							elem.Set(reflect.ValueOf(selectedValues))
						}
					}
				}
				return nil
			case string([]byte{keyCtrlC}):
				return errors.New("canceled")
			}

			t.moveCursorUp(len(m.options))
		}
	})

	return err
}
