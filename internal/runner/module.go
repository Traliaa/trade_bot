package runner

import (
	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("runner",
		// сам Manager
		fx.Provide(
			NewManager,
		),
	)
}
