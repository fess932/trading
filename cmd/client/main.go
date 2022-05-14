package main

import (
	"errors"
	"fmt"
	"github.com/akyoto/cache"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"trading/configs"

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
	cl := newTelegramClient(ch)
	run(cl, ch, config)
}

// ClientInterface ...
type ClientInterface interface {
	Status()
	Deal()
	Cancel()
	History()
}

type Message struct {
	ChatID    int64
	MessageID int
	Text      string
}

func run(client *TelegramClient, in <-chan tgbotapi.Chattable, config configs.ClientConfig) {
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

	_, err = bot.Request(wh)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to webhook request")
	}

	go http.ListenAndServe(config.Addr, nil)
	go func() { // принимаем сообщения и отправляем в обработку
		updates := bot.ListenForWebhook("/")
		for update := range updates {
			switch {
			case update.CallbackQuery != nil:
				log.Printf("command callback: [%v]", update.CallbackQuery.Data)
				go client.handleCommand(Message{
					ChatID:    update.CallbackQuery.Message.Chat.ID,
					MessageID: update.CallbackQuery.Message.MessageID,
					Text:      update.CallbackQuery.Data,
				})
			case update.Message == nil:
				log.Printf("nil message [%v]", update.UpdateID)
				continue

			case update.Message.IsCommand():
				log.Printf("command: %s", update.Message.Command())
				go client.handleCommand(Message{
					ChatID:    update.Message.Chat.ID,
					MessageID: update.Message.MessageID,
					Text:      strings.TrimLeft(update.Message.Text, "/"),
				})

			case update.Message != nil:
				log.Printf("userInput: %s", update.Message.Chat.ID)
				go client.handleUserInput(Message{
					ChatID:    update.Message.Chat.ID,
					MessageID: update.Message.MessageID,
					Text:      update.Message.Text,
				})
			}
		}
	}()

	// читае сообщения из канала и отправляем в бот, можно ограничить rps
	for msg := range in {
		_, err = bot.Send(msg)
		if err != nil {
			log.Err(err).Msg("Failed to send message")
		}
	}
}

// TelegramClient is client for telegram
type TelegramClient struct {
	Client ClientInterface
	Dealer *Dealer
	out    chan<- tgbotapi.Chattable
}

func newTelegramClient(out chan<- tgbotapi.Chattable) *TelegramClient {
	return &TelegramClient{
		Dealer: NewDealer(),
		out:    out,
	}
}

func createErrorMessage(chatID int64, err error) tgbotapi.Chattable {
	return tgbotapi.NewMessage(
		chatID,
		"Ошибка в команде: "+err.Error(),
	)
}

func (t *TelegramClient) handleCommand(m Message) {
	cmd := strings.Split(m.Text, " ")
	if len(cmd) == 0 {
		t.out <- createErrorMessage(m.ChatID, errors.New("нет команды"))
		return
	}

	switch cmd[0] {
	case "home":
		nMsg := tgbotapi.NewMessage(m.ChatID, "*Главное меню*")
		nMsg.ParseMode = tgbotapi.ModeMarkdownV2
		nMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("💵 Открыть позицию", "deal_start"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🏷️ Мои позиции", "myPositions"),

				tgbotapi.NewInlineKeyboardButtonData("⏱️ Цены", "prices"),
			),
		)

		t.out <- nMsg

	case "deal_start":
		nMsg := tgbotapi.NewMessage(m.ChatID, "")
		nMsg.ParseMode = tgbotapi.ModeMarkdownV2
		newDeal := t.Dealer.newDeal(m.ChatID)
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
		t.out <- nMsg

	case "deal":
		if len(cmd) < 2 {
			t.out <- createErrorMessage(m.ChatID, errors.New("нет аргументов команды"))
			return
		}
		nMsg, err := t.Dealer.handleDeal(m.ChatID, m.MessageID, cmd[1:]...)
		if err != nil {
			t.out <- createErrorMessage(m.ChatID, err)
			return
		}
		t.out <- nMsg

	case "myPositions":
		nMsg := tgbotapi.NewMessage(m.ChatID, "*Мои позиции*")
		nMsg.ParseMode = tgbotapi.ModeMarkdownV2
		t.out <- nMsg

	case "prices":
		nMsg := tgbotapi.NewMessage(m.ChatID, "*Цены*")
		nMsg.ParseMode = tgbotapi.ModeMarkdownV2
		t.out <- nMsg
	}
}

