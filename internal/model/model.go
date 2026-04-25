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
