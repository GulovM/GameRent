package user

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrInvalidTrustScore = errors.New("trust score must be between 0 and 1000")
	ErrInvalidEmail      = errors.New("invalid email address")
)

type UserRole string

const (
	RoleAdmin UserRole = "ADMIN"
	RoleRent  UserRole = "RENT"
)

type TrustLevel string

const (
	TrustLevelBronze  TrustLevel = "Bronze"
	TrustLevelSilver  TrustLevel = "Silver"
	TrustLevelGold    TrustLevel = "Gold"
	TrustLevelDiamond TrustLevel = "Diamond"
)

func (tl TrustLevel) String() string {
	return string(tl)
}

func GetTrustLevel(score int) TrustLevel {
	if score >= 850 {
		return TrustLevelDiamond
	}
	if score >= 600 {
		return TrustLevelGold
	}
	if score >= 300 {
		return TrustLevelSilver
	}
	return TrustLevelBronze
}

type User struct {
	ID         int64
	Email      string
	FirstName  string
	LastName   string
	TrustScore int
	Role       UserRole
	IsBlocked  bool
	Balance    int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
}

func NewUser(email, firstName, lastName string, now time.Time) (*User, error) {
	u := &User{
		Email:      email,
		FirstName:  firstName,
		LastName:   lastName,
		TrustScore: 300,
		Role:       RoleRent,
		CreatedAt:  now,
		UpdatedAt:  now,
		IsBlocked:  false,
	}
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *User) Validate() error {
	if err := ValidateEmail(u.Email); err != nil {
		return err
	}
	if u.TrustScore < 0 || u.TrustScore > 1000 {
		return ErrInvalidTrustScore
	}
	return nil
}

func ValidateEmail(email string) error {
	if len(email) == 0 {
		return ErrInvalidEmail
	}
	_, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ErrInvalidEmail
	}
	return nil
}

func (u *User) TrustLevel() TrustLevel {
	return GetTrustLevel(u.TrustScore)
}

func (u *User) ModifyTrustScore(delta int, now time.Time) error {
	newScore := u.TrustScore + delta
	if newScore < 0 {
		newScore = 0
	}
	if newScore > 1000 {
		newScore = 1000
	}
	u.TrustScore = newScore
	u.UpdatedAt = now
	return nil
}

func (u *User) Block(now time.Time) {
	u.IsBlocked = true
	u.UpdatedAt = now
}

func (u *User) Unblock(now time.Time) {
	u.IsBlocked = false
	u.UpdatedAt = now
}
