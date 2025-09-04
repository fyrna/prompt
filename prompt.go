// a simple prompt library
//
// It supports various prompt types including confirmation, text input,
// single selection, and multi-selection prompts with customizable theming.
//
// **note** not work well on any OS except Linux (maybe)
package prompt

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"syscall"

	"github.com/fyrna/x/term"
	"github.com/fyrna/x/term/key"
	"github.com/mattn/go-runewidth"
)

// ErrCanceled is returned when the user cancels a prompt operation
// using Ctrl+C or Ctrl+Q.
var ErrCanceled = errors.New("canceled")

// terminal wrapper
type terminal struct {
	t             *term.Terminal
	kr            *key.Reader
	width, height int
}

func newTerminal() (*terminal, error) {
	t := term.NewStdinTerminal()
	if !t.IsTerminal() {
		return nil, errors.New("stdin is not a terminal")
	}

	w, h, err := t.GetSize()
	if err != nil {
		return nil, err
	}

	return &terminal{t: t, width: w, height: h}, nil
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

func (t *terminal) removeCursor() {
	fmt.Print("\033[?25l")
}

func (t *terminal) bringBack() {
	fmt.Print("\033[?25h")
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

func (t *terminal) helpBar(text string) {
	if text == "" {
		return
	}
	fmt.Print("\x1b[s")                // save cursor
	fmt.Printf("\x1b[%d;1H", t.height) // bottom row
	fmt.Print("\x1b[2K")               // clear line
	fmt.Print(text)
	fmt.Print("\x1b[u") // restore cursor
}

func runRaw(fn func(*terminal) error) error {
	t, err := newTerminal()
	if err != nil {
		return err
	}

	if err := t.t.MakeRaw(); err != nil {
		return err
	}

	t.kr = key.NewReader(t.t)

	defer func() {
		_ = t.t.Restore()
		fmt.Println()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(sig)

	go func() {
		<-sig
		t.t.Restore()
		os.Exit(1)
	}()

	return fn(t)
}

// Theme defines the styling and layout configuration for prompts.
// Use NewTheme() to create a new theme and modify it with Set().
type Theme struct {
	Prompt, Cursor, Selected, Unselected string
	Error, SelectHelp, MultiSelectHelp   string
	MarginTop, MarginBottom              int
}

var defaultTheme = Theme{
	Prompt:          "\x1b[32m❯\x1b[0m ",
	Selected:        "\x1b[34m✓\x1b[0m ",
	Unselected:      "• ",
	Error:           "",
	MarginTop:       0,
	MarginBottom:    1,
	SelectHelp:      "\x1b[38;5;245m[↑↓] navigate • [enter] confirm\x1b[0m",
	MultiSelectHelp: "\x1b[38;5;245m[↑↓] navigate • [space] select • [enter] confirm\x1b[0m",
}

func chooseTheme(t *Theme) Theme {
	if t == nil {
		return defaultTheme
	}
	return *t
}

// NewTheme creates a new theme with default values:
// - Prompt: Green "❯" symbol
// - Selected: Blue checkmark "✓"
// - Unselected: Bullet "•"
// - SelectHelp: Navigation instructions for single select
// - MultiSelectHelp: Navigation instructions for multi-select
// - MarginTop: 0, MarginBottom: 1
func NewTheme() Theme {
	return defaultTheme
}

// Set modifies the theme with the given function and returns the updated theme.
// This enables method chaining for theme customization.
func (t Theme) Set(fn func(*Theme)) Theme {
	fn(&t)
	return t
}

// Option represents a selectable option in Select and MultiSelect prompts.
type Option struct {
	Text     string
	Value    any
	selected bool
}

// NewOption creates a new Option with the given display text and value.
func NewOption(text string, value any) *Option {
	return &Option{
		Text:  text,
		Value: value,
	}
}

// Selected sets whether the option is selected by default (for MultiSelect).
func (o *Option) Selected(selected bool) *Option {
	o.selected = selected
	return o
}

// Confirm provides a yes/no confirmation prompt.
type Confirm struct {
	title       string
	def         bool
	clearScreen bool
	theme       *Theme
	valuePtr    *bool
}

// NewConfirm creates a new confirmation prompt.
func NewConfirm() *Confirm {
	return &Confirm{}
}

// Title sets the question text for the confirmation prompt.
func (c Confirm) Title(title string) *Confirm {
	c.title = title
	return &c
}

// ClearScreen sets whether to clear the terminal screen before showing the prompt.
func (c Confirm) ClearScreen(on bool) *Confirm {
	c.clearScreen = on
	return &c
}

// Value sets a pointer to a boolean variable where the result will be stored.
// If the pointer is non-nil, its current value will be used as the default.
func (c Confirm) Value(v *bool) *Confirm {
	c.valuePtr = v
	if v != nil {
		c.def = *v
	}
	return &c
}

// Run executes the confirmation prompt and returns the user's choice.
// Returns ErrCanceled if the user cancels the operation.
func (c *Confirm) Run() error {
	var res bool

	err := runRaw(func(t *terminal) error {
		theme := chooseTheme(c.theme)
		df := "y/N"

		if c.def {
			df = "Y/n"
		}

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom)

		t.removeCursor()
		defer t.bringBack()

		for {
			if c.clearScreen {
				t.clearScreenAndTop()
			}

			t.clearLine()
			fmt.Printf("%s [%s]", c.title, df)

			ev, err := t.kr.ReadEvent()
			if err != nil {
				return err
			}

			switch {
			case ev.IsCtrl('c') || ev.IsCtrl('q'):
				return errors.New("canceled")

			case ev.Key == key.Enter:
				res = c.def
				return nil

			case ev.Key == key.Rune && (ev.Rune == 'y' || ev.Rune == 'Y'):
				res = true
				return nil

			case ev.Key == key.Rune && (ev.Rune == 'n' || ev.Rune == 'N'):
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

// InputPrompt provides a text input prompt with optional validation.
type InputPrompt struct {
	title, placeholder string
	valuePtr           *string
	validate           func(string) error
	theme              *Theme
	clearScreen        bool
}

// NewInput creates a new text input prompt.
func NewInput() *InputPrompt {
	return &InputPrompt{}
}

// Title sets the title text displayed
func (ip InputPrompt) Title(s string) *InputPrompt {
	ip.title = s
	return &ip
}

// Placeholder sets the placeholder text shown when the input is empty.
func (ip InputPrompt) Placeholder(s string) *InputPrompt {
	ip.placeholder = s
	return &ip
}

// Value sets a pointer to a string variable where the input will be stored.
// If the pointer contains a value, it will be used as the initial input.
func (ip InputPrompt) Value(v *string) *InputPrompt {
	ip.valuePtr = v
	return &ip
}

// Theme sets the theme for the input prompt.
func (ip InputPrompt) Theme(t *Theme) *InputPrompt {
	ip.theme = t
	return &ip
}

// ClearScreen sets whether to clear the terminal screen before showing the prompt.
func (ip InputPrompt) ClearScreen(on bool) *InputPrompt {
	ip.clearScreen = on
	return &ip
}

// Validate sets a validation function that will be called on form submission.
// The function should return an error if the input is invalid.
func (ip InputPrompt) Validate(fn func(string) error) *InputPrompt {
	ip.validate = fn
	return &ip
}

// Run executes the input prompt and returns the user's input.
// Returns ErrCanceled if the user cancels the operation.
// Returns validation error if the input fails validation.
func (ip *InputPrompt) Run() error {
	var res string

	err := runRaw(func(t *terminal) error {
		var buf []rune

		if ip.valuePtr != nil && *ip.valuePtr != "" {
			buf = []rune(*ip.valuePtr)
		}

		theme := chooseTheme(ip.theme)
		cursor := len(buf)

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom)

		for {
			if ip.clearScreen {
				t.clearScreenAndTop()
			}

			t.clearLine()

			prefix := 0
			if ip.title != "" {
				fmt.Printf("%s", ip.title)
				prefix = runewidth.StringWidth(ip.title)
			}

			if len(buf) == 0 && ip.placeholder != "" {
				fmt.Printf("\x1b[38;5;241m%s\x1b[0m", ip.placeholder)
			} else {
				fmt.Printf("%s", string(buf))
			}

			textW := 0
			if len(buf) > 0 {
				textW = runewidth.StringWidth(string(buf[:cursor]))
			}

			t.moveCursorRight(prefix + textW)

			ev, err := t.kr.ReadEvent()
			if err != nil {
				return err
			}

			switch {
			case ev.IsCtrl('c'), ev.IsCtrl('q'):
				return ErrCanceled
			case ev.Key == key.Enter:
				res = string(buf)

				if ip.validate != nil {
					if err := ip.validate(res); err != nil {
						return err
					}
				}
				if ip.valuePtr != nil {
					*ip.valuePtr = res
				}

				return nil
			case ev.Key == key.Backspace:
				if cursor > 0 {
					buf = append(buf[:cursor-1], buf[cursor:]...)
					cursor--
				}
			case ev.Key == key.Left:
				if cursor > 0 {
					cursor--
				}
			case ev.Key == key.Right:
				if cursor < len(buf) {
					cursor++
				}
			case ev.Key == key.Rune:
				buf = append(buf[:cursor], append([]rune{ev.Rune}, buf[cursor:]...)...)
				cursor++
			case ev.Key == key.Space:
				buf = append(buf[:cursor], append([]rune{' '}, buf[cursor:]...)...)
				cursor++
			}
		}
	})

	if err != nil {
		return err
	}

	return nil
}

// Select provides a single-selection prompt from a list of options.
type Select struct {
	title      string
	options    []*Option
	valuePtr   any
	theme      *Theme
	clearSreen bool
}

// NewSelect creates a new single-selection prompt.
func NewSelect() *Select {
	return &Select{}
}

// Title sets the title text displayed above the options.
func (s Select) Title(t string) *Select {
	s.title = t
	return &s
}

// Options sets the available options for selection.
// Accepts one or more slices of Option pointers.
func (s Select) Options(opts ...[]*Option) *Select {
	for _, o := range opts {
		s.options = o
	}
	return &s
}

// Value sets a pointer where the selected option's value will be stored.
// The pointer type should match the type of the option values.
func (s Select) Value(v any) *Select {
	s.valuePtr = v
	return &s
}

// Theme sets the theme for the selection prompt.
func (s Select) Theme(t *Theme) *Select {
	s.theme = t
	return &s
}

// ClearScreen sets whether to clear the terminal screen before showing the prompt.
func (s Select) ClearScreen(on bool) *Select {
	s.clearSreen = on
	return &s
}

// Run executes the selection prompt and returns the user's choice.
// Returns ErrCanceled if the user cancels the operation.
// Returns an error if no options are provided.
func (s *Select) Run() error {
	if len(s.options) == 0 {
		return errors.New("no options")
	}

	err := runRaw(func(t *terminal) error {
		cursor := 0
		theme := chooseTheme(s.theme)

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom - 1)
		t.removeCursor()
		defer t.bringBack()

		if s.title != "" {
			fmt.Println(s.title)
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

				fmt.Printf("\r%s%s\n", prefix, opt.Text)
			}

			t.helpBar(theme.SelectHelp)

			ev, err := t.kr.ReadEvent()
			if err != nil {
				return err
			}

			switch {
			case ev.IsCtrl('c'), ev.IsCtrl('q'):
				return ErrCanceled
			case ev.Key == key.Up:
				if cursor > 0 {
					cursor--
				}
			case ev.Key == key.Down:
				if cursor < len(s.options)-1 {
					cursor++
				}
			case ev.Key == key.Enter:
				if s.valuePtr != nil {
					selectedValue := s.options[cursor].Value
					ptrValue := reflect.ValueOf(s.valuePtr)
					if ptrValue.Kind() != reflect.Ptr {
						return errors.New("value must be a pointer")
					}
					ptrValue.Elem().Set(reflect.ValueOf(selectedValue))
				}
				return nil
			}

			t.moveCursorUp(len(s.options))
		}
	})

	return err
}

// MultiSelect provides a multiple-selection prompt from a list of options.
type MultiSelect struct {
	title       string
	options     []*Option
	valuePtr    any
	theme       *Theme
	clearScreen bool
}

// NewMultiSelect creates a new multiple-selection prompt.
func NewMultiSelect() *MultiSelect {
	return &MultiSelect{}
}

// Title sets the title text displayed above the options.
func (m MultiSelect) Title(t string) *MultiSelect {
	m.title = t
	return &m
}

// Options sets the available options for selection.
func (m MultiSelect) Options(o []*Option) *MultiSelect {
	m.options = o
	return &m
}

// Value sets a pointer where the selected options' values will be stored.
// Supports []string slices and []any slices of the option values.
func (m MultiSelect) Value(v any) *MultiSelect {
	m.valuePtr = v
	return &m
}

// Theme sets the theme for the multi-selection prompt.
func (m MultiSelect) Theme(t *Theme) *MultiSelect {
	m.theme = t
	return &m
}

// ClearScreen sets whether to clear the terminal screen before showing the prompt.
func (m MultiSelect) ClearScreen(on bool) *MultiSelect {
	m.clearScreen = on
	return &m
}

// Run executes the multi-selection prompt and returns the user's choices.
// Returns ErrCanceled if the user cancels the operation.
// Returns an error if no options are provided.
func (m *MultiSelect) Run() error {
	if len(m.options) == 0 {
		return errors.New("no options")
	}

	err := runRaw(func(t *terminal) error {
		cursor := 0
		theme := chooseTheme(m.theme)

		t.marginTop(theme.MarginTop)
		defer t.marginBottom(theme.MarginBottom - 1)

		t.removeCursor()
		defer t.bringBack()

		if m.title != "" {
			fmt.Println(m.title)
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

				fmt.Printf("\r%s%s %s\n", prefix, mark, opt.Text)
			}

			t.helpBar(theme.MultiSelectHelp)

			ev, err := t.kr.ReadEvent()
			if err != nil {
				return err
			}

			switch {
			case ev.IsCtrl('c'), ev.IsCtrl('q'):
				return ErrCanceled
			case ev.Key == key.Up:
				if cursor > 0 {
					cursor--
				}
			case ev.Key == key.Down:
				if cursor < len(m.options)-1 {
					cursor++
				}
			case ev.Key == key.Space:
				m.options[cursor].selected = !m.options[cursor].selected
			case ev.Key == key.Enter:
				if m.valuePtr != nil {
					ptrValue := reflect.ValueOf(m.valuePtr)
					if ptrValue.Kind() != reflect.Ptr {
						return errors.New("value must be a pointer")
					}

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
							selectedValues := make([]any, 0)

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
			}

			t.moveCursorUp(len(m.options))
		}
	})

	return err
}
