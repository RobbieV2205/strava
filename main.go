/*
Strava Sync
-----------

Creates database and user (if needed) and syncs all runs to the database.
*/
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/natefinch/lumberjack.v2"

	"strava-collector/internal/database"
	"strava-collector/internal/strava"
)

const (
	envPath             = ".env"
	syncIntervalSeconds = 6 * 60 * 60
)

func setupLogging() {
	logFile := &lumberjack.Logger{
		Filename:   "strava_sync.log",
		MaxSize:    5, // megabytes
		MaxBackups: 3,
	}
	handler := slog.NewTextHandler(io.MultiWriter(os.Stdout, logFile), &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02 15:04:05"))
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

// setEnvKey updates a key-value pair in the .env file.
func setEnvKey(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// ensureEnv checks that the .env file exists and contains all required variables.
func ensureEnv() {
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		slog.Error(".env file not found.")
		os.Exit(1)
	}
	if err := godotenv.Overload(envPath); err != nil {
		slog.Error("Failed to load .env", "error", err)
		os.Exit(1)
	}
	if os.Getenv("STRAVA_CLIENT_ID") == "" || os.Getenv("STRAVA_CLIENT_SECRET") == "" {
		slog.Error("STRAVA_CLIENT_ID or STRAVA_CLIENT_SECRET is missing in .env")
		os.Exit(1)
	}
	if os.Getenv("MYSQL_ROOT_PASSWORD") == "" {
		slog.Error("MYSQL_ROOT_PASSWORD missing in .env")
		os.Exit(1)
	}
	if os.Getenv("MYSQL_PASSWORD") == "" {
		slog.Error("MYSQL_PASSWORD missing in .env")
		os.Exit(1)
	}
}

// ensureDatabase ensures the database is available, setting it up if necessary.
func ensureDatabase() {
	alreadyInitialized := strings.ToLower(os.Getenv("DB_INITIALIZED")) == "true"

	db, err := database.Connect()
	if err != nil {
		if alreadyInitialized {
			slog.Error("Database not available after creation please check database.", "error", err)
			os.Exit(1)
		}
		if err := database.SetupDatabase(); err != nil {
			slog.Error("Failed to setup database", "error", err)
			os.Exit(1)
		}
		slog.Info("Database created.")
		if err := setEnvKey(envPath, "DB_INITIALIZED", "true"); err != nil {
			slog.Error("Failed to update .env", "error", err)
			os.Exit(1)
		}
		if err := godotenv.Overload(envPath); err != nil {
			slog.Error("Failed to reload .env", "error", err)
			os.Exit(1)
		}
		return
	}
	db.Close()
}

// syncOnce performs a single sync cycle: fetches all runs and upserts them into the database.
func syncOnce() error {
	if err := godotenv.Overload(envPath); err != nil {
		return fmt.Errorf("failed to reload .env: %w", err)
	}

	token, err := strava.GetAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	db, err := database.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	runs, err := strava.FetchAllRuns(token)
	if err != nil {
		return fmt.Errorf("failed to fetch runs: %w", err)
	}

	if len(runs) == 0 {
		slog.Info("No runs found.")
		return nil
	}

	if err := database.UpsertRuns(db, runs); err != nil {
		return fmt.Errorf("failed to upsert runs: %w", err)
	}
	return nil
}

func main() {
	setupLogging()
	ensureEnv()
	ensureDatabase()

	slog.Info(strings.Repeat("=", 60))
	slog.Info(fmt.Sprintf("Server started — sync each %d uur. Logfile: strava_sync.log", syncIntervalSeconds/3600))
	slog.Info(strings.Repeat("=", 60))

	cycle := 0
	for {
		cycle++
		slog.Info(fmt.Sprintf("--- Loop #%d started at %s ---", cycle, time.Now().Format("2006-01-02 15:04:05")))
		if err := syncOnce(); err != nil {
			slog.Error("Sync failed", "error", err)
		}
		time.Sleep(syncIntervalSeconds * time.Second)
	}
}
