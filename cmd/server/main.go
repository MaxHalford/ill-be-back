package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/maxhalford/ill-be-back/internal/app"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	dev := flag.Bool("dev", false, "allow local HTTP and create a local encryption key")
	flag.Parse()
	appURL := strings.TrimRight(os.Getenv("APP_URL"), "/")
	if appURL == "" && *dev {
		appURL = "http://localhost:8000"
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if !*dev {
			return errors.New("set DATABASE_URL to PostgreSQL or a persistent SQLite file")
		}
		if err := os.MkdirAll("data", 0700); err != nil {
			return err
		}
		dsn = "data/app.db"
	}
	key, err := encryptionKey(*dev)
	if err != nil {
		return err
	}
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "frontend/dist"
	}
	if _, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil {
		return errors.New("build the frontend first: cd frontend && npm ci && npm run build")
	}
	a, err := app.New(app.Config{DatabaseURL: dsn, AppURL: appURL, StaticDir: staticDir, EncryptionKey: key, Development: *dev,
		GitHubClientID: os.Getenv("GITHUB_CLIENT_ID"), GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"), GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		MicrosoftClientID: os.Getenv("MICROSOFT_CLIENT_ID"), MicrosoftClientSecret: os.Getenv("MICROSOFT_CLIENT_SECRET"),
		GoogleVerified: os.Getenv("GOOGLE_VERIFIED") == "true",
		SlackClientID:  os.Getenv("SLACK_CLIENT_ID"), SlackClientSecret: os.Getenv("SLACK_CLIENT_SECRET")})
	if err != nil {
		return err
	}
	defer a.Close()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	server := &http.Server{Addr: ":" + port, Handler: a, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { slog.Info("I’ll Be Back is ready", "url", appURL); errs <- server.ListenAndServe() }()
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func encryptionKey(dev bool) ([]byte, error) {
	if encoded := os.Getenv("TOKEN_ENCRYPTION_KEY"); encoded != "" {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return nil, errors.New("TOKEN_ENCRYPTION_KEY must be base64-encoded 32 random bytes")
		}
		return key, nil
	}
	if !dev {
		return nil, errors.New("set TOKEN_ENCRYPTION_KEY; generate with openssl rand -base64 32")
	}
	if err := os.MkdirAll("data", 0700); err != nil {
		return nil, err
	}
	path := "data/token.key"
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("invalid local encryption key")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err = f.Write(key); err != nil {
		return nil, err
	}
	return key, nil
}
