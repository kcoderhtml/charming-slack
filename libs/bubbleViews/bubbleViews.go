package bubbleViews

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/muesli/termenv"
	"github.com/slack-go/slack"

	"charming-slack/libs/database"
	"charming-slack/libs/keymaps"
	"charming-slack/libs/utils"
)

var style = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#FAFAFA")).
	Background(lipgloss.Color("#7D56F4")).
	Padding(1).
	PaddingLeft(2).
	PaddingRight(2).
	Border(lipgloss.RoundedBorder())

var highlightedStyle = lipgloss.NewStyle().
	Bold(true).Foreground(lipgloss.Color("#4e2ed1"))

type tab struct {
	title        string
	content      func(lipgloss.Style, Model) string
	messages     []slack.Message
	state        string
	messagePager viewport.Model
}

type Model struct {
	term               string
	width              int
	height             int
	time               time.Time
	keys               keymaps.KeyMap
	help               help.Model
	page               string
	tabs               []tab
	channelList        list.Model
	privateChannelList list.Model
	dmList             list.Model
	channels           []slack.Channel
	privateChannels    []slack.Channel
	dms                []slack.Channel
	user               string
	publicKey          ssh.PublicKey
	activeTab          int
	slackClient        *slack.Client
}

type timeMsg time.Time

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
)

type item string

func (i item) FilterValue() string { return string(i) }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

