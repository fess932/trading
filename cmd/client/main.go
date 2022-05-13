package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"trading/configs"

	"github.com/akyoto/cache"
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

	c := NewClient(config.BrokerAddr)
	tc := NewTelegramClient(c, config)
	tc.Run()
}

// ClientInterface ...
type ClientInterface interface {
	Status()
	Deal()
	Cancel()
	History()
}

// TelegramClient is client for telegram
type TelegramClient struct {
	Client ClientInterface
	Bot    *tgbotapi.BotAPI
}

func NewTelegramClient(c ClientInterface, config configs.ClientConfig) *TelegramClient {
	bot, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to create bot, token: %s", config.TelegramToken)
	}

	//bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// call on update webhook address
	wh, err := tgbotapi.NewWebhook(config.TelegramWebhookURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create webhook")
	}
	//
	_, err = bot.Request(wh)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to webhook request")
	}
	//////////

	go http.ListenAndServe(config.Addr, nil)
	return &TelegramClient{
		Client: c,
		Bot:    bot,
	}
}

type Deal struct {
	state string

	Action string
	Tool   string
	Price  int64
	Volume int64
}

func (d Deal) Render() string {
	sel := "\\> "
	ren := []struct {
		key   string
		value string
	}{
		{"Header", "__Открытие позиции__\n\n"},
		{"action", fmt.Sprintf("Действие: %s\n", d.Action)},
		{"tool", fmt.Sprintf("Инструмент: %s\n", d.Tool)},
		{"price", fmt.Sprintf("Цена: %d\n", d.Price)},
		{"volume", fmt.Sprintf("Количество: %d\n", d.Volume)},
	}

	text := ""
	for _, v := range ren {
		if d.state == v.key {
			text += sel + v.value
			continue
		}

		text += v.value
	}

	return text
}

type MessageLocator struct {
	ChatID int64
	MsgID  int
}

var ErrDealNowFound = fmt.Errorf("deal not found")
var ErrActionNotFound = fmt.Errorf("action not found")

type Dealer struct {
	cache   *cache.Cache
	expTime time.Duration
	states  map[string]func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error
}

func NewDealer() *Dealer {
	d := &Dealer{
		cache:   cache.New(time.Minute * 5),
		expTime: time.Minute * 5,
	}
	d.states = map[string]func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error{
		//"": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
		//	msg.Text = deal.Render()
		//	return nil
		//},
		"action": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			msg.Text = deal.Render()
			backMenu := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("SPFB.RTS", "buy SPFBRTS"),
					tgbotapi.NewInlineKeyboardButtonData("IMOEX", "buy IMOEX"),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
					tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
				),
			)
			msg.ReplyMarkup = &backMenu

			return nil
		},
		"tool": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			msg.Text = deal.Render()

			backMenu := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("SPFB.RTS", "buy SPFBRTS"),
					tgbotapi.NewInlineKeyboardButtonData("IMOEX", "buy IMOEX"),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
					tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
				),
			)
			msg.ReplyMarkup = &backMenu

			return nil
		},
		"price": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			panic("implement me")
		},
		"volume": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			panic("implement me")
		},
	}

	return d
}

func (d *Dealer) initDeal(newDeal Deal, m MessageLocator) error {
	d.cache.Set(m, newDeal, d.expTime)
	return nil
}

func (d *Dealer) handleDeal(m MessageLocator, args ...string) (tgbotapi.Chattable, error) {
	val, ok := d.cache.Get(m)
	if !ok {
		return nil, fmt.Errorf("сделка не найдена: %w", ErrDealNowFound)
	}
	deal, ok := val.(Deal)
	if !ok {
		return nil, fmt.Errorf("сделка неправильный тип: %w", ErrDealNowFound)
	}

	f, ok := d.states[args[0]]
	if !ok {
		return nil, fmt.Errorf("ошибка %w, не найдено действие: %v", ErrActionNotFound, args[0])
	}

	msg := tgbotapi.NewEditMessageText(m.ChatID, m.MsgID, "")
	msg.ParseMode = tgbotapi.ModeMarkdownV2

	if err := f(&msg, &deal, args[1:]...); err != nil {
		return nil, fmt.Errorf("ошибка при обработке сделки: %w", err)
	}

	d.cache.Set(m, deal, d.expTime)

	return msg, nil
}

