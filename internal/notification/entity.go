package notification

import "time"

type Notification struct {
	ID        int64
	Type      int16
	Title     string
	Body      string
	Read      bool
	CreatedAt time.Time
}
