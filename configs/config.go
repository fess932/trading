package configs

import (
	"os"
	"time"
)

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

type BrokerConfig struct {
	Addr, ExchangeAddr string
}

func ReadBrokerConfig() BrokerConfig {
	return BrokerConfig{
		Addr:         ":8081",
		ExchangeAddr: "localhost:8080",
	}
}

type ClientConfig struct {
	Addr, TelegramToken, TelegramWebhookURL, BrokerAddr string
}

func ReadClientConfig() ClientConfig {
	return ClientConfig{
		BrokerAddr:         "localhost:8081",
		Addr:               ":8082",
		TelegramWebhookURL: os.Getenv("TELEGRAM_WEBHOOK_URL"),
		TelegramToken:      os.Getenv("TELEGRAM_TOKEN"),
	}
}
