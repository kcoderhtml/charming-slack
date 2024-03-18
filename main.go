package main

// An example Bubble Tea server. This will put an ssh session into alt screen
// and continually print up to date terminal information.

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"strings"
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
	"github.com/slack-go/slack"

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
	Tab   key.Binding
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
	return [][]key.Binding{{k.Tab}, {k.Help}, {k.Quit}}
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
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "focus next pane"),
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

var slackApi *slack.Client
var slackUserApi *slack.Client

var groups []slack.Channel

var messages []slack.Message

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Error("Error loading .env file", "error", err)
	}

	botApi := slack.New(os.Getenv("SLACK_TOKEN"))
	userApi := slack.New(os.Getenv("SLACK_OAUTH_TOKEN"))
	slackApi = botApi
	slackUserApi = userApi

	// get a slack client from .env
	// Retrieve user groups
	ListOfGroups, _, err := slackApi.GetConversationsForUser(&slack.GetConversationsForUserParameters{UserID: "U062UG485EE"})
	if err != nil {
		log.Error("Failed to retrieve user groups", "error", err)
		return
	}

	// Assign retrieved user groups to the global variable
	groups = ListOfGroups

	// Retrieve messages from the first group
	ListOfMessages, err := slackUserApi.GetConversationHistory(&slack.GetConversationHistoryParameters{ChannelID: groups[0].ID})

	if err != nil {
		log.Error("Failed to retrieve messages", "error", err)
		return
	}

	// Assign retrieved messages to the global variable
	messages = ListOfMessages.Messages

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
			term:   pty.Term,
			width:  pty.Window.Width,
			height: pty.Window.Height,
			time:   time.Now(),
			keys:   keys,
			help:   help.New(),
		}
		return newProg(m, append(bubbletea.MakeOptions(s), tea.WithAltScreen())...)
	}
	return bubbletea.MiddlewareWithProgramHandler(teaHandler, termenv.ANSI256)
}

// Just a generic tea.Model to demo terminal information of ssh.
type model struct {
	term        string
	width       int
	height      int
	time        time.Time
	focusedPane int
	keys        keyMap
	help        help.Model
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
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Tab):
			m.focusedPane = (m.focusedPane + 1) % 2
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	// Make a list of groups
	var groupsList []string

	for _, group := range groups {
		// break if the list is longer than the line count of the terminal
		if len(groupsList) > m.height-7 {
			groupsList = append(groupsList, "...")
			break
		}

		groupsList = append(groupsList, group.Name)
	}

	// Render the list of groups
	channels := style.Copy().Bold(m.focusedPane == 0).Width(20).Render(strings.Join(groupsList, "\n"))
	conversations := style.Copy().Bold(m.focusedPane == 1).Width(m.width - 26).Render(messages[0].Text)
	helpView := m.help.View(m.keys)

	return lipgloss.JoinVertical(lipgloss.Top, lipgloss.JoinHorizontal(lipgloss.Left, channels, conversations), helpView)
}
