package utils

import (
	"bytes"
	"charming-slack/libs/database"
	"image"
	"net/http"
	"os"
	"regexp"

	"github.com/KononK/resize"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/mattn/go-sixel"
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

var (
	channelStyle = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("#1f7a9b"))
)

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

	channelRe := regexp.MustCompile(`<#(\w+)\|(\w+[-_]*\w*)>`)
	result2 := channelRe.ReplaceAllStringFunc(result, func(match string) string {
		// extract the user ID from the match
		channel := channelRe.FindStringSubmatch(match)[2]
		// get the display name for the user ID from the database
		// return the display name as the replacement

		return channelStyle.Render("#" + channel)
	})

	return result2
}

func UrlParser(s string) string {
	urlRe := regexp.MustCompile(`<(https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*))[|]?([^>]*)>`)
	result := urlRe.ReplaceAllStringFunc(s, func(match string) string {
		// extract the user ID from the match
		submatch := urlRe.FindStringSubmatch(match)

		return "[" + submatch[4] + "]" + "(" + submatch[1] + ")"
	})

	return result
}

func SixelEncode(url string, width uint) string {
	// download the image
	resp, err := http.Get(url)
	if err != nil {
		log.Error("erroring getting image", "err", err)
		return ""
	}
	defer resp.Body.Close()

	// decode the image
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		log.Error("erroring decoding image", "err", err)
		return ""
	}

	// resize image
	m := resize.Resize(width, 0, img, resize.NearestNeighbor)

	// encode the image as sixel and print to stdout
	var buf bytes.Buffer
	sixel.NewEncoder(os.Stdout).Encode(m)
	result := buf.String()

	return result
}
