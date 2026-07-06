package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	repo "rent_game_accs/internal/repository/postgres"
	"rent_game_accs/internal/shared/clock"
)

type MockExpiredCleanupRepository struct {
	GetExpiredRentalsFunc func(ctx context.Context, now time.Time) ([]repo.ExpiredRental, error)
	ExpireRentalFunc      func(ctx context.Context, rentalID, accountID int64, now time.Time) (bool, error)
}

func (m *MockExpiredCleanupRepository) GetExpiredRentals(ctx context.Context, now time.Time) ([]repo.ExpiredRental, error) {
	return m.GetExpiredRentalsFunc(ctx, now)
}

func (m *MockExpiredCleanupRepository) ExpireRental(ctx context.Context, rentalID, accountID int64, now time.Time) (bool, error) {
	return m.ExpireRentalFunc(ctx, rentalID, accountID, now)
}

type MockWaitingPaymentCleanupRepository struct {
	GetExpiredWaitingPaymentReservationsFunc func(ctx context.Context, now time.Time) ([]repo.ExpiredWaitingPaymentReservation, error)
	ExpireWaitingPaymentReservationFunc      func(ctx context.Context, paymentID int64, now time.Time) (bool, error)
}

func (m *MockWaitingPaymentCleanupRepository) GetExpiredWaitingPaymentReservations(ctx context.Context, now time.Time) ([]repo.ExpiredWaitingPaymentReservation, error) {
	return m.GetExpiredWaitingPaymentReservationsFunc(ctx, now)
}

func (m *MockWaitingPaymentCleanupRepository) ExpireWaitingPaymentReservation(ctx context.Context, paymentID int64, now time.Time) (bool, error) {
	return m.ExpireWaitingPaymentReservationFunc(ctx, paymentID, now)
}

type MockSteamSyncRepository struct {
	GetAccountsForSyncFunc    func(ctx context.Context) ([]int64, error)
	GetAccountSyncDetailsFunc func(ctx context.Context, accountID int64) (string, string, error)
	SyncAccountGamesFunc      func(ctx context.Context, accountID int64, games []repo.AccountGameSyncInfo) error
	BanAccountFunc            func(ctx context.Context, accountID int64) error
}

func (m *MockSteamSyncRepository) GetAccountsForSync(ctx context.Context) ([]int64, error) {
	return m.GetAccountsForSyncFunc(ctx)
}

func (m *MockSteamSyncRepository) GetAccountSyncDetails(ctx context.Context, accountID int64) (string, string, error) {
	return m.GetAccountSyncDetailsFunc(ctx, accountID)
}

func (m *MockSteamSyncRepository) SyncAccountGames(ctx context.Context, accountID int64, games []repo.AccountGameSyncInfo) error {
	return m.SyncAccountGamesFunc(ctx, accountID, games)
}

func (m *MockSteamSyncRepository) BanAccount(ctx context.Context, accountID int64) error {
	return m.BanAccountFunc(ctx, accountID)
}

type MockSteamClient struct {
	GetOwnedGamesFunc func(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error)
	CheckVACBansFunc  func(ctx context.Context, steamID64 string) (bool, error)
}

func (m *MockSteamClient) GetOwnedGames(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error) {
	return m.GetOwnedGamesFunc(ctx, steamID64)
}

func (m *MockSteamClient) CheckVACBans(ctx context.Context, steamID64 string) (bool, error) {
	return m.CheckVACBansFunc(ctx, steamID64)
}