func (t *TelegramClient) handleUserInput(m Message) {
	//nMsg := tgbotapi.NewMessage(m.ChatID, "обработка пользовательского сообщения")
	//nMsg.ParseMode = tgbotapi.ModeMarkdownV2

	nMsg, err := t.Dealer.handleDeal(m.ChatID, m.MessageID, "userInput", m.Text)
	if err != nil {
		t.out <- createErrorMessage(m.ChatID, err)
		return
	}

	t.out <- nMsg

	// then delte user input message
	dMsg := tgbotapi.NewDeleteMessage(m.ChatID, m.MessageID)
	t.out <- dMsg
}

type Deal struct {
	state string
	msgID int

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
		"userInput": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {

			switch deal.state {
			case "price":
				price, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("cant convert user input to price: %w", err)
				}

				deal.Price = int64(price)
				deal.state = "volume"

				backMenu := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
						tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
					),
				)
				msg.ReplyMarkup = &backMenu

				msg.Text = deal.Render() + "\n\\> *Введите количество*:"
				return nil

			case "volume":
				volume, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("cant convert user input to volume: %w", err)
				}

				deal.Volume = int64(volume)
				deal.state = "end"

				msg.Text = deal.Render()

				backMenu := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Открыть", "deal open"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
						tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
					),
				)
				msg.ReplyMarkup = &backMenu

				return nil
			}

			msg.Text = deal.Render()
			return nil
		},
		"action": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			deal.msgID = msg.MessageID

			deal.Action = args[0]
			switch args[0] {
			case "buy":
				deal.Action = "покупка"
			case "sell":
				deal.Action = "продажа"
			default:
				return fmt.Errorf("%w: %s", ErrActionNotFound, args[0])
			}

			deal.state = "tool"
			msg.Text = deal.Render()

			backMenu := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("SPFB.RTS", "deal tool SPFBRTS"),
					tgbotapi.NewInlineKeyboardButtonData("IMOEX", "deal tool IMOEX"),
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
			log.Printf("args: %v", args)

			switch args[0] {
			case "SPFBRTS":
				deal.Tool = "SPFB.RTS"
			case "IMOEX":
				deal.Tool = "IMOEX"
			default:
				return fmt.Errorf("%w: %s", ErrActionNotFound, args[0])
			}

			deal.Tool = args[0]
			deal.state = "price"
			msg.Text = deal.Render()
			msg.Text += "\n\\> *Введите цену:*"

			backMenu := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
					tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
				),
			)
			msg.ReplyMarkup = &backMenu

			return nil
		},
		"price": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			log.Printf("args: %v", args)

			deal.Tool = args[0]
			deal.state = "volume"
			msg.Text = deal.Render()

			backMenu := tgbotapi.NewInlineKeyboardMarkup(

				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
					tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "/delete"),
				),
			)
			msg.ReplyMarkup = &backMenu

			return nil
		},
		"volume": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			deal.Tool = args[0]
			deal.state = "end"
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
		"open": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			msg.Text = "Сделка открыта"
			return nil
		},
	}

	return d
}

func (d *Dealer) newDeal(chatID int64) Deal {
	deal := Deal{state: "action"}
	d.cache.Set(chatID, deal, d.expTime)
	return deal
}

func (d *Dealer) handleDeal(chatID int64, msgID int, args ...string) (tgbotapi.Chattable, error) {
	val, ok := d.cache.Get(chatID)
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

	msg := tgbotapi.NewEditMessageText(chatID, msgID, "")
	if deal.msgID != 0 {
		msg.MessageID = deal.msgID
	}
	log.Printf("msg: %v: %v", deal.msgID, msgID)

	msg.ParseMode = tgbotapi.ModeMarkdownV2

	if err := f(&msg, &deal, args[1:]...); err != nil {
		return nil, fmt.Errorf("ошибка при обработке сделки: %w", err)
	}

	d.cache.Set(chatID, deal, d.expTime)

	return msg, nil
}

