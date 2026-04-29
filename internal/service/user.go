package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"networkdisk/internal/config"
	"networkdisk/internal/model"
	"networkdisk/internal/store"
)

type pendingTOTPEntry struct {
	secret    string
	createdAt time.Time
}

type UserService struct {
	store        *store.Store
	config       atomic.Value
	pendingTOTP  map[int64]pendingTOTPEntry
	pendingMu    sync.Mutex
	totpAttempts map[int64]totpAttempt
	totpMu       sync.Mutex
}

type totpAttempt struct {
	count   int
	resetAt time.Time
}

func (svc *UserService) getCfg() *config.Config {
	return svc.config.Load().(*config.Config)
}

var (
	ErrUsernameTaken      = errors.New("用户名已被占用")
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrTokenRevoked       = errors.New("刷新令牌已吊销")
	ErrTokenExpired       = errors.New("刷新令牌已过期")
	ErrTOTPRequired       = errors.New("需要两步验证码")
	ErrInvalidTOTP        = errors.New("两步验证码无效")
)

func NewUserService(s *store.Store, cfg *config.Config) *UserService {
	svc := &UserService{
		store:        s,
		pendingTOTP:  make(map[int64]pendingTOTPEntry),
		totpAttempts: make(map[int64]totpAttempt),
	}
	svc.config.Store(cfg)
	return svc
}

