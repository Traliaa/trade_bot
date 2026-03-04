package service

import (
	"trade_bot/internal/modules/config"
)

// ModuleConfig ...
type ModuleConfig struct {
	IsStartNow bool
	cfg        *config.Config
}

// NewModuleConfig ...
func NewModuleConfig(cfg *config.Config) *ModuleConfig {

	return &ModuleConfig{
		IsStartNow: true,
		cfg:        cfg,
	}

}