func TestExpiredCleanupWorker_Success(t *testing.T) {
	mockTime := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMockClock(mockTime)
	logger := zap.NewNop()

	rentalID := int64(101)
	accountID := int64(202)

	mockRepo := &MockExpiredCleanupRepository{
		GetExpiredRentalsFunc: func(ctx context.Context, now time.Time) ([]repo.ExpiredRental, error) {
			if !now.Equal(mockTime) {
				t.Errorf("expected time %v, got %v", mockTime, now)
			}
			return []repo.ExpiredRental{
				{ID: rentalID, AccountID: accountID},
			}, nil
		},
		ExpireRentalFunc: func(ctx context.Context, rID, aID int64, now time.Time) (bool, error) {
			if rID != rentalID {
				t.Errorf("expected rental ID %d, got %d", rentalID, rID)
			}
			if aID != accountID {
				t.Errorf("expected account ID %d, got %d", accountID, aID)
			}
			if !now.Equal(mockTime) {
				t.Errorf("expected time %v, got %v", mockTime, now)
			}
			return true, nil
		},
	}

	worker := NewExpiredCleanupWorker(mockRepo, clk, logger)
	err := worker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExpiredCleanupWorker_RepositoryError(t *testing.T) {
	clk := clock.NewMockClock(time.Now())
	logger := zap.NewNop()

	expectedErr := errors.New("db error")
	mockRepo := &MockExpiredCleanupRepository{
		GetExpiredRentalsFunc: func(ctx context.Context, now time.Time) ([]repo.ExpiredRental, error) {
			return nil, expectedErr
		},
	}

	worker := NewExpiredCleanupWorker(mockRepo, clk, logger)
	err := worker(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestExpiredWaitingPaymentCleanupWorker_Success(t *testing.T) {
	mockTime := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMockClock(mockTime)
	logger := zap.NewNop()

	paymentID := int64(301)
	rentalID := int64(302)
	accountID := int64(303)

	mockRepo := &MockWaitingPaymentCleanupRepository{
		GetExpiredWaitingPaymentReservationsFunc: func(ctx context.Context, now time.Time) ([]repo.ExpiredWaitingPaymentReservation, error) {
			if !now.Equal(mockTime) {
				t.Errorf("expected time %v, got %v", mockTime, now)
			}
			return []repo.ExpiredWaitingPaymentReservation{{PaymentID: paymentID, RentalID: rentalID, AccountID: accountID, UserID: 404}}, nil
		},
		ExpireWaitingPaymentReservationFunc: func(ctx context.Context, gotPaymentID int64, now time.Time) (bool, error) {
			if gotPaymentID != paymentID {
				t.Errorf("expected payment ID %d, got %d", paymentID, gotPaymentID)
			}
			if !now.Equal(mockTime) {
				t.Errorf("expected time %v, got %v", mockTime, now)
			}
			return true, nil
		},
	}

	worker := NewExpiredWaitingPaymentCleanupWorker(mockRepo, clk, logger)
	if err := worker(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExpiredWaitingPaymentCleanupWorker_RepositoryError(t *testing.T) {
	clk := clock.NewMockClock(time.Now())
	logger := zap.NewNop()

	expectedErr := errors.New("db error")
	mockRepo := &MockWaitingPaymentCleanupRepository{
		GetExpiredWaitingPaymentReservationsFunc: func(ctx context.Context, now time.Time) ([]repo.ExpiredWaitingPaymentReservation, error) {
			return nil, expectedErr
		},
	}

	worker := NewExpiredWaitingPaymentCleanupWorker(mockRepo, clk, logger)
	err := worker(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestSteamSyncWorker_Success(t *testing.T) {
	logger := zap.NewNop()
	accountID := int64(456)
	loginName := "test_steam_user"
	steamID64Val := "76561198000000000"

	mockRepo := &MockSteamSyncRepository{
		GetAccountsForSyncFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{accountID}, nil
		},
		GetAccountSyncDetailsFunc: func(ctx context.Context, aID int64) (string, string, error) {
			if aID != accountID {
				t.Errorf("expected account ID %d, got %d", accountID, aID)
			}
			return loginName, steamID64Val, nil
		},
		SyncAccountGamesFunc: func(ctx context.Context, aID int64, games []repo.AccountGameSyncInfo) error {
			if aID != accountID {
				t.Errorf("expected account ID %d, got %d", accountID, aID)
			}
			if len(games) != 1 || games[0].StoreGameID != "123" || games[0].Name != "Test Game" {
				t.Errorf("unexpected synced games: %v", games)
			}
			return nil
		},
	}

	mockSteam := &MockSteamClient{
		CheckVACBansFunc: func(ctx context.Context, steamID64 string) (bool, error) {
			if steamID64 != steamID64Val {
				t.Errorf("expected steamID64 %s, got %s", steamID64Val, steamID64)
			}
			return false, nil
		},
		GetOwnedGamesFunc: func(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error) {
			if steamID64 != steamID64Val {
				t.Errorf("expected steamID64 %s, got %s", steamID64Val, steamID64)
			}
			return []repo.AccountGameSyncInfo{
				{StoreGameID: "123", Name: "Test Game", PlaytimeMinutes: 120},
			}, nil
		},
	}

	worker := NewSteamSyncWorker(mockRepo, mockSteam, logger)
	err := worker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSteamSyncWorker_VACBanned(t *testing.T) {
	logger := zap.NewNop()
	accountID := int64(789)
	loginName := "banned_steam_user"
	steamID64Val := "76561198000000999"
	banCalled := false

	mockRepo := &MockSteamSyncRepository{
		GetAccountsForSyncFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{accountID}, nil
		},
		GetAccountSyncDetailsFunc: func(ctx context.Context, aID int64) (string, string, error) {
			return loginName, steamID64Val, nil
		},
		BanAccountFunc: func(ctx context.Context, aID int64) error {
			if aID != accountID {
				t.Errorf("expected account ID %d, got %d", accountID, aID)
			}
			banCalled = true
			return nil
		},
		SyncAccountGamesFunc: func(ctx context.Context, aID int64, games []repo.AccountGameSyncInfo) error {
			t.Errorf("SyncAccountGames should not be called for VAC banned account")
			return nil
		},
	}

	mockSteam := &MockSteamClient{
		CheckVACBansFunc: func(ctx context.Context, steamID64 string) (bool, error) {
			return true, nil
		},
		GetOwnedGamesFunc: func(ctx context.Context, steamID64 string) ([]repo.AccountGameSyncInfo, error) {
			t.Errorf("GetOwnedGames should not be called for VAC banned account")
			return nil, nil
		},
	}

	worker := NewSteamSyncWorker(mockRepo, mockSteam, logger)
	err := worker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !banCalled {
		t.Errorf("expected BanAccount to be called")
	}
}