func (t *TelegramClient) Run() {
	mainMenu := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💵 Открыть позицию", "deal_start"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏷️ Мои позиции", "/myPositions"),

			tgbotapi.NewInlineKeyboardButtonData("⏱️ Цены", "/prices"),
		),
	)

	backMenu := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
		),
	)

	dealer := NewDealer()
	updates := t.Bot.ListenForWebhook("/")
	for update := range updates {
		if update.CallbackQuery != nil {
			log.Debug().Msgf("Callback query: %s", update.CallbackQuery.Data)

			var msg tgbotapi.Chattable

			chatID := update.CallbackQuery.Message.Chat.ID
			messageID := update.CallbackQuery.Message.MessageID
			cmd := strings.Split(update.CallbackQuery.Data, " ")

			if len(cmd) == 0 {
				continue
			}

			switch cmd[0] {
			case "deal_start": // create new message with inline keyboard
				nMsg := tgbotapi.NewMessage(chatID, "")
				nMsg.ParseMode = tgbotapi.ModeMarkdownV2
				newDeal := Deal{state: "action"}
				nMsg.Text = newDeal.Render()
				initDealMenu := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Купить", "deal action buy"),
						tgbotapi.NewInlineKeyboardButtonData("Продать", "deal action sell"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "deal delete"),
					),
				)
				nMsg.ReplyMarkup = &initDealMenu
				sMsg, err := t.Bot.Send(nMsg)
				if err != nil {
					log.Err(err).Msg("Failed to send message")
					continue
				}
				_ = dealer.initDeal(newDeal, MessageLocator{
					ChatID: sMsg.Chat.ID,
					MsgID:  sMsg.MessageID,
				})

				continue

			case "deal":
				if len(cmd) < 2 {
					log.Info().Msg("Invalid deal command")
					continue
				}
				nMsg, err := dealer.handleDeal(MessageLocator{
					ChatID: chatID,
					MsgID:  messageID,
				}, cmd[1:]...)
				if err != nil {
					log.Err(err).Msg("Failed to handle deal")
					continue
				}
				msg = nMsg

			//case "/ibuy":
			//	rmsg := update.CallbackQuery.Message
			//	val, ok := dcache.Get(MessageLocator{
			//		rmsg.Chat.ID,
			//		rmsg.MessageID,
			//	})
			//	if !ok {
			//		log.Err(ErrDealNowFound).Msgf("Failed to get deal: %v: %v", rmsg.Chat.ID, rmsg.MessageID)
			//		continue
			//	}
			//	deal := val.(Deal)
			//	deal.Action = "покупка"
			//	deal.state = stateSelectTool
			//	msg.Text = deal.Render()
			//
			//	backMenu = tgbotapi.NewInlineKeyboardMarkup(
			//		tgbotapi.NewInlineKeyboardRow(
			//			tgbotapi.NewInlineKeyboardButtonData("SPFB.RTS", "buy SPFBRTS"),
			//			tgbotapi.NewInlineKeyboardButtonData("IMOEX", "buy IMOEX"),
			//		),
			//		tgbotapi.NewInlineKeyboardRow(
			//			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
			//			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
			//		),
			//	)
			//	msg.ReplyMarkup = &backMenu
			//
			//case "/isell":
			//	msg.Text = "*Открытие позиции*\n"
			//	msg.Text += "__Продажа__\n\n"
			//	msg.Text += "Выберите инструмент/тикер\\:"
			//
			//	backMenu = tgbotapi.NewInlineKeyboardMarkup(
			//		tgbotapi.NewInlineKeyboardRow(
			//			tgbotapi.NewInlineKeyboardButtonData("SPFB.RTS", "/sell_SPFBRTS"),
			//			tgbotapi.NewInlineKeyboardButtonData("IMOEX", "/sell_IMOEX"),
			//		),
			//		tgbotapi.NewInlineKeyboardRow(
			//			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
			//		),
			//	)
			//	msg.ReplyMarkup = &backMenu

			case "/myPositions":
				nMsg := tgbotapi.NewMessage(chatID, "")
				nMsg.Text = "Мои позиции"
				nMsg.ReplyMarkup = &backMenu
				msg = nMsg
			case "/back":
				nMsg := tgbotapi.NewMessage(chatID, "*Главное меню*")
				nMsg.ReplyMarkup = &mainMenu
				msg = nMsg
			default:
				log.Printf("Unknown command: %s", cmd)

				nMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("не понял: %s", cmd))
				nMsg.ReplyMarkup = &backMenu
				msg = nMsg
			}

			if nMsg, err := t.Bot.Send(msg); err != nil {
				log.Err(err).Msg("Failed to send message")
			} else {
				log.Debug().Msgf("Message sent: %v", nMsg.MessageID)
			}

			//go func() {
			//	id := update.CallbackQuery.Message.MessageID
			//	chat := update.CallbackQuery.Message.Chat.ID
			//	reqNum := 0
			//
			//	msg := tgbotapi.NewEditMessageText(chat, id, fmt.Sprintf("Update: %v", reqNum))
			//	log.Printf("message id: %v", id)
			//	t.Bot.Send(msg)
			//	reqNum++
			//	time.Sleep(time.Second * 1)
			//}()

			//HandleNavigationCallbackQuery(bot, query.Message.MessageID, split[1:]...)

			//HandleNavigationCallbackQuery(bot, query.Message.MessageID, split[1:]...)

			//callback := tgbotapi.NewCallback(update.CallbackQuery.ID, update.CallbackQuery.Data)
			//if _, err := t.Bot.Request(callback); err != nil {
			//	panic(err)
			//}

			//// And finally, send a message containing the data received.
			//msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Data)
			//if _, err := t.Bot.Send(msg); err != nil {
			//	panic(err)
			//}
		}

		if update.Message == nil {
			continue
		}

		if !update.Message.IsCommand() { // ignore any non-command Messages
			msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				fmt.Sprintf("команда %s не распознана", update.Message.Text),
			)
			if _, err := t.Bot.Send(msg); err != nil {
				log.Fatal().Err(err).Msg("Failed to send message")
			}

			continue
		}

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "unknown command")
		msg.ParseMode = tgbotapi.ModeMarkdown

		cmd := update.Message.Command()

		switch cmd {
		case "home":
			msg.Text = "*Главное меню*"
			msg.ReplyMarkup = mainMenu

			//case "open":
			//	msg.Text = "Инлайн"
			//	msg.ReplyMarkup = mainMenu
			//
			//case "открыть позицию":
			//	msg.Text = "Инлайн"
			//	msg.ReplyMarkup = mainMenu

			//case "myPositions":
			//	msg.Text = "мои позиции"
			//chatID = update.Message.Chat.ID
			//SendDummyData(t.Bot, chatID, 0, 2, nil)
			////msg.ReplyMarkup = mainMenu

			//case "close":
			//	msg.Text = "Закрытие меню"
			//	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
		}

		if _, err := t.Bot.Send(msg); err != nil {
			log.Fatal().Err(err).Msg("Failed to send message")
		}

		//msg := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Text)
		//msg.ReplyMarkup = kb
		//msg.ReplyToMessageID = update.Message.MessageID
	}
}

