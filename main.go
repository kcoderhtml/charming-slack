package main

// An example Bubble Tea server. This will put an ssh session into alt screen
// and continually print up to date terminal information.

import (
	"context"
	"encoding/base64"
	"errors" // Add this line to import the fmt package
	"net"
	"net/http"
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

	"github.com/slack-go/slack"
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
	return []key.Binding{k.Help, k.Quit, k.Enter}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Help}, {k.Quit}, {k.Enter}}
}

var keys = keyMap{
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "continue"),
	),
}

var style = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#FAFAFA")).
	Background(lipgloss.Color("#7D56F4")).
	Padding(1).
	PaddingLeft(2).
	PaddingRight(2).
	Border(lipgloss.RoundedBorder())

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Error("Error loading .env file", "error", err)
	}

	http.HandleFunc("/slack/install", slackInstallHandler)
	http.HandleFunc("/install", redirectToSlackInstallHandler)
	http.HandleFunc("/", helloWorldHandler)

	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		wish.WithPublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool {
			return true
		}),
		wish.WithMiddleware(
			firstLineDefenseMiddleware(),
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

	log.Info("Starting HTTP server", "host", "localhost", "port", "23233")
	go func() {
		if err = http.ListenAndServe(":23233", nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Could not start HTTP server", "error", err)
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
	log.Info("Stopping HTTP server")
}

func slackInstallHandler(w http.ResponseWriter, r *http.Request) {
	// get code from query param
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "no code provided", http.StatusBadRequest)
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "no state provided", http.StatusBadRequest)
		return
	}

	// get the client id from the env
	slackClientID := os.Getenv("SLACK_CLIENT_ID")
	if slackClientID == "" {
		http.Error(w, "no slack client id provided", http.StatusBadRequest)
		return
	}

	// get the client secret from the env
	slackClientSecret := os.Getenv("SLACK_CLIENT_SECRET")
	if slackClientSecret == "" {
		http.Error(w, "no slack client secret provided", http.StatusBadRequest)
		return
	}

	// http client to make the request
	client := &http.Client{}

	// get token from slack
	token, err := slack.GetOAuthV2Response(client, slackClientID, slackClientSecret, code, "http://localhost:23233/slack/install")
	if err != nil {
		http.Error(w, "could not get token from slack", http.StatusInternalServerError)
		log.Error("could not get token from slack", "error", err)
		return
	}

	slackClient := slack.New(token.AuthedUser.AccessToken)

	identity, err := slackClient.GetUserInfo(token.AuthedUser.ID)
	if err != nil {
		http.Error(w, "could not get identity from slack", http.StatusInternalServerError)
		log.Error("could not get identity from slack", "error", err)
		return
	}

	users[state] = userData{
		SlackToken:   token.AuthedUser.AccessToken,
		RefreshToken: token.AuthedUser.RefreshToken,
		RealName:     identity.RealName,
	}

	log.Info("adding user", "user", users[state])

	// tell the user they can close this tab now and return to ssh
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("you can close this tab now and return to ssh"))
}
func redirectToSlackInstallHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "no username provided", http.StatusBadRequest)
		return
	}
	slackClientID := os.Getenv("SLACK_CLIENT_ID")
	log.Info("redirecting to slack install page", "slackClientID", slackClientID)
	http.Redirect(w, r, "https://slack.com/oauth/v2/authorize?scope=&user_scope=channels%3Aread%2Cchannels%3Awrite%2Cchannels%3Ahistory%2Cgroups%3Ahistory%2Cgroups%3Aread%2Cgroups%3Awrite%2Cmpim%3Ahistory%2Cmpim%3Aread%2Cmpim%3Awrite%2Cim%3Ahistory%2Cim%3Aread%2Cim%3Awrite%2Cidentify%2Cchat%3Awrite%2Cusers.profile%3Aread%2Cusers%3Aread&redirect_uri=http%3A%2F%2Flocalhost%3A23233%2Fslack%2Finstall&client_id="+slackClientID+"&state="+state, http.StatusFound)
}
func helloWorldHandler(w http.ResponseWriter, r *http.Request) {
	// respond with a 200 OK and "hello world from charming slack :)"
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("hello world from charming slack :)"))
}

