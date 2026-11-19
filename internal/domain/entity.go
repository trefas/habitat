package domain

import "time"

// Типы сущностей, поддержанные сервером.
const (
	EntityTypeMessage = "message"
)

// Режимы и права передачи доступа.
const (
	ShareModeDirect = "direct"
	ShareModeCopy   = "copy"

	PermissionRead  = "read"
	PermissionWrite = "write"
)

// Entity — запись единого реестра сущностей.
type Entity struct {
	ID         string
	TypeCode   string
	OwnerID    string
	Title      string
	IsArchived bool
	Version    int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Share — выданный доступ к сущности.
type Share struct {
	ID             string
	EntityID       string
	GrantorID      string
	GranteeID      string
	Mode           string
	Permission     string
	GrantedAt      time.Time
	ExpiresAt      *time.Time
	RevokedAt      *time.Time
	CopiedEntityID *string
}

// ValidShareMode сообщает, допустим ли режим передачи.
func ValidShareMode(mode string) bool {
	return mode == ShareModeDirect || mode == ShareModeCopy
}

// ValidPermission сообщает, допустимо ли право доступа.
func ValidPermission(permission string) bool {
	return permission == PermissionRead || permission == PermissionWrite
}
