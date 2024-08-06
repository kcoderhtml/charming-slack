package main

// An example Bubble Tea server. This will put an ssh session into alt screen
// and continually print up to date terminal information.

import (
	"context"
	"errors" // Add this line to import the fmt package
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"

	"github.com/joho/godotenv"

	"charming-slack/libs/bubbleViews"
	"charming-slack/libs/database"
	"charming-slack/libs/httpHandlers"
	"charming-slack/libs/utils"
)

const (
	host = "0.0.0.0"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Error("Error loading .env file", "error", err)
	}
	// load the database
	log.Info("Loading database")
	database.LoadUserData()

	http.HandleFunc("/slack/install", func(w http.ResponseWriter, r *http.Request) {
		httpHandlers.SlackInstallHandler(w, r, database.SetUserData)
	})
	http.HandleFunc("/install", httpHandlers.RedirectToSlackInstallHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://github.com/kcoderhtml/charming-slack", http.StatusFound)
	})

	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, os.Getenv("SSH_PORT"))),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		wish.WithPublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool {
			return true
		}),
		wish.WithMiddleware(
			bubbleViews.FirstLineDefenseMiddleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Error("Could not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH server", "host", host, "port", os.Getenv("SSH_PORT"))
	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	log.Info("Starting HTTP server", "host", host, "port", os.Getenv("HTTP_PORT"))
	go func() {
		if err = http.ListenAndServe(":"+os.Getenv("HTTP_PORT"), nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Could not start HTTP server", "error", err)
			done <- nil
		}
	}()

	fmt.Println(utils.SixelEncode("https://emoji.slack-edge.com/T0266FRGM/blob_thumbs_up/1ef9fba2c56e12aa.png", 36))

	<-done
	log.Info("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
	log.Info("Stopping HTTP server")
	// save the database
	log.Info("Saving database")
	database.SaveUserData()
}
