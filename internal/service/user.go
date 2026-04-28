package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"networkdisk/internal/config"
	"networkdisk/internal/model"
	"networkdisk/internal/store"
)

type UserService struct {
	store  *store.Store
	config *config.Config
}

var (
	ErrUsernameTaken      = errors.New("用户名已被占用")
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrTokenRevoked       = errors.New("刷新令牌已吊销")
	ErrTokenExpired       = errors.New("刷新令牌已过期")
)

func NewUserService(s *store.Store, cfg *config.Config) *UserService {
	return &UserService{store: s, config: cfg}
}

func (svc *UserService) UpdateConfig(cfg *config.Config) {
	svc.config = cfg
}

type RegisterInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (svc *UserService) Register(in RegisterInput) (*model.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	if len(in.Username) < 3 || len(in.Username) > 64 {
		return nil, fmt.Errorf("用户名长度必须在3-64个字符之间")
	}
	if len(in.Password) < 6 {
		return nil, fmt.Errorf("密码长度不能少于6个字符")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), svc.config.Auth.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user, err := svc.store.CreateUser(in.Username, string(hash))
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"-"`
	ExpiresIn    int64  `json:"expires_in"`
	CSRFToken    string `json:"csrf_token"`
}

func (svc *UserService) Login(in LoginInput) (*model.User, *TokenPair, error) {
	in.Username = strings.TrimSpace(in.Username)
	user, err := svc.store.UserByUsername(in.Username)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	tokens, err := svc.issueTokens(user)
	if err != nil {
		return nil, nil, fmt.Errorf("issue tokens: %w", err)
	}
	return user, tokens, nil
}

func (svc *UserService) RefreshAccessToken(refreshToken string) (*model.User, *TokenPair, error) {
	tokenHash := hashToken(refreshToken)
	rt, err := svc.store.RefreshTokenByHash(tokenHash)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if rt.Revoked {
		if err := svc.store.RevokeTokenFamily(rt.FamilyID); err != nil {
			return nil, nil, fmt.Errorf("revoke token family: %w", err)
		}
		return nil, nil, ErrTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, nil, ErrTokenExpired
	}

	tx, err := svc.store.BeginTx()
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := tx.RevokeRefreshToken(rt.ID); err != nil {
		return nil, nil, fmt.Errorf("revoke old token: %w", err)
	}

	user, err := svc.store.UserByID(rt.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("lookup user: %w", err)
	}

	newRefreshToken, err := generateToken()
	if err != nil {
		return nil, nil, fmt.Errorf("generate refresh token: %w", err)
	}
	newTokenHash := hashToken(newRefreshToken)
	_, err = tx.CreateRefreshToken(user.ID, newTokenHash, rt.FamilyID, time.Now().Add(svc.config.RefreshExpireDuration()))
	if err != nil {
		return nil, nil, fmt.Errorf("store refresh token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	accessToken, expiresAt, err := svc.generateAccessToken(user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate access token: %w", err)
	}

	csrfToken, err := generateToken()
	if err != nil {
		return nil, nil, fmt.Errorf("generate csrf token: %w", err)
	}
	return user, &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    int64(time.Until(expiresAt).Seconds()),
		CSRFToken:    csrfToken,
	}, nil
}

func (svc *UserService) Logout(refreshToken string) error {
	tokenHash := hashToken(refreshToken)
	rt, err := svc.store.RefreshTokenByHash(tokenHash)
	if err != nil {
		return nil
	}
	if err := svc.store.RevokeRefreshToken(rt.ID); err != nil {
		return fmt.Errorf("logout revoke: %w", err)
	}
	return nil
}

func (svc *UserService) ChangePassword(userID int64, oldPassword, newPassword string) error {
	if len(newPassword) < 6 {
		return fmt.Errorf("密码长度不能少于6个字符")
	}
	user, err := svc.store.UserByID(userID)
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), svc.config.Auth.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := svc.store.UpdateUserPassword(userID, string(hash)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

func (svc *UserService) UserByID(id int64) (*model.User, error) {
	return svc.store.UserByID(id)
}

func (svc *UserService) issueTokens(user *model.User) (*TokenPair, error) {
	accessToken, expiresAt, err := svc.generateAccessToken(user)
	if err != nil {
		return nil, err
	}
	refreshToken, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	tokenHash := hashToken(refreshToken)
	familyID := refreshToken[:36]
	_, err = svc.store.CreateRefreshToken(user.ID, tokenHash, familyID, time.Now().Add(svc.config.RefreshExpireDuration()))
	if err != nil {
		return nil, err
	}
	csrfToken, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate csrf token: %w", err)
	}
	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(time.Until(expiresAt).Seconds()),
		CSRFToken:    csrfToken,
	}, nil
}

func (svc *UserService) generateAccessToken(user *model.User) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(svc.config.JWTExpireDuration())
	claims := jwt.MapClaims{
		"iss": "networkdisk",
		"aud": "networkdisk",
		"sub": fmt.Sprintf("%d", user.ID),
		"usr": user.Username,
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(svc.config.Auth.JWTSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
