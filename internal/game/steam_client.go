package game

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"
	repo "rent_game_accs/internal/repository/postgres"
)

type SteamClient interface {
	GetOwnedGames(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error)
	CheckVACBans(ctx context.Context, steamID64 string) (bool, error)
}

type SteamConfig struct {
	APIKey  string
	BaseURL string
}

type SteamClientImpl struct {
	cfg        SteamConfig
	httpClient *http.Client
	log        *zap.Logger
}

func NewSteamClient(cfg SteamConfig, log *zap.Logger) *SteamClientImpl {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.steampowered.com"
	}
	return &SteamClientImpl{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

func (c *SteamClientImpl) executeWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second
	maxRetries := 5

	var lastErr error
	var resp *http.Response

	for attempt := 0; attempt < maxRetries; attempt++ {

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		reqCopy := req.Clone(ctx)

		resp, lastErr = c.httpClient.Do(reqCopy)
		if lastErr == nil {

			if resp.StatusCode == http.StatusOK {
				return resp, nil
			}

			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return resp, nil
			}

			lastErr = fmt.Errorf("HTTP status %d", resp.StatusCode)
			resp.Body.Close()
		}

		delay := baseDelay * time.Duration(1<<attempt)
		jitter := time.Duration(rand.Intn(int(delay)/10 + 1))
		delay = delay + jitter
		if delay > maxDelay {
			delay = maxDelay
		}

		c.log.Warn("Steam Web API request failed, retrying",
			zap.String("url", req.URL.String()),
			zap.Int("attempt", attempt+1),
			zap.Duration("delay", delay),
			zap.Error(lastErr),
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("steam api call failed after %d retries: %w", maxRetries, lastErr)
}

type steamGameResponse struct {
	Response struct {
		GameCount int `json:"game_count"`
		Games     []struct {
			AppID           int    `json:"appid"`
			Name            string `json:"name"`
			PlaytimeForever int    `json:"playtime_forever"`
		} `json:"games"`
	} `json:"response"`
}

func (c *SteamClientImpl) GetOwnedGames(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error) {
	if steamID64 == "" {
		return nil, fmt.Errorf("steamID64 cannot be empty")
	}

	apiURL := fmt.Sprintf("%s/IPlayerService/GetOwnedGames/v1/", c.cfg.BaseURL)
	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("key", c.cfg.APIKey)
	q.Set("steamid", steamID64)
	q.Set("include_appinfo", "1")
	q.Set("include_played_free_games", "1")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d from Steam Web API GetOwnedGames", resp.StatusCode)
	}

	var gameResp steamGameResponse
	if err := json.NewDecoder(resp.Body).Decode(&gameResp); err != nil {
		return nil, fmt.Errorf("failed to decode Steam GetOwnedGames response: %w", err)
	}

	syncGames := make([]repo.AccountGameSyncInfo, 0, len(gameResp.Response.Games))
	for _, g := range gameResp.Response.Games {
		syncGames = append(syncGames, repo.AccountGameSyncInfo{
			StoreGameID:     strconv.Itoa(g.AppID),
			Name:            g.Name,
			PlaytimeMinutes: g.PlaytimeForever,
		})
	}

	return syncGames, nil
}

type steamBansResponse struct {
	Players []struct {
		SteamID          string `json:"SteamId"`
		CommunityBanned  bool   `json:"CommunityBanned"`
		VACBanned        bool   `json:"VACBanned"`
		NumberOfVACBans  int    `json:"NumberOfVACBans"`
		DaysSinceLastBan int    `json:"DaysSinceLastBan"`
		NumberOfGameBans int    `json:"NumberOfGameBans"`
		EconomyBan       string `json:"EconomyBan"`
	} `json:"players"`
}

func (c *SteamClientImpl) CheckVACBans(ctx context.Context, steamID64 string) (bool, error) {
	if steamID64 == "" {
		return false, fmt.Errorf("steamID64 cannot be empty")
	}

	apiURL := fmt.Sprintf("%s/ISteamUser/GetPlayerBans/v1/", c.cfg.BaseURL)
	u, err := url.Parse(apiURL)
	if err != nil {
		return false, err
	}

	q := u.Query()
	q.Set("key", c.cfg.APIKey)
	q.Set("steamids", steamID64)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err
	}

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status code %d from Steam Web API GetPlayerBans", resp.StatusCode)
	}

	var bansResp steamBansResponse
	if err := json.NewDecoder(resp.Body).Decode(&bansResp); err != nil {
		return false, fmt.Errorf("failed to decode Steam GetPlayerBans response: %w", err)
	}

	if len(bansResp.Players) == 0 {
		return false, fmt.Errorf("no player records returned for steamid: %s", steamID64)
	}

	return bansResp.Players[0].VACBanned, nil
}
