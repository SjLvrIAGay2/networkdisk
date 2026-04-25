package store

import (
	"time"

	"networkdisk/internal/model"
)

func (s *Store) CreateUser(username, passwordHash string) (*model.User, error) {
	result, err := s.DB.Exec(
		"INSERT INTO users (username, password_hash) VALUES (?, ?)",
		username, passwordHash,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.UserByID(id)
}

func (s *Store) UserByUsername(username string) (*model.User, error) {
	user := &model.User{}
	err := s.DB.QueryRow(
		"SELECT id, username, password_hash, created_at, updated_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) UserByID(id int64) (*model.User, error) {
	user := &model.User{}
	err := s.DB.QueryRow(
		"SELECT id, username, password_hash, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) UpdateUserPassword(id int64, passwordHash string) error {
	_, err := s.DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id)
	return err
}

func (s *Store) CreateRefreshToken(userID int64, tokenHash, familyID string, expiresAt time.Time) (*model.RefreshToken, error) {
	result, err := s.DB.Exec(
		"INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at) VALUES (?, ?, ?, ?)",
		userID, tokenHash, familyID, expiresAt,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.RefreshTokenByID(id)
}

func (s *Store) RefreshTokenByHash(tokenHash string) (*model.RefreshToken, error) {
	rt := &model.RefreshToken{}
	err := s.DB.QueryRow(
		"SELECT id, user_id, token_hash, family_id, revoked, expires_at, created_at FROM refresh_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.Revoked, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

func (s *Store) RefreshTokenByID(id int64) (*model.RefreshToken, error) {
	rt := &model.RefreshToken{}
	err := s.DB.QueryRow(
		"SELECT id, user_id, token_hash, family_id, revoked, expires_at, created_at FROM refresh_tokens WHERE id = ?",
		id,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.FamilyID, &rt.Revoked, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

func (s *Store) RevokeRefreshToken(id int64) error {
	_, err := s.DB.Exec("UPDATE refresh_tokens SET revoked = 1 WHERE id = ?", id)
	return err
}

func (s *Store) RevokeTokenFamily(familyID string) error {
	_, err := s.DB.Exec("UPDATE refresh_tokens SET revoked = 1 WHERE family_id = ?", familyID)
	return err
}

func (s *Store) DeleteExpiredRefreshTokens() (int64, error) {
	result, err := s.DB.Exec("DELETE FROM refresh_tokens WHERE expires_at < NOW()")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) UserExists(username string) (bool, error) {
	var exists bool
	err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE username = ?)", username).Scan(&exists)
	return exists, err
}
