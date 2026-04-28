package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	"networkdisk/internal/config"
	"networkdisk/internal/model"
	"networkdisk/internal/store"
)

const shareVerifyTokenTTL = 5 * time.Minute

type ShareService struct {
	store           *store.Store
	fileSvc         *FileService
	cfg             atomic.Value
	mu              sync.Mutex
	passwordLimiter map[string]*passwordAttempts
}

type passwordAttempts struct {
	count     int
	resetAt   time.Time
}

var (
	ErrShareNotFound    = errors.New("分享链接不存在")
	ErrShareExpired     = errors.New("分享链接已过期")
	ErrShareMaxReached  = errors.New("分享链接已达到下载上限")
	ErrSharePassword    = errors.New("分享密码错误")
	ErrTooManyAttempts  = errors.New("密码尝试次数过多")
)

func NewShareService(s *store.Store, fs *FileService, cfg *config.Config) *ShareService {
	svc := &ShareService{
		store:           s,
		fileSvc:         fs,
		passwordLimiter: make(map[string]*passwordAttempts),
	}
	svc.cfg.Store(cfg)
	return svc
}

func (svc *ShareService) getCfg() *config.Config {
	return svc.cfg.Load().(*config.Config)
}

func (svc *ShareService) CreateShare(fileID int64, ownerID int64, password string, expireAt *time.Time, maxDownloads int) (*model.Share, error) {
	f, err := svc.store.FileByID(fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("create share lookup file: %w", err)
	}
	if f.UserID != ownerID || f.IsDeleted {
		return nil, ErrFileNotFound
	}
	if f.IsDir {
		return nil, fmt.Errorf("不支持分享文件夹")
	}

	token, err := generateShareToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	var passwordHash string
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), svc.getCfg().ShareBcryptCost())
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		passwordHash = string(hash)
	}

	if expireAt == nil && svc.getCfg().Share.DefaultExpire != "" {
		d, err := parseShareDuration(svc.getCfg().Share.DefaultExpire)
		if err == nil && d > 0 {
			t := time.Now().Add(d)
			expireAt = &t
		}
	}

	share := &model.Share{
		Token:        token,
		FileID:       fileID,
		OwnerID:      ownerID,
		PasswordHash: passwordHash,
		ExpireAt:     expireAt,
		MaxDownloads: int64(maxDownloads),
	}
	return svc.store.CreateShare(share)
}

func parseShareDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err == nil {
		return d, nil
	}
	days, err := strconv.Atoi(s)
	if err == nil && days > 0 {
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid share duration: %s", s)
}

func (svc *ShareService) UpdateConfig(cfg *config.Config) {
	svc.cfg.Store(cfg)
}

func (svc *ShareService) MyShares(ownerID int64) ([]*model.Share, error) {
	return svc.store.SharesByOwner(ownerID)
}

func (svc *ShareService) DeleteShare(id int64, ownerID int64) error {
	share, err := svc.store.ShareByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrShareNotFound
		}
		return fmt.Errorf("delete share lookup: %w", err)
	}
	if share.OwnerID != ownerID {
		return ErrShareNotFound
	}
	return svc.store.DeleteShare(id)
}

func (svc *ShareService) GetShareByToken(token string) (*model.Share, *model.File, error) {
	share, err := svc.store.ShareByToken(token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrShareNotFound
		}
		return nil, nil, fmt.Errorf("get share: %w", err)
	}
	if share.ExpireAt != nil && time.Now().After(*share.ExpireAt) {
		return nil, nil, ErrShareExpired
	}
	if share.MaxDownloads > 0 && share.ViewCount >= share.MaxDownloads {
		return nil, nil, ErrShareMaxReached
	}
	f, err := svc.store.FileByID(share.FileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrShareNotFound
		}
		return nil, nil, fmt.Errorf("get share file: %w", err)
	}
	if f.IsDeleted {
		return nil, nil, ErrShareNotFound
	}
	return share, f, nil
}

