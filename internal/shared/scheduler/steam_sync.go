package scheduler

import (
	"context"
	"fmt"
	"math/rand"

	"go.uber.org/zap"
	"rent_game_accs/internal/game"
	repo "rent_game_accs/internal/repository/postgres"
)

type SteamSyncRepository interface {
	GetAccountsForSync(ctx context.Context) ([]int64, error)
	GetAccountSyncDetails(ctx context.Context, accountID int64) (string, string, error)
	SyncAccountGames(ctx context.Context, accountID int64, games []repo.AccountGameSyncInfo) error
	DisableAccountIfIdle(ctx context.Context, accountID int64) error
}

type FakeSteamClient struct {
	randSource *rand.Rand
}

func NewFakeSteamClient() *FakeSteamClient {
	return &FakeSteamClient{
		randSource: rand.New(rand.NewSource(42)),
	}
}

func (f *FakeSteamClient) GetOwnedGames(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error) {

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	gamesPool := []struct {
		AppID string
		Title string
	}{
		{"730", "Counter-Strike 2"},
		{"570", "Dota 2"},
		{"1091500", "Cyberpunk 2077"},
		{"1174180", "Red Dead Redemption 2"},
		{"292030", "The Witcher 3: Wild Hunt"},
		{"400", "Portal"},
		{"220", "Half-Life 2"},
	}

	numGames := (len(steamID64) % len(gamesPool)) + 1
	if numGames == 0 {
		numGames = 1
	}

	result := make([]repo.AccountGameSyncInfo, 0, numGames)
	for i := 0; i < numGames; i++ {
		g := gamesPool[(i+len(steamID64))%len(gamesPool)]

		playtime := 10 + (len(steamID64)*31+i*17)%10000

		result = append(result, repo.AccountGameSyncInfo{
			StoreGameID:     g.AppID,
			Name:            g.Title,
			PlaytimeMinutes: playtime,
		})
	}

	return result, nil
}

func (f *FakeSteamClient) CheckVACBans(ctx context.Context, steamID64 string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	if len(steamID64) > 3 && steamID64[len(steamID64)-3:] == "999" {
		return true, nil
	}
	return false, nil
}

func NewSteamSyncWorker(
	r SteamSyncRepository,
	client game.SteamClient,
	log *zap.Logger,
) Task {
	return func(ctx context.Context) error {
		log.Info("running steam library synchronization")

		accounts, err := r.GetAccountsForSync(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch accounts for sync: %w", err)
		}

		if len(accounts) == 0 {
			log.Info("no accounts found to sync")
			return nil
		}

		log.Info("found accounts to sync", zap.Int("count", len(accounts)))

		for _, accountID := range accounts {
			login, steamID64, err := r.GetAccountSyncDetails(ctx, accountID)
			if err != nil {
				log.Error("failed to get account sync details",
					zap.Int64("account_id", accountID),
					zap.Error(err),
				)
				continue
			}

			if steamID64 == "" {
				log.Warn("account has no steam_id64, skipping sync",
					zap.Int64("account_id", accountID),
					zap.String("login", login),
				)
				continue
			}

			log.Info("checking VAC bans for account",
				zap.Int64("account_id", accountID),
				zap.String("login", login),
				zap.String("steam_id64", steamID64),
			)

			vacBanned, err := client.CheckVACBans(ctx, steamID64)
			if err != nil {
				log.Error("failed to check VAC bans from Steam Web API",
					zap.String("login", login),
					zap.String("steam_id64", steamID64),
					zap.Error(err),
				)
				continue
			}

			if vacBanned {
				log.Warn("account is VAC banned, banning in database",
					zap.Int64("account_id", accountID),
					zap.String("login", login),
					zap.String("steam_id64", steamID64),
				)
				err = r.DisableAccountIfIdle(ctx, accountID)
				if err != nil {
					log.Error("failed to ban account in database",
						zap.Int64("account_id", accountID),
						zap.Error(err),
					)
				}
				continue
			}

			log.Info("syncing library for account",
				zap.Int64("account_id", accountID),
				zap.String("login", login),
				zap.String("steam_id64", steamID64),
			)

			games, err := client.GetOwnedGames(ctx, steamID64)
			if err != nil {
				log.Error("failed to fetch owned games from Steam Web API",
					zap.String("login", login),
					zap.String("steam_id64", steamID64),
					zap.Error(err),
				)
				continue
			}

			err = r.SyncAccountGames(ctx, accountID, games)
			if err != nil {
				log.Error("failed to sync games in database",
					zap.Int64("account_id", accountID),
					zap.Error(err),
				)
				continue
			}

			log.Info("successfully synced library for account",
				zap.Int64("account_id", accountID),
				zap.Int("games_count", len(games)),
			)
		}

		return nil
	}
}
