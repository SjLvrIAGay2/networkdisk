package config

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Host             string `toml:"host"`
	Port             int    `toml:"port"`
	ReadTimeout      string `toml:"read_timeout"`
	WriteTimeout     string `toml:"write_timeout"`
	ShutdownTimeout  string `toml:"shutdown_timeout"`
	RateLimit        int    `toml:"rate_limit"`
	RateLimitWindow  string `toml:"rate_limit_window"`
}

type DatabaseConfig struct {
	Host            string `toml:"host"`
	Port            int    `toml:"port"`
	User            string `toml:"user"`
	Password        string `toml:"password"`
	Database        string `toml:"database"`
	MaxOpenConns    int    `toml:"max_open_conns"`
	MaxIdleConns    int    `toml:"max_idle_conns"`
	ConnMaxLifetime string `toml:"conn_max_lifetime"`
}

type StorageConfig struct {
	Root             string `toml:"root"`
	MaxFileSize      int64  `toml:"max_file_size"`
	ThumbnailMaxSize int64  `toml:"thumbnail_max_size"`
	ThumbnailQuality int    `toml:"thumbnail_quality"`
}

type UploadConfig struct {
	AllowedExtensions []string `toml:"allowed_extensions"`
	BlockedExtensions []string `toml:"blocked_extensions"`
	DetectMime        bool     `toml:"detect_mime"`
	OnNameConflict    string   `toml:"on_name_conflict"`
}

type AuthConfig struct {
	JWTSecret     string `toml:"jwt_secret"`
	JWTExpire     string `toml:"jwt_expire"`
	RefreshExpire string `toml:"refresh_expire"`
	BcryptCost    int    `toml:"bcrypt_cost"`
}

type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
	File   string `toml:"file"`
}

type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Storage  StorageConfig  `toml:"storage"`
	Upload   UploadConfig   `toml:"upload"`
	Auth     AuthConfig     `toml:"auth"`
	Log      LogConfig      `toml:"log"`
}

var (
	global *Config
	mu     sync.RWMutex
)

var defaults = map[string]interface{}{
	"server.host":               "0.0.0.0",
	"server.port":               8080,
	"server.read_timeout":       "30s",
	"server.write_timeout":      "60s",
	"server.shutdown_timeout":   "10s",
	"server.rate_limit":         10,
	"server.rate_limit_window":  "1m",
	"database.max_open_conns": 25,
	"database.max_idle_conns": 5,
	"database.conn_max_lifetime": "5m",
	"storage.max_file_size":    int64(100 << 20),
	"storage.thumbnail_max_size": int64(50 << 20),
	"storage.thumbnail_quality": 80,
	"upload.detect_mime":       true,
	"upload.on_name_conflict":  "rename",
	"auth.jwt_expire":          "15m",
	"auth.refresh_expire":      "7d",
	"auth.bcrypt_cost":         12,
	"log.level":                "info",
	"log.format":               "text",
}

var reloadWhitelist = map[string]bool{
	"auth.jwt_secret":             false,
	"database.host":               false,
	"database.port":               false,
	"database.user":               false,
	"database.password":           false,
	"database.database":           false,
	"database.max_open_conns":     false,
	"database.max_idle_conns":     false,
	"database.conn_max_lifetime": false,
}

