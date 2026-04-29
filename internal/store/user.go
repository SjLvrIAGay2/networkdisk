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
		"SELECT id, username, password_hash, storage_used, totp_secret, created_at, updated_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.StorageUsed, &user.TOTPSecret, &user.CreatedAt, &user.UpdatedAt)
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
		"SELECT id, username, password_hash, storage_used, totp_secret, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.StorageUsed, &user.TOTPSecret, &user.CreatedAt, &user.UpdatedAt)
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

func (tx *Tx) UpdateUserPassword(userID int64, passwordHash string) error {
	if userID <= 0 {
		return fmt.Errorf("更新用户密码（事务）：ID必须为正数")
	}
	if passwordHash == "" {
		return fmt.Errorf("更新用户密码（事务）：密码哈希不能为空")
	}
	_, err := tx.Tx.ExecContext(context.Background(), "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, userID)
	if err != nil {
		return fmt.Errorf("update user password tx: %w", err)
	}
	return nil
}

func (tx *Tx) RevokeUserTokens(userID int64) error {
	if userID <= 0 {
		return fmt.Errorf("吊销用户令牌（事务）：用户ID必须为正数")
	}
	_, err := tx.Tx.ExecContext(context.Background(), "UPDATE refresh_tokens SET revoked = 1 WHERE user_id = ? AND revoked = 0", userID)
	if err != nil {
		return fmt.Errorf("revoke user tokens tx: %w", err)
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
	return s.AllUserIDsPaginated(0, 0)
}

func (s *Store) AllUserIDsPaginated(limit, offset int) ([]int64, error) {
	query := "SELECT id FROM users ORDER BY id"
	var args []interface{}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := s.DB.QueryContext(context.Background(), query, args...)
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

func (s *Store) SetTOTPSecret(userID int64, secret string) error {
	if userID <= 0 {
		return fmt.Errorf("设置TOTP密钥：用户ID必须为正数")
	}
	_, err := s.DB.ExecContext(context.Background(), "UPDATE users SET totp_secret = ? WHERE id = ?", secret, userID)
	if err != nil {
		return fmt.Errorf("set totp secret: %w", err)
	}
	return nil
}

func (s *Store) RevokeUserTokens(userID int64) error {
	if userID <= 0 {
		return fmt.Errorf("吊销用户令牌：用户ID必须为正数")
	}
	_, err := s.DB.ExecContext(context.Background(), "UPDATE refresh_tokens SET revoked = 1 WHERE user_id = ? AND revoked = 0", userID)
	if err != nil {
		return fmt.Errorf("revoke user tokens: %w", err)
	}
	return nil
}

func (s *Store) DistinctDeviceFamilies(userID int64) ([]*model.RefreshToken, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("distinct device families: user id must be positive")
	}
	rows, err := s.DB.QueryContext(context.Background(),
		`SELECT rf.id, rf.user_id, rf.token_hash, rf.family_id, rf.revoked, rf.expires_at, rf.created_at
		 FROM refresh_tokens rf
		 INNER JOIN (
		     SELECT family_id, MAX(created_at) AS latest
		     FROM refresh_tokens
		     WHERE user_id = ? AND revoked = 0 AND expires_at > NOW()
		     GROUP BY family_id
		 ) latest_rf ON rf.family_id = latest_rf.family_id AND rf.created_at = latest_rf.latest
		 WHERE rf.user_id = ? AND rf.revoked = 0 AND rf.expires_at > NOW()
		 ORDER BY rf.created_at DESC`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("distinct device families: %w", err)
	}
	defer rows.Close()
	var tokens []*model.RefreshToken
	for rows.Next() {
		rt := &model.RefreshToken{}
		if err := rows.Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.Revoked, &rt.ExpiresAt, &rt.CreatedAt); err != nil {
			return nil, fmt.Errorf("distinct device families scan: %w", err)
		}
		tokens = append(tokens, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("distinct device families iterate: %w", err)
	}
	return tokens, nil
}

func (s *Store) RevokeDeviceFamily(userID int64, familyID string) error {
	if userID <= 0 {
		return fmt.Errorf("revoke device family: user id must be positive")
	}
	if familyID == "" {
		return fmt.Errorf("revoke device family: family id must not be empty")
	}
	result, err := s.DB.ExecContext(context.Background(),
		"UPDATE refresh_tokens SET revoked = 1 WHERE user_id = ? AND family_id = ? AND revoked = 0",
		userID, familyID)
	if err != nil {
		return fmt.Errorf("revoke device family: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke device family rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("设备未找到或已吊销")
	}
	return nil
}

func (s *Store) UserTOTPSecret(userID int64) (string, error) {
	if userID <= 0 {
		return "", fmt.Errorf("获取TOTP密钥：用户ID必须为正数")
	}
	var secret string
	err := s.DB.QueryRowContext(context.Background(), "SELECT totp_secret FROM users WHERE id = ?", userID).Scan(&secret)
	if err != nil {
		return "", fmt.Errorf("user totp secret: %w", err)
	}
	return secret, nil
}
