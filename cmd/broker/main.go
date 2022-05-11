package main

import (
	"context"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"os"
	"time"
	"trading/configs"
	"trading/pkg/gen/exchange"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.StampMilli})
	log.Printf("Starting broker...")

	config := configs.ReadConfig()

	broker := StarStockbrocker(config.Addr)

	e, err := broker.client.Statistic(context.Background(), &exchange.BrokerID{ID: broker.id})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get broker statistics")
	}

	for {
		ohlcv, err := e.Recv()
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to get broker statistics")
		}

		log.Printf("ohlcv: %+v", ohlcv)
	}
}

type Broker struct {
	id     int64
	client exchange.ExchangeClient
}

func StarStockbrocker(exchangeAddr string) *Broker {
	grpcConn, err := grpc.Dial(
		exchangeAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to gRPC server")
	}

	client := exchange.NewExchangeClient(grpcConn)
	return &Broker{
		id:     1,
		client: client,
	}
}
