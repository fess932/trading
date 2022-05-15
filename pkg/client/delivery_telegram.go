package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"trading/pkg/models"

	"github.com/akyoto/cache"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"
)

var ErrDealNowFound = fmt.Errorf("deal not found")
var ErrActionNotFound = fmt.Errorf("action not found")
var ErrCommandNotFound = fmt.Errorf("command not found")
var ErrNoArguments = fmt.Errorf("arguments not found")

// TelegramClient is client for telegram
type TelegramClient struct {
	Client IClient
	Dealer *Dealer
	out    chan<- tgbotapi.Chattable
}

func NewTelegramClient(bClient IClient, out chan<- tgbotapi.Chattable) *TelegramClient {
	return &TelegramClient{
		Client: bClient,
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

func (t *TelegramClient) HandleCommand(m models.Message) {
	cmd := strings.Split(m.Text, " ")
	if len(cmd) == 0 {
		t.out <- createErrorMessage(m.ChatID, ErrCommandNotFound)

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
			t.out <- createErrorMessage(m.ChatID, ErrNoArguments)

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
		t.handlePrices(m)
	}
}

func (t *TelegramClient) HandleUserInput(m models.Message) {
	// nMsg := tgbotapi.NewMessage(m.ChatID, "обработка пользовательского сообщения")
	// nMsg.ParseMode = tgbotapi.ModeMarkdownV2
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

// handlers

func (t *TelegramClient) handlePrices(m models.Message) {
	t.Client.Status()

	nMsg := tgbotapi.NewMessage(m.ChatID, "*Цены*")
	nMsg.ParseMode = tgbotapi.ModeMarkdownV2
	t.out <- nMsg
}

//// deal

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

// dealer

type Dealer struct {
	cache   *cache.Cache
	expTime time.Duration
	states  map[string]func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error
}

func NewDealer() *Dealer {
	const cacheTimeout = time.Minute * 5

	d := &Dealer{
		cache:   cache.New(cacheTimeout),
		expTime: cacheTimeout,
	}

	deleteBtn := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "deal delete"),
	)

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
					deleteBtn,
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
						tgbotapi.NewInlineKeyboardButtonData("🆗 Открыть", "deal open"),
					),
					deleteBtn,
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
				deleteBtn,
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

			backMenu := tgbotapi.NewInlineKeyboardMarkup(deleteBtn)
			msg.ReplyMarkup = &backMenu

			return nil
		},
		"price": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			log.Printf("args: %v", args)

			deal.Tool = args[0]
			deal.state = "volume"
			msg.Text = deal.Render()

			backMenu := tgbotapi.NewInlineKeyboardMarkup(deleteBtn)
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
				deleteBtn,
			)
			msg.ReplyMarkup = &backMenu

			return nil
		},
		"open": func(msg *tgbotapi.EditMessageTextConfig, deal *Deal, args ...string) error {
			msg.Text = "Позиция открыта"

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
