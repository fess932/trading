package configs

import "time"

type ExchangeConfig struct {
	Addr              string
	TickAggregateTime time.Duration
	Tickers           []string
}

func ReadConfig() ExchangeConfig {
	return ExchangeConfig{
		Addr:              ":8080",
		TickAggregateTime: time.Second,
		Tickers: []string{
			"SPFB.RTS",
		},
	}
}
