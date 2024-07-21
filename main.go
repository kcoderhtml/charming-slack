package main

// An example Bubble Tea server. This will put an ssh session into alt screen
// and continually print up to date terminal information.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/muesli/termenv"

	"github.com/joho/godotenv"
)

const (
	host = "localhost"
	port = "23234"
)

type keyMap struct {
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
	Enter key.Binding
	Help  key.Binding
	Quit  key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Help}, {k.Quit}}
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "move left"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "move right"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

var style = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#FAFAFA")).
	Background(lipgloss.Color("#7D56F4")).
	Padding(1).
	PaddingLeft(2).
	Border(lipgloss.RoundedBorder())

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Error("Error loading .env file", "error", err)
	}

	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		wish.WithMiddleware(
			myCustomBubbleteaMiddleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Error("Could not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH server", "host", host, "port", port)
	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}

// You can write your own custom bubbletea middleware that wraps tea.Program.
// Make sure you set the program input and output to ssh.Session.
func myCustomBubbleteaMiddleware() wish.Middleware {
	newProg := func(m tea.Model, opts ...tea.ProgramOption) *tea.Program {
		p := tea.NewProgram(m, opts...)
		go func() {
			for {
				<-time.After(1 * time.Second)
				p.Send(timeMsg(time.Now()))
			}
		}()
		return p
	}
	teaHandler := func(s ssh.Session) *tea.Program {
		pty, _, active := s.Pty()
		if !active {
			wish.Fatalln(s, "no active terminal, skipping")
			return nil
		}
		m := model{
			term:                 pty.Term,
			width:                pty.Window.Width,
			height:               pty.Window.Height,
			time:                 time.Now(),
			keys:                 keys,
			help:                 help.New(),
			selectedChannelIndex: 0,
			messageIndex:         0,
			messages:             []string{"Hello World"},
		}
		return newProg(m, append(bubbletea.MakeOptions(s), tea.WithAltScreen())...)
	}
	return bubbletea.MiddlewareWithProgramHandler(teaHandler, termenv.ANSI256)
}

// Just a generic tea.Model to demo terminal information of ssh.
type model struct {
	term                 string
	width                int
	height               int
	time                 time.Time
	keys                 keyMap
	help                 help.Model
	selectedChannelIndex int
	messages             []string
	messageIndex         int
}

type timeMsg time.Time

func (m model) Init() tea.Cmd {
	// set the dummy content to be the conversation history of the first group
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case timeMsg:
		m.time = time.Time(msg)
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.selectedChannelIndex > 0 {
				m.selectedChannelIndex--
			}
		case key.Matches(msg, m.keys.Down):
			if m.selectedChannelIndex < m.height-8 {
				m.selectedChannelIndex++
			}
		case key.Matches(msg, m.keys.Left):
			if m.messageIndex > 0 {
				m.messageIndex--
			}
		case key.Matches(msg, m.keys.Right):
			if m.messageIndex < len(m.messages)-1 {
				m.messageIndex++
			}
		case key.Matches(msg, m.keys.Enter):
			// Retrieve messages from the selected channel
			m.messages = []string{"Hello World"}
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	// Make a list of channels
	var groupsList []string = []string{"Hello World"}

	// clamp the selected channel index
	if m.selectedChannelIndex < 0 {
		m.selectedChannelIndex = 0
	} else if m.selectedChannelIndex >= len(groupsList) {
		m.selectedChannelIndex = len(groupsList) - 1
	}

	// Render the list of groups
	channels := style.Copy().Render(func() string {
		output := ""
		for i, group := range groupsList {
			if i == m.selectedChannelIndex {
				output += "> " + group + "\n"
			} else {
				output += "  " + group + "\n"
			}
		}
		return output
	}())

	conversations := style.Copy().Width(m.width - 26).
		Render(m.messages[0] +
			"\nMessage: " + fmt.Sprint(m.messageIndex) +
			" of " + fmt.Sprint(len(m.messages)))

	helpView := m.help.View(m.keys)

	return lipgloss.JoinVertical(lipgloss.Top, lipgloss.JoinHorizontal(lipgloss.Left, channels, conversations), helpView)
}