// You can write your own custom bubbletea middleware that wraps tea.Program.
// Make sure you set the program input and output to ssh.Session.
func FirstLineDefenseMiddleware() wish.Middleware {
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

		if _, ok := database.Users[s.User()]; ok {
			log.Info("existing user")
			// check the key is the one we expect
			if s.PublicKey() != nil {
				for user, userData := range database.Users {
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

		channels := []list.Item{
			item("plz wait while i'm 		loading..."),
		}
		l := list.New(channels, itemDelegate{}, 24, 14)
		l.Title = "Public Channels"
		l.SetShowStatusBar(false)
		l.SetShowHelp(false)
		l.SetFilteringEnabled(false)
		l.Styles.Title = titleStyle
		l.Styles.PaginationStyle = paginationStyle
		l.Styles.HelpStyle = helpStyle

		privateChannelL := l
		privateChannelL.Title = "Private Channels"

		dmL := l
		dmL.Title = "DMs"

		p := viewport.New(pty.Window.Width-4, pty.Window.Height-6)
		p.Style = p.Style.Border(lipgloss.RoundedBorder()).
			BorderTop(false).BorderForeground(lipgloss.Color("#7D56F3")).
			PaddingTop(0).PaddingLeft(2).PaddingRight(2)

		m := Model{
			term:               pty.Term,
			width:              pty.Window.Width,
			height:             pty.Window.Height,
			time:               time.Now(),
			keys:               keymaps.Keys,
			help:               help.New(),
			user:               s.User(),
			publicKey:          s.PublicKey(),
			page:               page,
			tabs:               []tab{{"Public Channels", publicChannelsView, []slack.Message{}, "select", p}, {"Private Channels", privateChannelsView, []slack.Message{}, "select", p}, {"DMs", directMessagesView, []slack.Message{}, "select", p}, {"Search", searchView, []slack.Message{}, "select", p}},
			channelList:        l,
			privateChannelList: privateChannelL,
			dmList:             dmL,
			activeTab:          0,
			slackClient:        slack.New(database.Users[s.User()].SlackToken),
		}

		return newProg(m, append(bubbletea.MakeOptions(s), tea.WithAltScreen())...)
	}
	return bubbletea.MiddlewareWithProgramHandler(teaHandler, termenv.ANSI256)
}

type channelUpdateMessage struct{ channels []slack.Channel }
type privateChannelUpdateMessage struct{ channels []slack.Channel }
type dmUpdateMessage struct {
	items []list.Item
	dms   []slack.Channel
}

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

func getChannels(slackClient *slack.Client) tea.Cmd {
	return func() tea.Msg {
		// get the channels
		channels, _, err := slackClient.GetConversationsForUser(&slack.GetConversationsForUserParameters{Limit: 10000, ExcludeArchived: true})
		if err != nil {
			return errMsg{err}
		}

		return channelUpdateMessage{channels}
	}
}

func getPrivateChannels(slackClient *slack.Client) tea.Cmd {
	return func() tea.Msg {
		// get the channels
		channels, _, err := slackClient.GetConversationsForUser(&slack.GetConversationsForUserParameters{Limit: 10000, ExcludeArchived: true, Types: []string{"private_channel"}})
		if err != nil {
			return errMsg{err}
		}

		return privateChannelUpdateMessage{channels}
	}
}

func getDms(slackClient *slack.Client) tea.Cmd {
	return func() tea.Msg {
		// get the channels
		dms, _, err := slackClient.GetConversationsForUser(&slack.GetConversationsForUserParameters{Limit: 10000, ExcludeArchived: true, Types: []string{"im"}})
		if err != nil {
			return errMsg{err}
		}

		items := []list.Item{}
		for _, dm := range dms {
			// name := "unknown"
			// if dm.NameNormalized == "" {
			// 	// get the user's display name
			// 	identity, err := slackClient.GetUserInfo(dm.User)
			// 	if err != nil {
			// 		log.Error("error getting dm name", "err", err)
			// 		// wait 2 seconds before trying again
			// 		time.Sleep(2 * time.Second)
			// 		identity, err = slackClient.GetUserInfo(dm.User)
			// 		if err != nil {
			// 			log.Error("error getting dm name", "err", err)
			// 			continue
			// 		}
			// 	}

			// 	if identity.Profile.DisplayName != "" {
			// 		name = identity.Profile.DisplayName
			// 	} else {
			// 		if identity.Profile.RealName != "" {
			// 			name = identity.Profile.RealName
			// 		} else {
			// 			name = "unknown"
			// 		}
			// 	}
			// }

			items = append(items, item(dm.Name))
		}

		log.Info("got dms", "#dm", len(items))

		return dmUpdateMessage{items, dms}
	}
}

type tabMessageUpdate struct {
	messages []slack.Message
	tab      int
	channel  string
}

func getMessages(slackClient *slack.Client, channel string, tab int) tea.Cmd {
	return func() tea.Msg {
		messages, err := slackClient.GetConversationHistory(&slack.GetConversationHistoryParameters{ChannelID: channel, Limit: 100})
		if err != nil {
			log.Error("error fetching messages", "err", err)

			return errMsg{err}
		}

		return tabMessageUpdate{messages: messages.Messages, tab: tab, channel: channel}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(getChannels(m.slackClient), getPrivateChannels(m.slackClient), getDms(m.slackClient))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case time.Time:
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
			// page specific logic
			switch {
			case m.page == "auth":
				// add the user and their public key to the map
				// parse the public key
				parsed := m.publicKey.Type() + " " + base64.StdEncoding.EncodeToString(m.publicKey.Marshal())
				database.Users[m.user] = database.UserData{
					PublicKey: string(parsed),
				}
				log.Info("added user", "user", m.user, "with public key", parsed[:20])
				m.page = "slackOnboarding"
			case m.page == "slackOnboarding":
				// check if the user has a slack token
				// if they do, redirect to home
				if database.Users[m.user].SlackToken != "" {
					m.slackClient = slack.New(database.Users[m.user].SlackToken)
					m.page = "home"
				}
			case m.page == "home":
				// redirect to slack page
				m.page = "slack"
			case m.page == "slack":
				// check what page we are on
				switch m.activeTab {
				case 0:
					// switch tab state to messages and run the get messages command
					m.tabs[m.activeTab].state = "messages"
					cmds = append(cmds, getMessages(m.slackClient, m.channels[m.channelList.Index()].ID, m.activeTab))
				case 1:
					// switch tab state to messages and run the get messages command
					m.tabs[m.activeTab].state = "messages"
					cmds = append(cmds, getMessages(m.slackClient, m.privateChannels[m.privateChannelList.Index()].ID, m.activeTab))
				case 2:
					// switch tab state to messages and run the get messages command
					m.tabs[m.activeTab].state = "messages"
					cmds = append(cmds, getMessages(m.slackClient, m.dms[m.dmList.Index()].ID, m.activeTab))
				}
			}
		case key.Matches(msg, m.keys.ShiftEnter):
			if m.page == "slack" {
				m.tabs[m.activeTab].state = "select"
				log.Info("set state to select")
			}
		case key.Matches(msg, m.keys.Tab):
			if m.page == "slack" {
				m.activeTab = (m.activeTab + 1) % len(m.tabs)
			}
		case key.Matches(msg, m.keys.ShiftTab):
			if m.page == "slack" {
				m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			}
		}
	case channelUpdateMessage:
		m.channels = msg.channels
		items := []list.Item{}
		for _, channel := range m.channels {
			items = append(items, item(channel.Name))
		}
		m.channelList.SetItems(items)
	case privateChannelUpdateMessage:
		m.privateChannels = msg.channels
		items := []list.Item{}
		for _, channel := range m.privateChannels {
			items = append(items, item(channel.Name))
		}
		m.privateChannelList.SetItems(items)
	case dmUpdateMessage:
		m.dms = msg.dms
		m.dmList.SetItems(msg.items)
	case tabMessageUpdate:
		m.tabs[msg.tab].messages = msg.messages
		// message content
		var b strings.Builder
		for _, message := range msg.messages {
			b.WriteString("---\n\n" + highlightedStyle.Render(message.Username) + " - " + message.Text + "\n\n---\n\n")
		}
		m.tabs[msg.tab].messagePager.SetContent(b.String())
	}

	// check which tab the user is on
	switch m.activeTab {
	case 0:
		switch m.tabs[0].state {
		case "select":
			channelList, channelCmd := m.channelList.Update(msg)
			m.channelList = channelList
			cmds = append(cmds, channelCmd)
		case "messages":
			messagePager, messageCommand := m.tabs[0].messagePager.Update(msg)
			m.tabs[0].messagePager = messagePager
			cmds = append(cmds, messageCommand)
		}
	case 1:
		switch m.tabs[1].state {
		case "select":
			mpimList, mpimCmd := m.privateChannelList.Update(msg)
			m.privateChannelList = mpimList
			cmds = append(cmds, mpimCmd)
		case "messages":
			messagePager, messageCommand := m.tabs[1].messagePager.Update(msg)
			m.tabs[1].messagePager = messagePager
			cmds = append(cmds, messageCommand)
		}
	case 2:
		switch m.tabs[2].state {
		case "select":
			imList, imCmd := m.dmList.Update(msg)
			m.dmList = imList
			cmds = append(cmds, imCmd)
		case "messages":
			messagePager, messageCommand := m.tabs[2].messagePager.Update(msg)
			m.tabs[2].messagePager = messagePager
			cmds = append(cmds, messageCommand)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	fittedStyle := style.Copy().
		Width(m.width - 2).
		Height(m.height - 3)

	content := ""

	switch m.page {
	case "home":
		content = m.HomeView(fittedStyle)
	case "slack":
		content = SlackView(fittedStyle, m)
	case "auth":
		content = m.AuthView(fittedStyle)
	case "slackOnboarding":
		content = m.SlackOnboardingView(fittedStyle)
	default:
		content = "unknown page"
	}

	return lipgloss.JoinVertical(lipgloss.Center, content, m.help.View(m.keys))
}

func (m Model) HomeView(fittedStyle lipgloss.Style) string {
	content := fittedStyle.
		Align(lipgloss.Center, lipgloss.Center).
		Render("ello world!!!" + "\n\n" + database.Users[m.user].RealName + " welcome to charming slack! :)")

	return content
}

func SlackView(fittedStyle lipgloss.Style, m Model) string {
	doc := strings.Builder{}

	var renderedTabs []string

	for i, t := range m.tabs {
		var style lipgloss.Style
		isFirst, isLast, isActive := i == 0, i == len(m.tabs)-1, i == m.activeTab
		if isActive {
			style = activeTabStyle.Copy()
		} else {
			style = inactiveTabStyle.Copy()
		}
		border, _, _, _, _ := style.GetBorder()

		if isFirst && isActive {
			border.BottomLeft = "┘"
		} else if isFirst && !isActive {
			border.BottomLeft = "┴"
		} else if isLast && isActive {
			border.BottomRight = "└"
		} else if isLast && !isActive {
			border.BottomRight = "┴"
		}

		style = style.Border(border, true)

		renderedTabs = append(renderedTabs, style.Render(t.title))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	borderLength := (m.width - lipgloss.Width(row) - 8)
	borderLengthLeft := borderLength / 2
	borderLengthRight := borderLength / 2
	if (m.width-lipgloss.Width(row)-8)%2 != 0 {
		borderLengthRight++
	}
	// add a tab with just a bottom border to the left and right of the row
	borderBottomLeftStyle := lipgloss.NewStyle().Border(tabBorderWithBottom("┌", "─", "─", true), true).BorderForeground(highlightColor).Width(borderLengthLeft)
	renderedTabs = append([]string{borderBottomLeftStyle.Render("")}, renderedTabs...)
	borderBottomRightStyle := lipgloss.NewStyle().Border(tabBorderWithBottom("─", "─", "┐", true), true).BorderForeground(highlightColor).Width(borderLengthRight)
	renderedTabs = append(renderedTabs, borderBottomRightStyle.Render(""))
	row = lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	if lipgloss.Width(row)+4 > m.width {
		// return a too small window message
		paddingStyle := lipgloss.NewStyle().
			Padding(0, 2)

		content := fittedStyle.
			Width(m.width-6).
			Height(m.height-3).
			Background(lipgloss.NoColor{}).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Window too small")
		return paddingStyle.Render(content)
	}
	doc.WriteString(row)
	doc.WriteString("\n")

	windowStyle := windowStyle.
		Width(m.width - 6).
		Height(m.height - lipgloss.Height(row) - 3)

	doc.WriteString(m.tabs[m.activeTab].content(windowStyle, m))
	return docStyle.Render(doc.String())
}

func (m Model) AuthView(fittedStyle lipgloss.Style) string {
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

func (m Model) SlackOnboardingView(fittedStyle lipgloss.Style) string {
	content := fittedStyle.Copy().
		Align(lipgloss.Center, lipgloss.Center).
		Render("Click the link below to oauth your slack account with CS!" +
			"\n\n" +
			"http://localhost:23233/install?state=" + m.user)

	return content
}

func publicChannelsView(style lipgloss.Style, m Model) string {
	switch m.tabs[m.activeTab].state {
	case "select":
		m.channelList.SetHeight(style.GetHeight() - 4)
		m.channelList.SetWidth(style.GetWidth() - 8)

		items := []list.Item{}
		for _, channel := range m.channelList.Items() {
			items = append(items, item(utils.ClampString(fmt.Sprintf("%v", channel), m.channelList.Width())))
		}
		m.channelList.SetItems(items)

		return style.Render(m.channelList.View())
	case "messages":
		return m.tabs[0].messagePager.View()
	}

	return ""
}

func privateChannelsView(style lipgloss.Style, m Model) string {
	switch m.tabs[m.activeTab].state {
	case "select":
		m.privateChannelList.SetHeight(style.GetHeight() - 4)
		m.privateChannelList.SetWidth(style.GetWidth() - 8)

		items := []list.Item{}
		for _, channel := range m.privateChannelList.Items() {
			items = append(items, item(utils.ClampString(fmt.Sprintf("%v", channel), m.privateChannelList.Width())))
		}
		m.privateChannelList.SetItems(items)

		return style.Render(m.privateChannelList.View())
	case "messages":
		return m.tabs[1].messagePager.View()
	}

	return ""
}

func directMessagesView(style lipgloss.Style, m Model) string {
	switch m.tabs[m.activeTab].state {
	case "select":
		m.dmList.SetHeight(style.GetHeight() - 4)
		m.dmList.SetWidth(style.GetWidth() - 8)

		items := []list.Item{}
		for _, dm := range m.dmList.Items() {
			items = append(items, item(utils.ClampString(fmt.Sprintf("%v", dm), m.dmList.Width())))
		}
		m.dmList.SetItems(items)

		return style.Render(m.dmList.View())
	case "messages":
		return m.tabs[0].messagePager.View()
	}

	return ""
}

func searchView(style lipgloss.Style, m Model) string {
	return style.Render("search")
}

func tabBorderWithBottom(left, middle, right string, noTop bool) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	if noTop {
		border.TopLeft = ""
		border.Top = ""
		border.TopRight = ""
		border.Left = ""
		border.Right = ""
	}
	return border
}

var (
	docStyle         = lipgloss.NewStyle().Padding(0, 2, 1, 2)
	highlightColor   = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	inactiveTabStyle = lipgloss.NewStyle().Border(tabBorderWithBottom("┴", "─", "┴", false), true).BorderForeground(highlightColor).Padding(0, 1)
	activeTabStyle   = inactiveTabStyle.Copy().Border(tabBorderWithBottom("┘", " ", "└", false), true)
	windowStyle      = lipgloss.NewStyle().BorderForeground(highlightColor).Padding(2, 0).Align(lipgloss.Center).Border(lipgloss.NormalBorder()).UnsetBorderTop()
)