var envMapping = map[string]string{
	"server.host":                  "NETWORKDISK_SERVER_HOST",
	"server.port":                  "NETWORKDISK_SERVER_PORT",
	"server.read_timeout":          "NETWORKDISK_SERVER_READ_TIMEOUT",
	"server.write_timeout":         "NETWORKDISK_SERVER_WRITE_TIMEOUT",
	"server.shutdown_timeout":      "NETWORKDISK_SERVER_SHUTDOWN_TIMEOUT",
	"server.rate_limit":            "NETWORKDISK_SERVER_RATE_LIMIT",
	"server.rate_limit_window":     "NETWORKDISK_SERVER_RATE_LIMIT_WINDOW",
	"database.host":                "NETWORKDISK_DATABASE_HOST",
	"database.port":                "NETWORKDISK_DATABASE_PORT",
	"database.user":                "NETWORKDISK_DATABASE_USER",
	"database.password":            "NETWORKDISK_DATABASE_PASSWORD",
	"database.database":            "NETWORKDISK_DATABASE_DATABASE",
	"database.max_open_conns":      "NETWORKDISK_DATABASE_MAX_OPEN_CONNS",
	"database.max_idle_conns":      "NETWORKDISK_DATABASE_MAX_IDLE_CONNS",
	"database.conn_max_lifetime":   "NETWORKDISK_DATABASE_CONN_MAX_LIFETIME",
	"storage.root":                 "NETWORKDISK_STORAGE_ROOT",
	"storage.max_file_size":        "NETWORKDISK_STORAGE_MAX_FILE_SIZE",
	"storage.thumbnail_max_size":   "NETWORKDISK_STORAGE_THUMBNAIL_MAX_SIZE",
	"storage.thumbnail_quality":    "NETWORKDISK_STORAGE_THUMBNAIL_QUALITY",
	"upload.allowed_extensions":    "NETWORKDISK_UPLOAD_ALLOWED_EXTENSIONS",
	"upload.blocked_extensions":    "NETWORKDISK_UPLOAD_BLOCKED_EXTENSIONS",
	"upload.detect_mime":           "NETWORKDISK_UPLOAD_DETECT_MIME",
	"upload.on_name_conflict":      "NETWORKDISK_UPLOAD_ON_NAME_CONFLICT",
	"auth.jwt_secret":              "NETWORKDISK_AUTH_JWT_SECRET",
	"auth.jwt_expire":              "NETWORKDISK_AUTH_JWT_EXPIRE",
	"auth.refresh_expire":          "NETWORKDISK_AUTH_REFRESH_EXPIRE",
	"auth.bcrypt_cost":             "NETWORKDISK_AUTH_BCRYPT_COST",
	"log.level":                    "NETWORKDISK_LOG_LEVEL",
	"log.format":                   "NETWORKDISK_LOG_FORMAT",
	"log.file":                     "NETWORKDISK_LOG_FILE",
}

func Load(path string) (*Config, error) {
	cfg, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	applyDefaults(cfg)
	applyEnvOverrides(cfg)
	mu.Lock()
	global = cfg
	mu.Unlock()
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	for key, val := range defaults {
		parts := strings.Split(key, ".")
		field := getNested(v, parts)
		if !field.IsValid() {
			continue
		}
		if isZero(field) {
			setFieldValueFromAny(field, val)
		}
	}
}

func isZero(field reflect.Value) bool {
	switch field.Kind() {
	case reflect.String:
		return field.String() == ""
	case reflect.Int, reflect.Int64:
		return field.Int() == 0
	case reflect.Bool:
		return !field.Bool()
	case reflect.Slice:
		return field.IsNil() || field.Len() == 0
	}
	return false
}

func setFieldValueFromAny(field reflect.Value, val interface{}) {
	switch field.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("%v", val))
	case reflect.Int, reflect.Int64:
		field.SetInt(reflect.ValueOf(val).Int())
	case reflect.Bool:
		field.SetBool(reflect.ValueOf(val).Bool())
	case reflect.Slice:
		sliceV := reflect.ValueOf(val)
		slice := reflect.MakeSlice(field.Type(), sliceV.Len(), sliceV.Len())
		for i := 0; i < sliceV.Len(); i++ {
			slice.Index(i).Set(sliceV.Index(i))
		}
		field.Set(slice)
	}
}

func loadFile(path string) (*Config, error) {
	cfg := &Config{}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode config file %s: %w", path, err)
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	for key, envVar := range envMapping {
		if val, ok := os.LookupEnv(envVar); ok && val != "" {
			setNested(v, strings.Split(key, "."), val)
		}
	}
}

func setNested(v reflect.Value, parts []string, value string) {
	if len(parts) == 0 {
		return
	}
	field := v.FieldByName(fieldName(parts[0]))
	if !field.IsValid() {
		return
	}
	if len(parts) == 1 {
		setFieldValue(field, value)
		return
	}
	if field.Kind() == reflect.Struct {
		setNested(field, parts[1:], value)
	}
}

func fieldName(tomlKey string) string {
	mapping := map[string]string{
		"read_timeout":       "ReadTimeout",
		"write_timeout":      "WriteTimeout",
		"shutdown_timeout":   "ShutdownTimeout",
		"max_open_conns":     "MaxOpenConns",
		"max_idle_conns":     "MaxIdleConns",
		"conn_max_lifetime":  "ConnMaxLifetime",
		"jwt_secret":         "JWTSecret",
		"jwt_expire":         "JWTExpire",
		"refresh_expire":     "RefreshExpire",
		"bcrypt_cost":        "BcryptCost",
		"max_file_size":      "MaxFileSize",
		"thumbnail_max_size": "ThumbnailMaxSize",
		"thumbnail_quality":  "ThumbnailQuality",
		"allowed_extensions": "AllowedExtensions",
		"blocked_extensions": "BlockedExtensions",
		"detect_mime":        "DetectMime",
		"on_name_conflict":   "OnNameConflict",
	}
	if mapped, ok := mapping[tomlKey]; ok {
		return mapped
	}
	words := strings.Split(tomlKey, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, "")
}

