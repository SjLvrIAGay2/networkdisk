package model

import "time"

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	FamilyID  string
	Revoked   bool
	ExpiresAt time.Time
	CreatedAt time.Time
}

type File struct {
	ID           int64
	UserID       int64
	ParentID     *int64
	Name         string
	IsDir        bool
	Size         int64
	FileHash     string
	StorageKey   string
	ThumbnailKey string
	MimeType     string
	IsDeleted    bool
	DeletedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
