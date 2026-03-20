/*
strava.go
---------

File with all the functions used to fetch data from the Strava API.
*/
package strava

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	baseURL  = "https://www.strava.com/api/v3"
	tokenURL = "https://www.strava.com/oauth/token"
)

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// GetAccessToken reads the access token from the token file and refreshes it if expired.
func GetAccessToken() (string, error) {
	clientID := os.Getenv("STRAVA_CLIENT_ID")
	clientSecret := os.Getenv("STRAVA_CLIENT_SECRET")
	tokenFile := getEnv("STRAVA_TOKEN_FILE", "strava_tokens.json")

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("no tokens found, run get_access_tokens first: %w", err)
	}

	var tokens map[string]interface{}
	if err := json.Unmarshal(data, &tokens); err != nil {
		return "", fmt.Errorf("failed to parse token file: %w", err)
	}

	expiresAt, _ := tokens["expires_at"].(float64)
	if float64(time.Now().Unix()+60) >= expiresAt {
		slog.Info("[auth] Token expired — renewing token...")

		refreshToken, _ := tokens["refresh_token"].(string)
		resp, err := http.PostForm(tokenURL, url.Values{
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"refresh_token": {refreshToken},
			"grant_type":    {"refresh_token"},
		})
		if err != nil {
			return "", fmt.Errorf("token refresh request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("token refresh failed: HTTP %d", resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
			return "", fmt.Errorf("failed to parse refresh response: %w", err)
		}

		updated, _ := json.MarshalIndent(tokens, "", "  ")
		if err := os.WriteFile(tokenFile, updated, 0644); err != nil {
			return "", fmt.Errorf("failed to save tokens: %w", err)
		}
		slog.Info("[auth] Token renewed.")
	}

	accessToken, ok := tokens["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("access_token missing or invalid in token file")
	}
	return accessToken, nil
}

// apiGet performs a GET request to the Strava API and handles rate limiting.
func apiGet(endpoint, token string, params map[string]string) (interface{}, error) {
	client := &http.Client{}
	for {
		req, err := http.NewRequest("GET", baseURL+endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		q := req.URL.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 429 {
			resp.Body.Close()
			reset, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
			wait := reset - time.Now().Unix()
			if wait < 60 {
				wait = 60
			}
			slog.Info(fmt.Sprintf("[api] Rate limit — waits %ds...", wait))
			time.Sleep(time.Duration(wait) * time.Second)
			continue
		}

		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("API request failed: HTTP %d", resp.StatusCode)
		}

		var result interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode API response: %w", err)
		}
		return result, nil
	}
}

// FetchAllRuns fetches all running activities from Strava and returns them as a list.
func FetchAllRuns(token string) ([]map[string]interface{}, error) {
	var runs []map[string]interface{}
	page := 1
	slog.Info("[strava] getting activities...")

	runTypes := map[string]bool{"Run": true, "TrailRun": true, "VirtualRun": true}

	for {
		result, err := apiGet("/athlete/activities", token, map[string]string{
			"per_page": "200",
			"page":     strconv.Itoa(page),
		})
		if err != nil {
			return nil, err
		}

		batch, ok := result.([]interface{})
		if !ok || len(batch) == 0 {
			break
		}

		var runBatch []map[string]interface{}
		for _, item := range batch {
			a, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			sportType, _ := a["sport_type"].(string)
			actType, _ := a["type"].(string)
			if runTypes[sportType] || actType == "Run" {
				runBatch = append(runBatch, a)
			}
		}

		runs = append(runs, runBatch...)
		slog.Info(fmt.Sprintf("[strava]   page %d: %d activities, %d runs (totaal: %d)",
			page, len(batch), len(runBatch), len(runs)))
		page++
	}

	slog.Info(fmt.Sprintf("[strava] finished — %d runs received succesfully.", len(runs)))
	return runs, nil
}
