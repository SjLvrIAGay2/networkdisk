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
	Host            string `toml:"host"`
	Port            int    `toml:"port"`
	ReadTimeout     string `toml:"read_timeout"`
	WriteTimeout    string `toml:"write_timeout"`
	ShutdownTimeout string `toml:"shutdown_timeout"`
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
	Root string `toml:"root"`
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
}

type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Storage  StorageConfig  `toml:"storage"`
	Auth     AuthConfig     `toml:"auth"`
	Log      LogConfig      `toml:"log"`
}

var (
	global *Config
	mu     sync.RWMutex
)

var reloadWhitelist = map[string]bool{
	"auth.jwt_secret":    false,
	"database.host":      false,
	"database.port":      false,
	"database.user":      false,
	"database.password":  false,
	"database.database":  false,
	"database.max_open_conns": false,
	"database.max_idle_conns": false,
	"database.conn_max_lifetime": false,
}

var envMapping = map[string]string{
	"server.host":               "NETWORKDISK_SERVER_HOST",
	"server.port":               "NETWORKDISK_SERVER_PORT",
	"server.read_timeout":       "NETWORKDISK_SERVER_READ_TIMEOUT",
	"server.write_timeout":      "NETWORKDISK_SERVER_WRITE_TIMEOUT",
	"server.shutdown_timeout":   "NETWORKDISK_SERVER_SHUTDOWN_TIMEOUT",
	"database.host":             "NETWORKDISK_DATABASE_HOST",
	"database.port":             "NETWORKDISK_DATABASE_PORT",
	"database.user":             "NETWORKDISK_DATABASE_USER",
	"database.password":         "NETWORKDISK_DATABASE_PASSWORD",
	"database.database":         "NETWORKDISK_DATABASE_DATABASE",
	"database.max_open_conns":   "NETWORKDISK_DATABASE_MAX_OPEN_CONNS",
	"database.max_idle_conns":   "NETWORKDISK_DATABASE_MAX_IDLE_CONNS",
	"database.conn_max_lifetime": "NETWORKDISK_DATABASE_CONN_MAX_LIFETIME",
	"storage.root":              "NETWORKDISK_STORAGE_ROOT",
	"auth.jwt_secret":           "NETWORKDISK_AUTH_JWT_SECRET",
	"auth.jwt_expire":           "NETWORKDISK_AUTH_JWT_EXPIRE",
	"auth.refresh_expire":       "NETWORKDISK_AUTH_REFRESH_EXPIRE",
	"auth.bcrypt_cost":          "NETWORKDISK_AUTH_BCRYPT_COST",
	"log.level":                 "NETWORKDISK_LOG_LEVEL",
	"log.format":                "NETWORKDISK_LOG_FORMAT",
}

func Load(path string) (*Config, error) {
	cfg, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	mu.Lock()
	global = cfg
	mu.Unlock()
	return cfg, nil
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
		"read_timeout":      "ReadTimeout",
		"write_timeout":     "WriteTimeout",
		"shutdown_timeout":  "ShutdownTimeout",
		"max_open_conns":    "MaxOpenConns",
		"max_idle_conns":    "MaxIdleConns",
		"conn_max_lifetime": "ConnMaxLifetime",
		"jwt_secret":        "JWTSecret",
		"jwt_expire":        "JWTExpire",
		"refresh_expire":    "RefreshExpire",
		"bcrypt_cost":       "BcryptCost",
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
	case reflect.Int:
		var ival int
		fmt.Sscanf(value, "%d", &ival)
		field.SetInt(int64(ival))
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
	new.Auth.JWTSecret = old.Auth.JWTSecret
	new.Database.Host = old.Database.Host
	new.Database.Port = old.Database.Port
	new.Database.User = old.Database.User
	new.Database.Password = old.Database.Password
	new.Database.Database = old.Database.Database
	new.Database.MaxOpenConns = old.Database.MaxOpenConns
	new.Database.MaxIdleConns = old.Database.MaxIdleConns
	new.Database.ConnMaxLifetime = old.Database.ConnMaxLifetime
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

func (c *Config) ParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(fmt.Sprintf("invalid duration %s: %v", s, err))
	}
	return d
}

func (c *Config) JWTExpireDuration() time.Duration {
	return c.ParseDuration(c.Auth.JWTExpire)
}

func (c *Config) RefreshExpireDuration() time.Duration {
	return c.ParseDuration(c.Auth.RefreshExpire)
}

func (c *Config) ReadTimeoutDuration() time.Duration {
	return c.ParseDuration(c.Server.ReadTimeout)
}

func (c *Config) WriteTimeoutDuration() time.Duration {
	return c.ParseDuration(c.Server.WriteTimeout)
}

func (c *Config) ShutdownTimeoutDuration() time.Duration {
	return c.ParseDuration(c.Server.ShutdownTimeout)
}

func (c *Config) ConnMaxLifetimeDuration() time.Duration {
	return c.ParseDuration(c.Database.ConnMaxLifetime)
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