//
//var data = []string{"DummyData1", "DummyData2", "DummyData3", "DummyData4", "DummyData5", "DummyData6", "DummyData7", "DummyData8", "DummyData9", "DummyData10"}
//var count = 2
//var maxPages = len(data) / count // = 5
//var chatID int64
//
//func DummyDataTextMarkup(currentPage, count int) (text string, markup tgbotapi.InlineKeyboardMarkup) {
//	text = strings.Join(data[currentPage*count:currentPage*count+count], "\n")
//
//	var rows []tgbotapi.InlineKeyboardButton
//	if currentPage > 0 {
//		rows = append(rows, tgbotapi.NewInlineKeyboardButtonData("Previous", fmt.Sprintf("pager:prev:%d:%d", currentPage, count)))
//	}
//	if currentPage < maxPages-1 {
//		rows = append(rows, tgbotapi.NewInlineKeyboardButtonData("Next", fmt.Sprintf("pager:next:%d:%d", currentPage, count)))
//	}
//
//	markup = tgbotapi.NewInlineKeyboardMarkup(rows)
//	return
//}
//
//func SendDummyData(bot *tgbotapi.BotAPI, chatId int64, currentPage, count int, messageId *int) {
//	text, keyboard := DummyDataTextMarkup(currentPage, count)
//
//	var cfg tgbotapi.Chattable
//	if messageId == nil {
//		msg := tgbotapi.NewMessage(chatId, text)
//		msg.ReplyMarkup = keyboard
//		cfg = msg
//	} else {
//		msg := tgbotapi.NewEditMessageText(chatId, *messageId, text)
//		msg.ReplyMarkup = &keyboard
//		cfg = msg
//	}
//
//	bot.Send(cfg)
//}
//
////func CallbackQueryHandler(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) {
////	//split := strings.Split(query.Data, ":")
////	if split[0] == "pager" {
////		//HandleNavigationCallbackQuery(bot, query.Message.MessageID, split[1:]...)
////
////
////
////		return
////	}
////}
//
////func HandleNavigationCallbackQuery(bot *tgbotapi.BotAPI, messageId int, data ...string) {
////	pagerType := data[0]
////	currentPage, _ := strconv.Atoi(data[1])
////	itemsPerPage, _ := strconv.Atoi(data[2])
////
////	go func() {
////		for {
////			SendDummyData(bot, chatID, currentPage, itemsPerPage, &messageId)
////			log.Print("sended")
////			time.Sleep(time.Second)
////		}
////	}()
////
////	if pagerType == "next" {
////		nextPage := currentPage + 1
////		if nextPage < maxPages {
////			SendDummyData(bot, chatID, nextPage, itemsPerPage, &messageId)
////		}
////	}
////	if pagerType == "prev" {
////		previousPage := currentPage - 1
////		if previousPage >= 0 {
////			SendDummyData(bot, chatID, previousPage, itemsPerPage, &messageId)
////		}
////	}
////}
