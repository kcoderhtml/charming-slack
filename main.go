package main

// An example Bubble Tea server. This will put an ssh session into alt screen
// and continually print up to date terminal information.

import (
	"context"
	"errors" // Add this line to import the fmt package
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
)

const (
	host = "localhost"
	port = "23234"
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
	http.HandleFunc("/", httpHandlers.HelloWorldHandler)

	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
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
	// save the database
	log.Info("Saving database")
	database.SaveUserData()
}
