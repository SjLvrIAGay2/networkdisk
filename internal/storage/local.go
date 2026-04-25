package storage

import (
	"networkdisk/internal/config"
)

func NewFromConfig(cfg *config.Config) (*Local, error) {
	return NewLocal(cfg.Storage.Root)
}
