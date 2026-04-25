package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[server]
host = "127.0.0.1"
port = 8080
read_timeout = "10s"
write_timeout = "30s"
shutdown_timeout = "5s"

[database]
host = "db.local"
port = 3307
user = "tester"
password = "secret"
database = "testdb"
max_open_conns = 10
max_idle_conns = 5
conn_max_lifetime = "3m"

[storage]
root = "/tmp/storage"

[auth]
jwt_secret = "test-secret-key"
jwt_expire = "15m"
refresh_expire = "168h"
bcrypt_cost = 12

[log]
level = "debug"
format = "text"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("server.host = %s, want 127.0.0.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Database.Database != "testdb" {
		t.Errorf("database.database = %s, want testdb", cfg.Database.Database)
	}
	if cfg.Auth.BcryptCost != 12 {
		t.Errorf("auth.bcrypt_cost = %d, want 12", cfg.Auth.BcryptCost)
	}
	if cfg.Auth.JWTSecret != "test-secret-key" {
		t.Errorf("auth.jwt_secret = %s, want test-secret-key", cfg.Auth.JWTSecret)
	}
}

func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[server]
host = "0.0.0.0"
port = 3000
read_timeout = "10s"
write_timeout = "30s"
shutdown_timeout = "5s"

[database]
host = "oldhost"
port = 3306
user = "olduser"
password = "oldpass"
database = "olddb"
max_open_conns = 5
max_idle_conns = 3
conn_max_lifetime = "2m"

[storage]
root = "/tmp/old"

[auth]
jwt_secret = "abc"
jwt_expire = "10m"
refresh_expire = "72h"
bcrypt_cost = 10

[log]
level = "warn"
format = "json"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	os.Setenv("NETWORKDISK_SERVER_PORT", "9090")
	os.Setenv("NETWORKDISK_DATABASE_HOST", "envhost")
	os.Setenv("NETWORKDISK_AUTH_JWT_SECRET", "env-secret")
	os.Setenv("NETWORKDISK_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("NETWORKDISK_SERVER_PORT")
		os.Unsetenv("NETWORKDISK_DATABASE_HOST")
		os.Unsetenv("NETWORKDISK_AUTH_JWT_SECRET")
		os.Unsetenv("NETWORKDISK_LOG_LEVEL")
	}()
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("server.port = %d after env override, want 9090", cfg.Server.Port)
	}
	if cfg.Database.Host != "envhost" {
		t.Errorf("database.host = %s after env override, want envhost", cfg.Database.Host)
	}
	if cfg.Auth.JWTSecret != "env-secret" {
		t.Errorf("auth.jwt_secret = %s after env override, want env-secret", cfg.Auth.JWTSecret)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %s after env override, want debug", cfg.Log.Level)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("server.host = %s, should remain 0.0.0.0 (no env set)", cfg.Server.Host)
	}
}

func TestGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[server]
host = "0.0.0.0"
port = 4000
read_timeout = "10s"
write_timeout = "30s"
shutdown_timeout = "5s"

[database]
host = "h"
port = 1
user = "u"
password = "p"
database = "d"
max_open_conns = 1
max_idle_conns = 1
conn_max_lifetime = "1m"

[storage]
root = "/tmp"

[auth]
jwt_secret = "s"
jwt_expire = "1m"
refresh_expire = "1h"
bcrypt_cost = 4

[log]
level = "info"
format = "json"
`
	os.WriteFile(path, []byte(content), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := Get()
	if got.Server.Port != cfg.Server.Port {
		t.Errorf("Get() returned different config")
	}
}

func TestClone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[server]
host = "0.0.0.0"
port = 4000
read_timeout = "10s"
write_timeout = "30s"
shutdown_timeout = "5s"

[database]
host = "h"
port = 1
user = "u"
password = "p"
database = "d"
max_open_conns = 1
max_idle_conns = 1
conn_max_lifetime = "1m"

[storage]
root = "/tmp"

[auth]
jwt_secret = "s"
jwt_expire = "1m"
refresh_expire = "1h"
bcrypt_cost = 4

[log]
level = "info"
format = "json"
`
	os.WriteFile(path, []byte(content), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	clone := cfg.Clone()
	if clone.Server.Port != cfg.Server.Port {
		t.Error("clone mismatch")
	}
	clone.Server.Port = 9999
	if cfg.Server.Port == 9999 {
		t.Error("clone should be independent")
	}
}

func TestDSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[server]
host = "0.0.0.0"
port = 4000
read_timeout = "10s"
write_timeout = "30s"
shutdown_timeout = "5s"

[database]
host = "mysql.local"
port = 3306
user = "app"
password = "pass123"
database = "mydb"
max_open_conns = 1
max_idle_conns = 1
conn_max_lifetime = "1m"

[storage]
root = "/tmp"

[auth]
jwt_secret = "s"
jwt_expire = "1m"
refresh_expire = "1h"
bcrypt_cost = 4

[log]
level = "info"
format = "json"
`
	os.WriteFile(path, []byte(content), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dsn := cfg.DSN()
	expected := "app:pass123@tcp(mysql.local:3306)/mydb?charset=utf8mb4&parseTime=true&loc=Local"
	if dsn != expected {
		t.Errorf("DSN = %s, want %s", dsn, expected)
	}
}