func setFieldValue(field reflect.Value, value string) {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int64:
		var ival int64
		fmt.Sscanf(value, "%d", &ival)
		field.SetInt(ival)
	case reflect.Bool:
		val := strings.ToLower(value)
		if val == "true" || val == "1" {
			field.SetBool(true)
		} else if val == "false" || val == "0" {
			field.SetBool(false)
		}
	case reflect.Slice:
		parts := strings.Split(value, ",")
		if len(parts) > 0 && parts[0] != "" {
			slice := reflect.MakeSlice(field.Type(), len(parts), len(parts))
			for i, p := range parts {
				slice.Index(i).SetString(strings.TrimSpace(p))
			}
			field.Set(slice)
		}
	}
}

func (c *Config) Clone() *Config {
	mu.RLock()
	defer mu.RUnlock()
	clone := *c
	return &clone
}

func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return global
}

func Reload(path string) (*Config, error) {
	newCfg, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	applyEnvOverrides(newCfg)

	mu.Lock()
	old := global
	mu.Unlock()

	mergeReloadable(old, newCfg)

	mu.Lock()
	copyProtectedFields(old, newCfg)
	global = newCfg
	mu.Unlock()

	return newCfg, nil
}

func mergeReloadable(old, new *Config) {
	ov := reflect.ValueOf(old).Elem()
	nv := reflect.ValueOf(new).Elem()
	for key, allowed := range reloadWhitelist {
		if allowed {
			continue
		}
		parts := strings.Split(key, ".")
		oldField := getNested(ov, parts)
		newField := getNested(nv, parts)
		if oldField.IsValid() && newField.IsValid() && newField.CanSet() {
			newField.Set(oldField)
		}
	}
}

func getNested(v reflect.Value, parts []string) reflect.Value {
	for _, part := range parts {
		v = v.FieldByName(fieldName(part))
	}
	return v
}

func copyProtectedFields(old, new *Config) {
	if old == nil {
		return
	}
	keyCopier := func(parts []string) {
		oldField := getNested(reflect.ValueOf(old).Elem(), parts)
		newField := getNested(reflect.ValueOf(new).Elem(), parts)
		if oldField.IsValid() && newField.IsValid() && newField.CanSet() {
			newField.Set(oldField)
		}
	}
	keyCopier([]string{"auth", "jwt_secret"})
	keyCopier([]string{"database", "host"})
	keyCopier([]string{"database", "port"})
	keyCopier([]string{"database", "user"})
	keyCopier([]string{"database", "password"})
	keyCopier([]string{"database", "database"})
	keyCopier([]string{"database", "max_open_conns"})
	keyCopier([]string{"database", "max_idle_conns"})
	keyCopier([]string{"database", "conn_max_lifetime"})
}

func StartReloadWatcher(path string, onReload func(*Config)) chan struct{} {
	done := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-sigCh:
				newCfg, err := Reload(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "config reload failed: %v\n", err)
					continue
				}
				if onReload != nil {
					onReload(newCfg)
				}
			case <-done:
				signal.Stop(sigCh)
				close(sigCh)
				return
			}
		}
	}()
	return done
}

func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		c.Database.User, c.Database.Password, c.Database.Host, c.Database.Port, c.Database.Database)
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

func (c *Config) JWTExpireDuration() time.Duration {
	d, err := parseDuration(c.Auth.JWTExpire)
	if err != nil {
		d = 15 * time.Minute
	}
	return d
}

func (c *Config) RefreshExpireDuration() time.Duration {
	d, err := parseDuration(c.Auth.RefreshExpire)
	if err != nil {
		d = 7 * 24 * time.Hour
	}
	return d
}

func (c *Config) ReadTimeoutDuration() time.Duration {
	d, err := parseDuration(c.Server.ReadTimeout)
	if err != nil {
		d = 30 * time.Second
	}
	return d
}

func (c *Config) WriteTimeoutDuration() time.Duration {
	d, err := parseDuration(c.Server.WriteTimeout)
	if err != nil {
		d = 60 * time.Second
	}
	return d
}

func (c *Config) ShutdownTimeoutDuration() time.Duration {
	d, err := parseDuration(c.Server.ShutdownTimeout)
	if err != nil {
		d = 10 * time.Second
	}
	return d
}

func (c *Config) RateLimitWindowDuration() time.Duration {
	d, err := parseDuration(c.Server.RateLimitWindow)
	if err != nil {
		d = time.Minute
	}
	return d
}

func (c *Config) ConnMaxLifetimeDuration() time.Duration {
	d, err := parseDuration(c.Database.ConnMaxLifetime)
	if err != nil {
		d = 5 * time.Minute
	}
	return d
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
