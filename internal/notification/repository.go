package notification

import "context"

type Repository interface {
	ListByUser(ctx context.Context, userID int64) ([]Notification, error)
	MarkRead(ctx context.Context, notificationID, userID int64) (bool, error)
}