func (svc *UserService) UpdateConfig(cfg *config.Config) {
	svc.config.Store(cfg)
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
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), svc.getCfg().Auth.BcryptCost)
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
	TOTPCode string `json:"totp_code"`
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
	if user.TOTPSecret != "" {
		if in.TOTPCode == "" {
			return user, nil, ErrTOTPRequired
		}
		svc.totpMu.Lock()
		attempt, _ := svc.totpAttempts[user.ID]
		if time.Now().After(attempt.resetAt) {
			attempt = totpAttempt{resetAt: time.Now().Add(15 * time.Minute)}
		}
		if attempt.count >= 5 {
			svc.totpAttempts[user.ID] = attempt
			svc.totpMu.Unlock()
			return nil, nil, fmt.Errorf("两步验证尝试次数过多，请15分钟后重试")
		}
		attempt.count++
		svc.totpAttempts[user.ID] = attempt
		svc.totpMu.Unlock()
		if !totp.Validate(in.TOTPCode, user.TOTPSecret) {
			return nil, nil, ErrInvalidTOTP
		}
		svc.totpMu.Lock()
		delete(svc.totpAttempts, user.ID)
		svc.totpMu.Unlock()
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
	_, err = tx.CreateRefreshToken(user.ID, newTokenHash, rt.FamilyID, time.Now().Add(svc.getCfg().RefreshExpireDuration()))
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
		return fmt.Errorf("logout lookup token: %w", err)
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
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), svc.getCfg().Auth.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	tx, err := svc.store.BeginTx()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := tx.UpdateUserPassword(userID, string(hash)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if err := tx.RevokeUserTokens(userID); err != nil {
		return fmt.Errorf("revoke user tokens: %w", err)
	}
	return tx.Commit()
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
	_, err = svc.store.CreateRefreshToken(user.ID, tokenHash, familyID, time.Now().Add(svc.getCfg().RefreshExpireDuration()))
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
	expiresAt := now.Add(svc.getCfg().JWTExpireDuration())
	claims := jwt.MapClaims{
		"iss": "networkdisk",
		"aud": "networkdisk",
		"sub": fmt.Sprintf("%d", user.ID),
		"usr": user.Username,
		"iat": now.Unix(),
		"exp": expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(svc.getCfg().Auth.JWTSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (svc *UserService) GenerateTOTP(userID int64) (string, string, error) {
	user, err := svc.store.UserByID(userID)
	if err != nil {
		return "", "", fmt.Errorf("generate totp: %w", err)
	}
	if user.TOTPSecret != "" {
		return "", "", fmt.Errorf("两步验证已开启")
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      svc.getCfg().Auth.TOTPIssuer,
		AccountName: user.Username,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp key: %w", err)
	}
	svc.pendingMu.Lock()
	svc.pendingTOTP[userID] = pendingTOTPEntry{secret: key.Secret(), createdAt: time.Now()}
	svc.pendingMu.Unlock()
	return key.Secret(), key.URL(), nil
}

func (svc *UserService) EnableTOTP(userID int64, code string) error {
	if err := svc.checkTOTPRateLimit(userID); err != nil {
		return err
	}
	svc.pendingMu.Lock()
	entry, ok := svc.pendingTOTP[userID]
	delete(svc.pendingTOTP, userID)
	svc.pendingMu.Unlock()
	if !ok || entry.secret == "" {
		return fmt.Errorf("请先获取两步验证密钥")
	}
	if !totp.Validate(code, entry.secret) {
		svc.recordTOTPAttempt(userID)
		return fmt.Errorf("验证码无效，请确认扫描了正确的二维码")
	}
	svc.resetTOTPAttempts(userID)
	return svc.store.SetTOTPSecret(userID, entry.secret)
}

func (svc *UserService) DisableTOTP(userID int64, code string) error {
	if err := svc.checkTOTPRateLimit(userID); err != nil {
		return err
	}
	secret, err := svc.store.UserTOTPSecret(userID)
	if err != nil {
		return fmt.Errorf("disable totp: %w", err)
	}
	if secret == "" {
		return fmt.Errorf("两步验证未开启")
	}
	if !totp.Validate(code, secret) {
		svc.recordTOTPAttempt(userID)
		return fmt.Errorf("验证码无效")
	}
	svc.resetTOTPAttempts(userID)
	return svc.store.SetTOTPSecret(userID, "")
}

const maxTOTPAttempts = 5
const totpRateLimitReset = 15 * time.Minute

func (svc *UserService) checkTOTPRateLimit(userID int64) error {
	svc.totpMu.Lock()
	defer svc.totpMu.Unlock()
	attempt, exists := svc.totpAttempts[userID]
	now := time.Now()
	if exists && attempt.count >= maxTOTPAttempts && !now.After(attempt.resetAt) {
		return fmt.Errorf("尝试次数过多，请15分钟后再试")
	}
	if !exists || now.After(attempt.resetAt) {
		svc.totpAttempts[userID] = totpAttempt{resetAt: now.Add(totpRateLimitReset)}
	}
	return nil
}

func (svc *UserService) recordTOTPAttempt(userID int64) {
	svc.totpMu.Lock()
	defer svc.totpMu.Unlock()
	attempt := svc.totpAttempts[userID]
	attempt.count++
	svc.totpAttempts[userID] = attempt
}

func (svc *UserService) resetTOTPAttempts(userID int64) {
	svc.totpMu.Lock()
	delete(svc.totpAttempts, userID)
	svc.totpMu.Unlock()
}

func (svc *UserService) CleanupTOTPState() {
	svc.pendingMu.Lock()
	for id, entry := range svc.pendingTOTP {
		if time.Since(entry.createdAt) > 30*time.Minute {
			delete(svc.pendingTOTP, id)
		}
	}
	svc.pendingMu.Unlock()

	svc.totpMu.Lock()
	for id, attempt := range svc.totpAttempts {
		if time.Now().After(attempt.resetAt) && attempt.count >= maxTOTPAttempts {
			delete(svc.totpAttempts, id)
		}
	}
	svc.totpMu.Unlock()
}

func (svc *UserService) VerifyLoginTOTP(userID int64, code string) error {
	secret, err := svc.store.UserTOTPSecret(userID)
	if err != nil {
		return fmt.Errorf("verify login totp: %w", err)
	}
	if secret == "" {
		return nil
	}
	if code == "" {
		return fmt.Errorf("需要两步验证码")
	}
	if !totp.Validate(code, secret) {
		return fmt.Errorf("两步验证码无效")
	}
	return nil
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
