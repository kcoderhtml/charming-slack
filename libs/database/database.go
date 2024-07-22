package database

import (
	"encoding/json"
	"os"

	"github.com/charmbracelet/log"
)

// a variable holding all the users and their public keys and slack tokens and refresh token and real name
var Users = map[string]UserData{
	// You can add add your name and public key here :)
}

type UserData struct {
	PublicKey    string
	SlackToken   string
	RefreshToken string
	RealName     string
}

func SetUserData(user string, slackToken string, refreshToken string, realName string) {
	Users[user] = UserData{
		PublicKey:    Users[user].PublicKey,
		SlackToken:   slackToken,
		RefreshToken: refreshToken,
		RealName:     realName,
	}
}

func SaveUserData() {
	// save the database to a file, if it doesn't exist, create it
	jsonData, err := json.Marshal(Users)
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

	err = json.Unmarshal(jsonData, &Users)
	if err != nil {
		log.Error("Could not unmarshal users data from JSON", "error", err)
		return
	}
}
