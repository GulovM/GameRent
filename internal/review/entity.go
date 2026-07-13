package review

import "time"

type Review struct {
	ID        int64
	UserID    int64
	AccountID int64
	Rating    int16
	Comment   string
	CreatedAt time.Time
}
