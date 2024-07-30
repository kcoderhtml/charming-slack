package httpHandlers

import (
	"net/http"
	"net/url"
	"os"

	"github.com/charmbracelet/log"
	"github.com/slack-go/slack"
)

func SlackInstallHandler(w http.ResponseWriter, r *http.Request, setUserData func(user string, slackToken string, refreshToken string, realName string)) {
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
	token, err := slack.GetOAuthV2Response(client, slackClientID, slackClientSecret, code, os.Getenv("REDIRECT_URL")+"/slack/install")
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

	setUserData(state, token.AuthedUser.AccessToken, token.AuthedUser.RefreshToken, identity.RealName)

	// tell the user they can close this tab now and return to ssh
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("you can close this tab now and return to ssh"))
}
func RedirectToSlackInstallHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "no username provided", http.StatusBadRequest)
		return
	}
	slackClientID := os.Getenv("SLACK_CLIENT_ID")
	log.Info("redirecting to slack install page", "slackClientID", slackClientID)
	http.Redirect(w, r, "https://slack.com/oauth/v2/authorize?scope=&user_scope=channels%3Aread%2Cchannels%3Awrite%2Cchannels%3Ahistory%2Cgroups%3Ahistory%2Cgroups%3Aread%2Cgroups%3Awrite%2Cmpim%3Ahistory%2Cmpim%3Aread%2Cmpim%3Awrite%2Cim%3Ahistory%2Cim%3Aread%2Cim%3Awrite%2Cidentify%2Cchat%3Awrite%2Cusers.profile%3Aread%2Cusers%3Aread&redirect_uri="+url.QueryEscape(os.Getenv("REDIRECT_URL")+"/slack/install")+"&client_id="+slackClientID+"&state="+state, http.StatusFound)
}
