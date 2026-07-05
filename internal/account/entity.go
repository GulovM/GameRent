package account

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidSteamID      = errors.New("steam ID must be a non-empty numeric string")
	ErrInvalidHourlyPrice  = errors.New("hourly price must be positive")
	ErrInvalidDeposit      = errors.New("deposit amount must be non-negative")
	ErrCannotPublish       = errors.New("cannot publish account: security check or synchronization requirements not met")
	ErrInvalidState        = errors.New("invalid status transition")
	ErrEmptyLogin          = errors.New("login cannot be empty")
	ErrEmptyPassword       = errors.New("password cannot be empty")
	ErrUnsupportedCurrency = errors.New("unsupported currency")
)

type AccountStatus uint8

const (
	StatusCreated     AccountStatus = 0
	StatusVerifying   AccountStatus = 1
	StatusAvailable   AccountStatus = 2
	StatusReserved    AccountStatus = 3
	StatusRented      AccountStatus = 4
	StatusMaintenance AccountStatus = 5
	StatusDisabled    AccountStatus = 6
)

func (s AccountStatus) String() string {
	switch s {
	case StatusCreated:
		return "Created"
	case StatusVerifying:
		return "Verifying"
	case StatusAvailable:
		return "Available"
	case StatusReserved:
		return "Reserved"
	case StatusRented:
		return "Rented"
	case StatusMaintenance:
		return "Maintenance"
	case StatusDisabled:
		return "Disabled"
	default:
		return "Unknown"
	}
}

type Money struct {
	Amount   int64
	Currency string
}

func NewMoney(amount int64, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency != "USD" && currency != "EUR" && currency != "RUB" && currency != "TJS" {
		return Money{}, ErrUnsupportedCurrency
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func (m Money) IsZero() bool {
	return m.Amount == 0
}

func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, errors.New("currency mismatch")
	}
	return Money{Amount: m.Amount + o.Amount, Currency: m.Currency}, nil
}

func (m Money) Sub(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, errors.New("currency mismatch")
	}
	return Money{Amount: m.Amount - o.Amount, Currency: m.Currency}, nil
}

type SteamCredentials struct {
	Login             string
	EncryptedPassword []byte
	SteamID64         string
}

func NewSteamCredentials(login string, encryptedPassword []byte, steamID64 string) (SteamCredentials, error) {
	if login == "" {
		return SteamCredentials{}, ErrEmptyLogin
	}
	if len(encryptedPassword) == 0 {
		return SteamCredentials{}, ErrEmptyPassword
	}
	if steamID64 == "" {
		return SteamCredentials{}, ErrInvalidSteamID
	}
	for _, c := range steamID64 {
		if c < '0' || c > '9' {
			return SteamCredentials{}, ErrInvalidSteamID
		}
	}
	return SteamCredentials{
		Login:             login,
		EncryptedPassword: encryptedPassword,
		SteamID64:         steamID64,
	}, nil
}

type Game struct {
	ID               int64
	SteamAppID       int
	Name             string
	ShortDescription string
	HeaderImage      string
	ReleaseDate      *time.Time
	Developers       []string
	Publishers       []string
	Genres           []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AccountGame struct {
	Game            Game
	PlaytimeMinutes int
}

type Account struct {
	ID                int64
	Credentials       SteamCredentials
	SteamGuardEnabled bool
	InventoryVerified bool
	LastSecurityCheck time.Time
	HourlyPrice       Money
	DepositAmount     Money
	Status            AccountStatus
	ProfileURL        string
	AvatarURL         string
	LibrarySyncedAt   time.Time
	Games             []AccountGame
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

func NewAccount(creds SteamCredentials, hourlyPrice, depositAmount Money, now time.Time) (*Account, error) {
	if hourlyPrice.Amount <= 0 {
		return nil, ErrInvalidHourlyPrice
	}
	if depositAmount.Amount < 0 {
		return nil, ErrInvalidDeposit
	}
	return &Account{
		Credentials:   creds,
		HourlyPrice:   hourlyPrice,
		DepositAmount: depositAmount,
		Status:        StatusCreated,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (a *Account) Verify(now time.Time) {
	a.Status = StatusVerifying
	a.UpdatedAt = now
}

func (a *Account) MarkSecurityChecked(steamGuard, inventoryVerified bool, now time.Time) {
	a.SteamGuardEnabled = steamGuard
	a.InventoryVerified = inventoryVerified
	a.LastSecurityCheck = now
	a.UpdatedAt = now
}

func (a *Account) SyncLibrary(games []AccountGame, now time.Time) {
	a.Games = games
	a.LibrarySyncedAt = now
	a.UpdatedAt = now
}

func (a *Account) Publish(now time.Time) error {

	if !a.SteamGuardEnabled || !a.InventoryVerified || a.LibrarySyncedAt.IsZero() {
		return ErrCannotPublish
	}
	a.Status = StatusAvailable
	a.UpdatedAt = now
	return nil
}

func (a *Account) Reserve(now time.Time) error {
	if a.Status != StatusAvailable {
		return ErrInvalidState
	}
	a.Status = StatusReserved
	a.UpdatedAt = now
	return nil
}

func (a *Account) Rent(now time.Time) error {
	if a.Status != StatusReserved {
		return ErrInvalidState
	}
	a.Status = StatusRented
	a.UpdatedAt = now
	return nil
}

func (a *Account) Release(now time.Time) error {
	if a.Status != StatusRented && a.Status != StatusReserved {
		return ErrInvalidState
	}
	a.Status = StatusAvailable
	a.UpdatedAt = now
	return nil
}

func (a *Account) SetMaintenance(now time.Time) {
	a.Status = StatusMaintenance
	a.UpdatedAt = now
}

func (a *Account) Disable(now time.Time) {
	a.Status = StatusDisabled
	a.UpdatedAt = now
}
