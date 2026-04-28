package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"networkdisk/internal/model"
)

var ErrUsernameTaken = fmt.Errorf("用户名已被占用")

func (s *Store) CreateUser(username, passwordHash string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return nil, fmt.Errorf("创建用户：用户名长度必须在3-64个字符之间")
	}
	if passwordHash == "" {
		return nil, fmt.Errorf("创建用户：密码哈希不能为空")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO users (username, password_hash) VALUES (?, ?)",
		username, passwordHash,
	)
	if err != nil {
		if mysqlErr, ok := err.(*mysql.MySQLError); ok && mysqlErr.Number == 1062 {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create user get id: %w", err)
	}
	return s.UserByID(id)
}

func (s *Store) UserByUsername(username string) (*model.User, error) {
	if strings.TrimSpace(username) == "" {
		return nil, fmt.Errorf("通过用户名查找用户：用户名不能为空")
	}
	user := &model.User{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, username, password_hash, storage_used, created_at, updated_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.StorageUsed, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user by username: %w", err)
	}
	return user, nil
}

func (s *Store) UserByID(id int64) (*model.User, error) {
	if id <= 0 {
		return nil, fmt.Errorf("通过ID查找用户：ID必须为正数，当前值为%d", id)
	}
	user := &model.User{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, username, password_hash, storage_used, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.StorageUsed, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user by id: %w", err)
	}
	return user, nil
}

func (s *Store) UpdateUserPassword(id int64, passwordHash string) error {
	if id <= 0 {
		return fmt.Errorf("更新用户密码：ID必须为正数，当前值为%d", id)
	}
	if passwordHash == "" {
		return fmt.Errorf("更新用户密码：密码哈希不能为空")
	}
	_, err := s.DB.ExecContext(context.Background(), "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	return nil
}

func (s *Store) BeginTx() (*Tx, error) {
	t, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &Tx{Tx: t}, nil
}

type Tx struct {
	Tx *sql.Tx
}

func (tx *Tx) CreateRefreshToken(userID int64, tokenHash, familyID string, expiresAt time.Time) (*model.RefreshToken, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("创建刷新令牌：用户ID必须为正数")
	}
	if tokenHash == "" || familyID == "" {
		return nil, fmt.Errorf("创建刷新令牌：需要令牌哈希和家族ID")
	}
	result, err := tx.Tx.ExecContext(context.Background(),
		"INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at) VALUES (?, ?, ?, ?)",
		userID, tokenHash, familyID, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create refresh token get id: %w", err)
	}
	rt := &model.RefreshToken{}
	err = tx.Tx.QueryRowContext(context.Background(),
		"SELECT id, user_id, token_hash, family_id, revoked, expires_at, created_at FROM refresh_tokens WHERE id = ?",
		id,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.Revoked, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create refresh token readback: %w", err)
	}
	return rt, nil
}

func (tx *Tx) RevokeRefreshToken(id int64) error {
	if id <= 0 {
		return fmt.Errorf("吊销刷新令牌：ID必须为正数")
	}
	result, err := tx.Tx.ExecContext(context.Background(),
		"UPDATE refresh_tokens SET revoked = 1 WHERE id = ? AND revoked = 0", id,
	)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke refresh token rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("吊销刷新令牌：令牌已被吊销或不存在")
	}
	return nil
}

func (tx *Tx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (tx *Tx) Rollback() error {
	if err := tx.Tx.Rollback(); err != nil {
		return fmt.Errorf("rollback tx: %w", err)
	}
	return nil
}

func (s *Store) CreateRefreshToken(userID int64, tokenHash, familyID string, expiresAt time.Time) (*model.RefreshToken, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("创建刷新令牌：用户ID必须为正数")
	}
	if tokenHash == "" || familyID == "" {
		return nil, fmt.Errorf("创建刷新令牌：需要令牌哈希和家族ID")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at) VALUES (?, ?, ?, ?)",
		userID, tokenHash, familyID, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create refresh token get id: %w", err)
	}
	return s.RefreshTokenByID(id)
}

func (s *Store) RefreshTokenByHash(tokenHash string) (*model.RefreshToken, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("通过哈希查找刷新令牌：令牌哈希不能为空")
	}
	rt := &model.RefreshToken{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, token_hash, family_id, revoked, expires_at, created_at FROM refresh_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.Revoked, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("refresh token by hash: %w", err)
	}
	return rt, nil
}

func (s *Store) RefreshTokenByID(id int64) (*model.RefreshToken, error) {
	if id <= 0 {
		return nil, fmt.Errorf("通过ID查找刷新令牌：ID必须为正数")
	}
	rt := &model.RefreshToken{}
	err := s.DB.QueryRowContext(context.Background(),
		"SELECT id, user_id, token_hash, family_id, revoked, expires_at, created_at FROM refresh_tokens WHERE id = ?",
		id,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.Revoked, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("refresh token by id: %w", err)
	}
	return rt, nil
}

func (s *Store) RevokeRefreshToken(id int64) error {
	if id <= 0 {
		return fmt.Errorf("吊销刷新令牌：ID必须为正数")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"UPDATE refresh_tokens SET revoked = 1 WHERE id = ? AND revoked = 0", id,
	)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke refresh token rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("吊销刷新令牌：令牌已被吊销或不存在")
	}
	return nil
}

func (s *Store) RevokeTokenFamily(familyID string) error {
	if familyID == "" {
		return fmt.Errorf("吊销令牌家族：家族ID不能为空")
	}
	_, err := s.DB.ExecContext(context.Background(), "UPDATE refresh_tokens SET revoked = 1 WHERE family_id = ?", familyID)
	if err != nil {
		return fmt.Errorf("revoke token family: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredRefreshTokens() (int64, error) {
	result, err := s.DB.ExecContext(context.Background(), "DELETE FROM refresh_tokens WHERE expires_at < NOW()")
	if err != nil {
		return 0, fmt.Errorf("delete expired refresh tokens: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) UserExists(username string) (bool, error) {
	if strings.TrimSpace(username) == "" {
		return false, fmt.Errorf("检查用户是否存在：用户名不能为空")
	}
	var exists bool
	err := s.DB.QueryRowContext(context.Background(), "SELECT EXISTS(SELECT 1 FROM users WHERE username = ?)", username).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("user exists: %w", err)
	}
	return exists, nil
}

func (tx *Tx) UpdateUserStorageUsed(userID int64, delta int64) error {
	userID, err := mustBePositive(userID)
	if err != nil {
		return fmt.Errorf("update storage used tx: %w", err)
	}
	_, err = tx.Tx.ExecContext(context.Background(), "UPDATE users SET storage_used = GREATEST(storage_used + ?, 0) WHERE id = ?", delta, userID)
	if err != nil {
		return fmt.Errorf("update storage used tx: %w", err)
	}
	return nil
}

func (s *Store) UpdateUserStorageUsed(userID int64, delta int64) error {
	userID, err := mustBePositive(userID)
	if err != nil {
		return fmt.Errorf("update storage used: %w", err)
	}
	_, err = s.DB.ExecContext(context.Background(), "UPDATE users SET storage_used = GREATEST(storage_used + ?, 0) WHERE id = ?", delta, userID)
	if err != nil {
		return fmt.Errorf("update storage used: %w", err)
	}
	return nil
}

func (s *Store) AllUserIDs() ([]int64, error) {
	rows, err := s.DB.QueryContext(context.Background(), "SELECT id FROM users")
	if err != nil {
		return nil, fmt.Errorf("all user ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("all user ids scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("all user ids iterate: %w", err)
	}
	return ids, nil
}
