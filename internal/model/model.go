package model

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	StorageUsed  int64     `json:"storage_used"`
	TOTPSecret   string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Tag struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

type FileTag struct {
	FileID int64 `json:"file_id"`
	TagID  int64 `json:"tag_id"`
}

type RefreshToken struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	TokenHash string    `json:"-"`
	FamilyID  string    `json:"family_id"`
	Revoked   bool      `json:"revoked"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type File struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	ParentID     *int64     `json:"parent_id"`
	Name         string     `json:"name"`
	IsDir        bool       `json:"is_dir"`
	Size         int64      `json:"size"`
	FileHash     string     `json:"file_hash"`
	StorageKey   string     `json:"storage_key"`
	ThumbnailKey string     `json:"thumbnail_key"`
	MimeType     string     `json:"mime_type"`
	IsStarred    bool       `json:"is_starred"`
	IsDeleted    bool       `json:"is_deleted"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type AuditLog struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   int64     `json:"target_id"`
	Detail     string    `json:"detail"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}

type Share struct {
	ID           int64      `json:"id"`
	Token        string     `json:"token"`
	FileID       int64      `json:"file_id"`
	OwnerID      int64      `json:"owner_id"`
	PasswordHash string     `json:"-"`
	ExpireAt     *time.Time `json:"expire_at,omitempty"`
	MaxDownloads int64      `json:"max_downloads"`
	ViewCount    int64      `json:"view_count"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type TempDownload struct {
	ID        int64     `json:"id"`
	Token     string    `json:"token"`
	FileID    int64     `json:"file_id"`
	UserID    int64     `json:"user_id"`
	ExpireAt  time.Time `json:"expire_at"`
	CreatedAt time.Time `json:"created_at"`
}
