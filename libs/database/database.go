package database

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
