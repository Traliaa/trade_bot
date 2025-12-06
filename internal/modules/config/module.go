package config

import "go.uber.org/fx"

// ProvideAppConfig регистрируем как fx-провайдер.
func Module() fx.Option {
	return fx.Module("config",
		fx.Provide(
			NewConfig,
		),
	)
}
