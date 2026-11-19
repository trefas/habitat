package domain

import "time"

// Виды уведомлений.
const (
	NotificationShareReceived = "share_received"
	NotificationShareRevoked  = "share_revoked"
	NotificationShareExpiring = "share_expiring"
	NotificationEntityCopied  = "entity_copied"
	NotificationEntityUpdated = "entity_updated"
)

// Notification — входящее уведомление пользователя.
type Notification struct {
	ID        string
	UserID    string
	Kind      string
	EntityID  *string
	ShareID   *string
	ActorID   *string
	Payload   []byte
	ReadAt    *time.Time
	CreatedAt time.Time
}
