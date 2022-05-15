package main

import (
	"net/http"
	"os"
	"strings"
	"time"
	"trading/configs"
	"trading/pkg/client"
	"trading/pkg/models"

	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// телеграм авторизация
// https://makesomecode.me/2021/10/telegram-bot-oauth/
func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	log.Logger = log.Logger.With().Caller().Logger()
	log.Logger = log.Output(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.StampMilli},
	)
	log.Print("Starting telegram client...")

	config := configs.ReadClientConfig()
	log.Print("config" + config.TelegramToken)

	ch := make(chan tgbotapi.Chattable, 100)

	bClient := client.NewClient(config.BrokerAddr)
	cl := client.NewTelegramClient(bClient, ch)
	run(cl, ch, config)
}

func run(client *client.TelegramClient, in <-chan tgbotapi.Chattable, config configs.ClientConfig) {
	bot, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to create bot, token: %s", config.TelegramToken)
	}
	// bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// call on update webhook address
	wh, err := tgbotapi.NewWebhook(config.TelegramWebhookURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create webhook")
	}

	_, err = bot.Request(wh)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to webhook request")
	}

	go func() { _ = http.ListenAndServe(config.Addr, nil) }()
	go func() { // принимаем сообщения и отправляем в обработку
		updates := bot.ListenForWebhook("/")
		for update := range updates {
			switch {
			case update.CallbackQuery != nil:
				log.Printf("command callback: %v", update.CallbackQuery.Data)

				go client.HandleCommand(models.Message{
					ChatID:    update.CallbackQuery.Message.Chat.ID,
					MessageID: update.CallbackQuery.Message.MessageID,
					Text:      update.CallbackQuery.Data,
				})
			case update.Message == nil:
				log.Printf("nil message %v", update.UpdateID)

				continue

			case update.Message.IsCommand():
				log.Printf("command: %s", update.Message.Command())

				go client.HandleCommand(models.Message{
					ChatID:    update.Message.Chat.ID,
					MessageID: update.Message.MessageID,
					Text:      strings.TrimLeft(update.Message.Text, "/"),
				})

			case update.Message != nil:
				log.Printf("userInput: %v", update.Message.Chat.ID)

				go client.HandleUserInput(models.Message{
					ChatID:    update.Message.Chat.ID,
					MessageID: update.Message.MessageID,
					Text:      update.Message.Text,
				})
			}
		}
	}()

	// читае сообщения из канала и отправляем в бот, можно ограничить rps,
	// самое простое отправлять сообщение каждые 0.05 секунды (20 в секунду)
	for msg := range in {
		_, err = bot.Send(msg)
		if err != nil {
			log.Err(err).Msg("Failed to send message")
		}

		time.Sleep(time.Second / 20)
	}
}
