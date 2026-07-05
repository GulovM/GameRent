package game

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

func TestSteamClient_GetOwnedGames_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/IPlayerService/GetOwnedGames/v1/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("unexpected key: %s", r.URL.Query().Get("key"))
		}
		if r.URL.Query().Get("steamid") != "12345678" {
			t.Errorf("unexpected steamid: %s", r.URL.Query().Get("steamid"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"response": {
				"game_count": 2,
				"games": [
					{"appid": 730, "name": "Counter-Strike 2", "playtime_forever": 1500},
					{"appid": 570, "name": "Dota 2", "playtime_forever": 3000}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewSteamClient(SteamConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	}, zap.NewNop())

	games, err := client.GetOwnedGames(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(games) != 2 {
		t.Errorf("expected 2 games, got %d", len(games))
	}
	if games[0].StoreGameID != "730" || games[0].Name != "Counter-Strike 2" || games[0].PlaytimeMinutes != 1500 {
		t.Errorf("unexpected game 0 details: %+v", games[0])
	}
}

func TestSteamClient_CheckVACBans_Banned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ISteamUser/GetPlayerBans/v1/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"players": [
				{
					"SteamId": "12345678",
					"CommunityBanned": false,
					"VACBanned": true,
					"NumberOfVACBans": 1
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewSteamClient(SteamConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	}, zap.NewNop())

	banned, err := client.CheckVACBans(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !banned {
		t.Errorf("expected VACBanned to be true")
	}
}

func TestSteamClient_RetryOnFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"players": [
				{
					"SteamId": "12345678",
					"CommunityBanned": false,
					"VACBanned": false
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewSteamClient(SteamConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	}, zap.NewNop())

	banned, err := client.CheckVACBans(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if banned {
		t.Errorf("expected VACBanned to be false")
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
