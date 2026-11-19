package domain

import "time"

// Message — одиночное сообщение, хранящееся в реестре сущностей.
type Message struct {
	ID               string
	OwnerID          string
	OwnerEmail       string
	OwnerDisplayName string
	Title            string
	Text             string
	IsOwner          bool
	ReadAt           *time.Time
	RecipientsCount  int
	ReadCount        int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// MessageRecipient — получатель сообщения и факт его прочтения.
type MessageRecipient struct {
	UserID      string
	Email       string
	DisplayName string
	ReadAt      *time.Time
}

// MessageView — сообщение вместе со списком получателей (для владельца).
type MessageView struct {
	Message    Message
	Recipients []MessageRecipient
}