// You can write your own custom bubbletea middleware that wraps tea.Program.
// Make sure you set the program input and output to ssh.Session.
func firstLineDefenseMiddleware() wish.Middleware {
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

		page := "auth"

		if _, ok := users[s.User()]; ok {
			log.Info("existing user")
			// check the key is the one we expect
			if s.PublicKey() != nil {
				for user, userData := range users {
					parsed, _, _, _, _ := ssh.ParseAuthorizedKey(
						[]byte(userData.PublicKey),
					)
					if ssh.KeysEqual(s.PublicKey(), parsed) && user == s.User() {
						if userData.SlackToken != "" {
							page = "home"
							log.Info("authorized by public key and slack integration is installed")
						} else {
							page = "slackOnboarding"
							log.Info("authorized by public key")
							log.Info("needs to install slack integration (redirecting to slack onboarding page)")
						}
					} else {
						log.Info("not authorized by public key (redirecting to auth page)")
					}
				}
			}
		} else {
			log.Info("new user")
		}

		m := model{
			term:      pty.Term,
			width:     pty.Window.Width,
			height:    pty.Window.Height,
			time:      time.Now(),
			keys:      keys,
			help:      help.New(),
			user:      s.User(),
			publicKey: s.PublicKey(),
			page:      page,
		}
		return newProg(m, append(bubbletea.MakeOptions(s), tea.WithAltScreen())...)
	}
	return bubbletea.MiddlewareWithProgramHandler(teaHandler, termenv.ANSI256)
}

// Just a generic tea.Model to demo terminal information of ssh.
type model struct {
	term      string
	width     int
	height    int
	time      time.Time
	keys      keyMap
	help      help.Model
	page      string
	user      string
	publicKey ssh.PublicKey
}

type timeMsg time.Time

// a variable holding all the users and their public keys and slack tokens and refresh token and real name
var users = map[string]userData{
	// You can add add your name and public key here :)
}

type userData struct {
	PublicKey    string
	SlackToken   string
	RefreshToken string
	RealName     string
}

func (m model) Init() tea.Cmd {
	// check if the user is an existing user
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
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Enter):
			if m.page == "auth" {
				// add the user and their public key to the map
				// parse the public key
				parsed := m.publicKey.Type() + " " + base64.StdEncoding.EncodeToString(m.publicKey.Marshal())
				users[m.user] = userData{
					PublicKey: string(parsed),
				}
				log.Info("added user", "user", m.user, "with public key", parsed[:20])
				m.page = "slackOnboarding"
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	fittedStyle := style.Copy().
		Width(m.width - 2).
		Height(m.height - 3)

	content := ""

	switch m.page {
	case "home":
		content = m.homeView(fittedStyle)
	case "auth":
		content = m.authView(fittedStyle)
	case "slackOnboarding":
		content = m.slackOnboardingView(fittedStyle)
	default:
		content = "unknown page"
	}

	return lipgloss.JoinVertical(lipgloss.Center, content, m.help.View(m.keys))
}

func (m model) homeView(fittedStyle lipgloss.Style) string {
	content := fittedStyle.
		Align(lipgloss.Center, lipgloss.Center).
		Render("ello world")

	return content
}

func (m model) authView(fittedStyle lipgloss.Style) string {
	content := fittedStyle.Copy().
		Align(lipgloss.Center, lipgloss.Center).
		Render("Welcome " + m.user +
			"\n" +
			"Public key type: " + string(m.publicKey.Type()) +
			"\n\n" +
			"Welcome to charming slack! :)" +
			"\n" +
			"To get started with your new account lets sign you into slack!" +
			"\n" +
			"(Hit enter to continue)")

	return content
}

func (m model) slackOnboardingView(fittedStyle lipgloss.Style) string {
	content := fittedStyle.Copy().
		Align(lipgloss.Center, lipgloss.Center).
		Render("Click the link below to oauth your slack account with CS!" +
			"\n\n" +
			"http://localhost:23233/install?state=" + m.user)

	return content
}
