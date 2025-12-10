package okx_client

//func Module() fx.Option {
//	return fx.Module("okx_client",
//		// 1. Репозиторий юзеров
//		fx.Provide(
//			pg.NewUser, // func(...) *pg.User
//		),
//
//		// 2. Сервис Telegram как *service.Telegram
//		fx.Provide(
//			service.NewTelegram, // func(*config.Config, *pg.User, *runner.Manager) (*service.Telegram, error)
//		),
//
//		// 3. Адаптер: *service.Telegram -> runner.TelegramNotifier
//		fx.Provide(
//			func(t *service.Telegram) runner.TelegramNotifier {
//				return t
//			},
//		),
//		// Запуск основного цикла через Lifecycle
//		fx.Invoke(
//			func(lc fx.Lifecycle, t *service.Telegram) {
//				lc.Append(fx.Hook{
//					OnStart: func(ctx context.Context) error {
//						t.Start(ctx)
//						return nil
//					},
//					OnStop: func(ctx context.Context) error {
//						t.Stop()
//						return nil
//					},
//				})
//			},
//		),
//	)
//}
