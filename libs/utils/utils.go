package utils

import (
	"charming-slack/libs/database"
	"regexp"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/slack-go/slack"
)

func ClampString(s string, max int) string {
	// convert the string to a slice of runes
	runes := []rune(s)

	// if the length of the string is greater than the max, return the first max runes
	if len(runes) > max {
		return string(runes[:max])
	}

	// if the length of the string is less than the max, return the string
	return s
}

func UserIdParser(s string, highlightedStyle lipgloss.Style, highlightedStyleBot lipgloss.Style, slackClient slack.Client) string {
	// look for things like <@U05JX2BHANT> and replace them with the proper display name
	re := regexp.MustCompile(`<@(U\w+)>`)
	result := re.ReplaceAllStringFunc(s, func(match string) string {
		// extract the user ID from the match
		userID := re.FindStringSubmatch(match)[1]
		// get the display name for the user ID from the database
		// return the display name as the replacement

		user := database.GetUserOrCreate(userID, slackClient)
		log.Info("names", "display name", user.DisplayName, "real name", user.RealName)
		if user.DisplayName == "" {
			return highlightedStyleBot.Render("@" + user.RealName + " (bot)")
		} else {
			return highlightedStyle.Render("@" + user.DisplayName)
		}
	})

	return result
}
