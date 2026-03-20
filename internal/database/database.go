/*
database.go
-----------

File with all the functions used to setup, read and write the database.
*/
package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS runs (
    id                       BIGINT PRIMARY KEY,
    name                     VARCHAR(255),
    sport_type               VARCHAR(50),
    distance                 FLOAT COMMENT 'meters',
    moving_time              INT   COMMENT 'seconds',
    elapsed_time             INT   COMMENT 'seconds',
    total_elevation_gain     FLOAT COMMENT 'meters',
    elev_high                FLOAT,
    elev_low                 FLOAT,
    start_date               DATETIME,
    start_date_local         DATETIME,
    timezone                 VARCHAR(100),
    average_speed            FLOAT COMMENT 'm/s',
    max_speed                FLOAT COMMENT 'm/s',
    average_cadence          FLOAT,
    average_watts            FLOAT,
    max_watts                INT,
    weighted_average_watts   INT,
    kilojoules               FLOAT,
    average_heartrate        FLOAT,
    max_heartrate            FLOAT,
    suffer_score             INT,
    calories                 FLOAT,
    kudos_count              INT,
    comment_count            INT,
    achievement_count        INT,
    pr_count                 INT,
    start_latlng             JSON,
    end_latlng               JSON,
    map_summary_polyline     LONGTEXT,
    gear_id                  VARCHAR(50),
    commute                  TINYINT(1),
    trainer                  TINYINT(1),
    manual                   TINYINT(1),
    private                  TINYINT(1),
    flagged                  TINYINT(1),
    workout_type             INT,
    description              TEXT,
    imported_at              DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`

var columns = []string{
	"name", "sport_type", "distance", "moving_time", "elapsed_time",
	"total_elevation_gain", "elev_high", "elev_low",
	"start_date", "start_date_local", "timezone",
	"average_speed", "max_speed", "average_cadence",
	"average_watts", "max_watts", "weighted_average_watts", "kilojoules",
	"average_heartrate", "max_heartrate", "suffer_score", "calories",
	"kudos_count", "comment_count", "achievement_count", "pr_count",
	"start_latlng", "end_latlng", "map_summary_polyline",
	"gear_id", "commute", "trainer", "manual", "private",
	"flagged", "workout_type", "description",
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// SetupDatabase creates the database, user, and table using root credentials.
func SetupDatabase() error {
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	dbName := getEnv("MYSQL_DATABASE", "strava_data")
	user := getEnv("MYSQL_USER", "strava_user")
	password := os.Getenv("MYSQL_PASSWORD")
	rootUser := getEnv("MYSQL_ROOT_USER", "root")
	rootPass := os.Getenv("MYSQL_ROOT_PASSWORD")

	slog.Info(fmt.Sprintf("[setup] connect as root with: %s:%s...", host, port))

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/", rootUser, rootPass, host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	queries := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s';", user, password),
		fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s';", user, password),
		fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, INDEX ON `%s`.* TO '%s'@'%%';", dbName, user),
		"FLUSH PRIVILEGES;",
		fmt.Sprintf("USE `%s`;", dbName),
		createTableSQL,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("setup query failed: %w", err)
		}
	}

	slog.Info(fmt.Sprintf("[setup] Database `%s` ready.", dbName))
	slog.Info(fmt.Sprintf("[setup] User `%s` setup with the correct credentials `%s`.", user, dbName))
	return nil
}

// Connect returns an active database connection using the app user credentials.
func Connect() (*sql.DB, error) {
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	dbName := getEnv("MYSQL_DATABASE", "strava_data")
	user := getEnv("MYSQL_USER", "strava_user")
	password := os.Getenv("MYSQL_PASSWORD")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true",
		user, password, host, port, dbName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// parseDateTime converts ISO 8601 format to MySQL datetime format.
func parseDateTime(value string) *string {
	if value == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", value)
	if err != nil {
		return nil
	}
	s := t.Format("2006-01-02 15:04:05")
	return &s
}

// coerce ensures data is in the correct format before being written to the database.
func coerce(col string, value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch col {
	case "start_date", "start_date_local":
		if s, ok := value.(string); ok {
			return parseDateTime(s)
		}
		return nil
	case "start_latlng", "end_latlng":
		if _, ok := value.([]interface{}); ok {
			b, _ := json.Marshal(value)
			return string(b)
		}
		return value
	case "commute", "trainer", "manual", "private", "flagged":
		if b, ok := value.(bool); ok && b {
			return 1
		}
		return 0
	}
	return value
}

// UpsertRuns inserts or updates runs in the database.
func UpsertRuns(db *sql.DB, runs []map[string]interface{}) error {
	allCols := append([]string{"id"}, columns...)

	backtickCols := make([]string, len(allCols))
	for i, c := range allCols {
		backtickCols[i] = "`" + c + "`"
	}
	colList := strings.Join(backtickCols, ", ")
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(allCols)), ", ")

	updateParts := make([]string, len(columns))
	for i, c := range columns {
		updateParts[i] = fmt.Sprintf("`%s` = VALUES(`%s`)", c, c)
	}
	updates := strings.Join(updateParts, ", ")

	query := fmt.Sprintf(
		"INSERT INTO runs (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s;",
		colList, placeholders, updates,
	)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	inserted, updated, skipped := 0, 0, 0
	for _, act := range runs {
		var id int64
		switch v := act["id"].(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		}

		row := make([]interface{}, 0, len(allCols))
		row = append(row, id)
		for _, col := range columns {
			row = append(row, coerce(col, act[col]))
		}

		result, err := stmt.Exec(row...)
		if err != nil {
			return fmt.Errorf("upsert run %d: %w", id, err)
		}

		n, _ := result.RowsAffected()
		switch n {
		case 1:
			inserted++
		case 2:
			updated++
		default:
			skipped++
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("[mysql] new: %d  | changed: %d | unchanged: %d", inserted, updated, skipped))
	return nil
}