//
//func (t *TelegramClient) Run() {
//	mainMenu := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("💵 Открыть позицию", "deal_start"),
//		),
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("🏷️ Мои позиции", "/myPositions"),
//
//			tgbotapi.NewInlineKeyboardButtonData("⏱️ Цены", "/prices"),
//		),
//	)
//
//	backMenu := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/back"),
//		),
//	)
//
//	dealer := NewDealer()
//	updates := t.Bot.ListenForWebhook("/")
//
//	for update := range updates {
//		if update.CallbackQuery != nil {
//			log.Debug().Msgf("Callback query: %s", update.CallbackQuery.Data)
//
//			var msg tgbotapi.Chattable
//
//			chatID := update.CallbackQuery.Message.Chat.ID
//			messageID := update.CallbackQuery.Message.MessageID
//			cmd := strings.Split(update.CallbackQuery.Data, " ")
//
//			if len(cmd) == 0 {
//				continue
//			}
//
//			switch cmd[0] {
//			case "deal_start": // create new message with inline keyboard
//				nMsg := tgbotapi.NewMessage(chatID, "")
//				nMsg.ParseMode = tgbotapi.ModeMarkdownV2
//				newDeal := Deal{state: "action"}
//				nMsg.Text = newDeal.Render()
//				initDealMenu := tgbotapi.NewInlineKeyboardMarkup(
//					tgbotapi.NewInlineKeyboardRow(
//						tgbotapi.NewInlineKeyboardButtonData("Купить", "deal action buy"),
//						tgbotapi.NewInlineKeyboardButtonData("Продать", "deal action sell"),
//					),
//					tgbotapi.NewInlineKeyboardRow(
//						tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "deal delete"),
//					),
//				)
//				nMsg.ReplyMarkup = &initDealMenu
//				sMsg, err := t.Bot.Send(nMsg)
//				if err != nil {
//					log.Err(err).Msg("Failed to send message")
//					continue
//				}
//				_ = dealer.initDeal(newDeal, MessageLocator{
//					ChatID: sMsg.Chat.ID,
//					MsgID:  sMsg.MessageID,
//				})
//
//				continue
//
//			case "deal":
//				if len(cmd) < 2 {
//					log.Info().Msg("Invalid deal command")
//					continue
//				}
//				nMsg, err := dealer.handleDeal(MessageLocator{
//					ChatID: chatID,
//					MsgID:  messageID,
//				}, cmd[1:]...)
//				if err != nil {
//					log.Err(err).Msg("Failed to handle deal")
//					continue
//				}
//				msg = nMsg
//
//			case "/myPositions":
//				nMsg := tgbotapi.NewMessage(chatID, "")
//				nMsg.Text = "Мои позиции"
//				nMsg.ReplyMarkup = &backMenu
//				msg = nMsg
//			case "/back":
//				nMsg := tgbotapi.NewMessage(chatID, "*Главное меню*")
//				nMsg.ReplyMarkup = &mainMenu
//				msg = nMsg
//			default:
//				log.Printf("Unknown command: %s", cmd)
//
//				nMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("не понял: %s", cmd))
//				nMsg.ReplyMarkup = &backMenu
//				msg = nMsg
//			}
//
//			if nMsg, err := t.Bot.Send(msg); err != nil {
//				log.Err(err).Msg("Failed to send message")
//			} else {
//				log.Debug().Msgf("Message sent: %v", nMsg.MessageID)
//			}
//		}
//
//		if update.Message == nil {
//			continue
//		}
//
//		if !update.Message.IsCommand() {
//			msg, err := dealer.handleDeal(MessageLocator{
//				ChatID: update.Message.Chat.ID,
//				MsgID:  update.Message.MessageID,
//			}, "userInput", update.Message.Text)
//			if err != nil {
//				log.Err(err).Msg("Failed to handle deal")
//				continue
//			}
//
//			if _, err = t.Bot.Send(msg); err != nil {
//				log.Fatal().Err(err).Msg("Failed to send message")
//			}
//
//			continue
//		}
//
//		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "unknown command")
//		msg.ParseMode = tgbotapi.ModeMarkdown
//
//		cmd := update.Message.Command()
//
//		switch cmd {
//		case "home":
//			msg.Text = "*Главное меню*"
//			msg.ReplyMarkup = mainMenu
//
//			//case "open":
//			//	msg.Text = "Инлайн"
//			//	msg.ReplyMarkup = mainMenu
//			//
//			//case "открыть позицию":
//			//	msg.Text = "Инлайн"
//			//	msg.ReplyMarkup = mainMenu
//
//			//case "myPositions":
//			//	msg.Text = "мои позиции"
//			//chatID = update.Message.Chat.ID
//			//SendDummyData(t.Bot, chatID, 0, 2, nil)
//			////msg.ReplyMarkup = mainMenu
//
//			//case "close":
//			//	msg.Text = "Закрытие меню"
//			//	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
//		}
//
//		if _, err := t.Bot.Send(msg); err != nil {
//			log.Fatal().Err(err).Msg("Failed to send message")
//		}
//
//		//msg := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Text)
//		//msg.ReplyMarkup = kb
//		//msg.ReplyToMessageID = update.Message.MessageID
//	}
//}
//
////
////var data = []string{"DummyData1", "DummyData2", "DummyData3", "DummyData4", "DummyData5", "DummyData6", "DummyData7", "DummyData8", "DummyData9", "DummyData10"}
////var count = 2
////var maxPages = len(data) / count // = 5
////var chatID int64
////
////func DummyDataTextMarkup(currentPage, count int) (text string, markup tgbotapi.InlineKeyboardMarkup) {
////	text = strings.Join(data[currentPage*count:currentPage*count+count], "\n")
////
////	var rows []tgbotapi.InlineKeyboardButton
////	if currentPage > 0 {
////		rows = append(rows, tgbotapi.NewInlineKeyboardButtonData("Previous", fmt.Sprintf("pager:prev:%d:%d", currentPage, count)))
////	}
////	if currentPage < maxPages-1 {
////		rows = append(rows, tgbotapi.NewInlineKeyboardButtonData("Next", fmt.Sprintf("pager:next:%d:%d", currentPage, count)))
////	}
////
////	markup = tgbotapi.NewInlineKeyboardMarkup(rows)
////	return
////}
////
////func SendDummyData(bot *tgbotapi.BotAPI, chatId int64, currentPage, count int, messageId *int) {
////	text, keyboard := DummyDataTextMarkup(currentPage, count)
////
////	var cfg tgbotapi.Chattable
////	if messageId == nil {
////		msg := tgbotapi.NewMessage(chatId, text)
////		msg.ReplyMarkup = keyboard
////		cfg = msg
////	} else {
////		msg := tgbotapi.NewEditMessageText(chatId, *messageId, text)
////		msg.ReplyMarkup = &keyboard
////		cfg = msg
////	}
////
////	bot.Send(cfg)
////}
////
//////func CallbackQueryHandler(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) {
//////	//split := strings.Split(query.Data, ":")
//////	if split[0] == "pager" {
//////		//HandleNavigationCallbackQuery(bot, query.Message.MessageID, split[1:]...)
//////
//////
//////
//////		return
//////	}
//////}
////
//////func HandleNavigationCallbackQuery(bot *tgbotapi.BotAPI, messageId int, data ...string) {
//////	pagerType := data[0]
//////	currentPage, _ := strconv.Atoi(data[1])
//////	itemsPerPage, _ := strconv.Atoi(data[2])
//////
//////	go func() {
//////		for {
//////			SendDummyData(bot, chatID, currentPage, itemsPerPage, &messageId)
//////			log.Print("sended")
//////			time.Sleep(time.Second)
//////		}
//////	}()
//////
//////	if pagerType == "next" {
//////		nextPage := currentPage + 1
//////		if nextPage < maxPages {
//////			SendDummyData(bot, chatID, nextPage, itemsPerPage, &messageId)
//////		}
//////	}
//////	if pagerType == "prev" {
//////		previousPage := currentPage - 1
//////		if previousPage >= 0 {
//////			SendDummyData(bot, chatID, previousPage, itemsPerPage, &messageId)
//////		}
//////	}
//////}
