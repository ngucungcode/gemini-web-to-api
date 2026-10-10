package providers

import (
	"context"

	"gemini-web-to-api/internal/commons/configs"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func newPoolFromConfig(cfg *configs.Config, log *zap.Logger) *Pool {
	clients := make([]*Client, 0, len(cfg.Gemini.Accounts))
	for i, acc := range cfg.Gemini.Accounts {
		if acc.Name == "" {
			acc.Name = "acc1"
			if i > 0 {
				acc.Name = "acc" + itoa(i+1)
			}
			cfg.Gemini.Accounts[i].Name = acc.Name
		}
		clients = append(clients, NewClientForAccount(acc, cfg.Gemini.RefreshInterval, cfg.Gemini.MaxRetries, cfg.Gemini.Temporary, cfg.Gemini.CookiesDir, log))
	}
	return NewPool(clients, log)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

var Module = fx.Options(
	fx.Provide(newPoolFromConfig),
	fx.Provide(NewProviderManager),
	fx.Invoke(RegisterProvider),
)

func RegisterProvider(lc fx.Lifecycle, pm *ProviderManager, pool *Pool, log *zap.Logger) {
	pm.Register("gemini", pool)

	if err := pm.SelectProvider("gemini"); err != nil {
		log.Error("Failed to select Gemini provider", zap.Error(err))
	}

	initCtx, cancelInit := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			log.Info("Starting Gemini account pool initialization in background", zap.Int("accounts", pool.Size()))
			go func() {
				if err := pool.Init(initCtx); err != nil {
					log.Error("Gemini pool initialization failed", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancelInit()
			return pool.Close()
		},
	})
}