func (svc *ShareService) VerifySharePassword(token string, password string, clientIP string, maxAttempts int) (*model.File, string, error) {
	svc.mu.Lock()
	attempts, exists := svc.passwordLimiter[clientIP]
	now := time.Now()
	if !exists || now.After(attempts.resetAt) {
		attempts = &passwordAttempts{resetAt: now.Add(svc.getCfg().SharePasswordRateLimitResetDuration())}
		svc.passwordLimiter[clientIP] = attempts
	}
	if exists && attempts.count >= maxAttempts && !now.After(attempts.resetAt) {
		svc.mu.Unlock()
		return nil, "", ErrTooManyAttempts
	}
	attempts.count++
	svc.mu.Unlock()

	share, err := svc.store.ShareByToken(token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrShareNotFound
		}
		return nil, "", fmt.Errorf("verify password lookup: %w", err)
	}
	if share.PasswordHash == "" {
		return nil, "", fmt.Errorf("此分享无密码")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(password)); err != nil {
		return nil, "", ErrSharePassword
	}
	svc.resetPasswordLimiter(clientIP)
	verifyToken, err := svc.signVerifyToken(token)
	if err != nil {
		return nil, "", fmt.Errorf("sign verify token: %w", err)
	}
	f, err := svc.store.FileByID(share.FileID)
	if err != nil {
		return nil, "", fmt.Errorf("verify password file: %w", err)
	}
	return f, verifyToken, nil
}

func (svc *ShareService) DownloadSharedFile(token string) (*model.File, *model.Share, error) {
	share, f, err := svc.GetShareByToken(token)
	if err != nil {
		return nil, nil, err
	}
	if share.PasswordHash != "" {
		return nil, nil, fmt.Errorf("需要密码验证")
	}
	if err := svc.store.IncrementShareViewCountIfUnderMax(share.ID, share.MaxDownloads); err != nil {
		return nil, nil, fmt.Errorf("increment view count: %w", err)
	}
	return f, share, nil
}

func (svc *ShareService) DownloadWithVerifyToken(token string, verifyToken string) (*model.File, error) {
	if err := svc.validateVerifyToken(token, verifyToken); err != nil {
		return nil, err
	}
	share, err := svc.store.ShareByToken(token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrShareNotFound
		}
		return nil, fmt.Errorf("download with verify token: %w", err)
	}
	if share.ExpireAt != nil && time.Now().After(*share.ExpireAt) {
		return nil, ErrShareExpired
	}
	if err := svc.store.IncrementShareViewCountIfUnderMax(share.ID, share.MaxDownloads); err != nil {
		return nil, ErrShareMaxReached
	}
	f, err := svc.store.FileByID(share.FileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrShareNotFound
		}
		return nil, fmt.Errorf("download with verify token file: %w", err)
	}
	if f.IsDeleted {
		return nil, ErrShareNotFound
	}
	return f, nil
}

func (svc *ShareService) signVerifyToken(shareToken string) (string, error) {
	expiry := time.Now().Add(shareVerifyTokenTTL).Unix()
	expiryStr := strconv.FormatInt(expiry, 10)
	payload := expiryStr + "|" + shareToken
	mac := hmac.New(sha256.New, []byte(svc.getCfg().Auth.JWTSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	encoded := base64.RawURLEncoding.EncodeToString([]byte(expiryStr + "|" + shareToken + "|" + sig))
	return encoded, nil
}

func (svc *ShareService) validateVerifyToken(shareToken string, verifyToken string) error {
	data, err := base64.RawURLEncoding.DecodeString(verifyToken)
	if err != nil {
		return fmt.Errorf("验证令牌无效")
	}
	parts := strings.SplitN(string(data), "|", 3)
	if len(parts) != 3 {
		return fmt.Errorf("验证令牌无效")
	}
	expiryUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("验证令牌无效")
	}
	if time.Now().Unix() > expiryUnix {
		return fmt.Errorf("验证令牌已过期，请重新验证密码")
	}
	if parts[1] != shareToken {
		return fmt.Errorf("验证令牌不匹配")
	}
	payload := parts[0] + "|" + parts[1]
	mac := hmac.New(sha256.New, []byte(svc.getCfg().Auth.JWTSecret))
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return fmt.Errorf("验证令牌无效")
	}
	return nil
}

func (svc *ShareService) GetTempDownload(token string) (*model.TempDownload, *model.File, error) {
	td, err := svc.store.TempDownloadByToken(token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrShareNotFound
		}
		return nil, nil, fmt.Errorf("get temp download: %w", err)
	}
	if time.Now().After(td.ExpireAt) {
		return nil, nil, fmt.Errorf("下载链接已过期")
	}
	f, err := svc.store.FileByID(td.FileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrFileNotFound
		}
		return nil, nil, fmt.Errorf("get temp download file: %w", err)
	}
	if f.IsDeleted {
		return nil, nil, ErrFileNotFound
	}
	return td, f, nil
}

func (svc *ShareService) CleanExpiredTempDownloads() (int64, error) {
	return svc.store.DeleteExpiredTempDownloads()
}

func (svc *ShareService) resetPasswordLimiter(clientIP string) {
	svc.mu.Lock()
	delete(svc.passwordLimiter, clientIP)
	svc.mu.Unlock()
}

func generateShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
