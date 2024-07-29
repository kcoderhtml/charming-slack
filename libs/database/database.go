package database

import (
	"encoding/json"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/slack-go/slack"
)

// a variable holding all the users and their public keys and slack tokens and refresh token and real name
var DB = Database{
	ApplicationData: map[string]UserData{},
	SlackMap:        map[string]SlackUserMap{},
}

type UserData struct {
	PublicKey    string
	SlackToken   string
	RefreshToken string
	RealName     string
}

type SlackUserMap struct {
	RealName    string
	DisplayName string
}

type Database struct {
	ApplicationData map[string]UserData
	SlackMap        map[string]SlackUserMap
}

func SetUserData(user string, slackToken string, refreshToken string, realName string) {
	DB.ApplicationData[user] = UserData{
		PublicKey:    DB.ApplicationData[user].PublicKey,
		SlackToken:   slackToken,
		RefreshToken: refreshToken,
		RealName:     realName,
	}
}

func QuerySlackUserID(userid string) SlackUserMap {
	return DB.SlackMap[userid]
}

func AddSlackUser(userid string, realName string, displayName string) {
	DB.SlackMap[userid] = SlackUserMap{
		RealName:    realName,
		DisplayName: displayName,
	}
}

func GetUserOrCreate(userid string, slackClient slack.Client) SlackUserMap {
	// check if the user exists in the map first
	user, ok := DB.SlackMap[userid]

	if !ok {
		identity, err := slackClient.GetUserInfo(userid)
		if err != nil {
			log.Error("error getting dm name", "err", err)
			// wait 2 seconds before trying again
			time.Sleep(4 * time.Second)
			identity, err = slackClient.GetUserInfo(userid)
			if err != nil {
				log.Error("error getting dm name", "err", err)
				return SlackUserMap{
					RealName:    "unknown",
					DisplayName: "unknown",
				}
			}
		}

		user = SlackUserMap{
			RealName:    identity.Profile.RealNameNormalized,
			DisplayName: identity.Profile.DisplayNameNormalized,
		}
	}

	DB.SlackMap[userid] = user

	return user
}

func SaveUserData() {
	// save the database to a file, if it doesn't exist, create it
	jsonData, err := json.Marshal(DB)
	if err != nil {
		log.Error("Could not marshal users data to JSON", "error", err)
		return
	}

	err = os.WriteFile("./.ssh/database.json", jsonData, 0644)
	if err != nil {
		log.Error("Could not create database file", "error", err)
	}
}

func LoadUserData() {
	// load the database from a file, if it doesn't exist, create it
	jsonData, err := os.ReadFile("./.ssh/database.json")
	if err != nil {
		log.Error("Could not read database file", "error", err)
		return
	}

	err = json.Unmarshal(jsonData, &DB)
	if err != nil {
		log.Error("Could not unmarshal users data from JSON", "error", err)
		return
	}
}
