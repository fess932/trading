package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"trading/pkg/gen/exchange"

	"trading/configs"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.StampMilli})
	log.Printf("Starting exchange.proto...")

	f, err := os.Open("./data/SPFB.RTS_190517_190517.csv")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open file")
	}

	defer f.Close()

	config := configs.ReadConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exch := NewExchange(config.Addr)

	if err = exch.startExchangeServer(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}

	// read lines from io.Reader
	ch := entryReader(config.TickAggregateTime, f)

	// send lines to exchange server
	exch.tickReader(ch)
}

type Entry struct {
	Ticker string
	Time   int64
	Last   int64
	Vol    int32
}

func entryReader(tickTime time.Duration, r io.Reader) chan []Entry {
	ch := make(chan []Entry)
	buf := bufio.NewScanner(r)

	go func() {
		var curr []Entry
		var next Entry
		var err error
		var currentTickTime int64 = -1

		buf.Scan() // skip header

		for {
			t := time.Now() // start scan time

			next, err = scan(buf, &curr, currentTickTime)
			if err != nil {
				log.Err(err).Msg("Failed to scan")
				break
			}

			ch <- curr
			currentTickTime = next.Time
			curr = []Entry{next}

			// wait tickTime - scan time
			sleepTime := tickTime - time.Since(t)
			log.Printf("scan time: %v, sleep time: %v", time.Since(t), sleepTime)
			if sleepTime > 0 {
				time.Sleep(sleepTime)
			}
		}
	}()

	return ch
}

// return all entryes with one currentTickTime, last Entry which is not in currentTickTime
func scan(buf *bufio.Scanner, current *[]Entry, currentTickTime int64) (next Entry, err error) {
	for buf.Scan() {
		next, err = parseLine(buf.Text())
		if err != nil {
			return Entry{}, fmt.Errorf("failed to parse line: %v", err)
		}

		if currentTickTime == -1 {
			currentTickTime = next.Time
		}

		if currentTickTime != next.Time {
			return next, nil
		}

		*current = append(*current, next)
	}

	return Entry{}, io.EOF
}

var ErrWrongLine = errors.New("wrong line")

func parseLine(line string) (Entry, error) {
	var entry Entry
	es := strings.Split(line, ",")
	if len(es) != 6 {
		return Entry{}, ErrWrongLine
	}

	entry.Ticker = es[0]

	etime, err := strconv.Atoi(es[3])
	if err != nil {
		return Entry{}, fmt.Errorf("err parse time: %w", err)
	}
	entry.Time = int64(etime)

	last, err := strconv.Atoi(strings.ReplaceAll(es[4], ".", ""))
	if err != nil {
		return Entry{}, fmt.Errorf("err parse last: %w", err)
	}
	entry.Last = int64(last)

	vol, err := strconv.Atoi(es[5])
	if err != nil {
		return Entry{}, fmt.Errorf("err parse vol: %w", err)
	}
	entry.Vol = int32(vol)

	return entry, nil
}

type OHLCV struct {
	Open, High, Low, Close int64
	Volume                 int32
}

// Exchange grpc service
type Exchange struct {
	Addr string

	sync.Mutex
	consumers map[chan OHLCV]struct{}
	exchange.UnimplementedExchangeServer
}

func NewExchange(addr string) *Exchange {
	return &Exchange{
		Addr:      addr,
		consumers: make(map[chan OHLCV]struct{}),
	}
}

func (e *Exchange) startExchangeServer(ctx context.Context) error {
	server := grpc.NewServer(
		grpc.UnaryInterceptor(logInterceptor),
		grpc.StreamInterceptor(logStreamInterceptor),
	)
	exchange.RegisterExchangeServer(server, e)

	log.Printf("Starting exchange server on %s", e.Addr)
	l, err := net.Listen("tcp", e.Addr)
	if err != nil {
		return fmt.Errorf("cant create net.Listen: %w", err)
	}

	go server.Serve(l)
	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()

	return nil
}

// Broadcast to broker(consumer)
func (e *Exchange) tickReader(ch chan []Entry) {
	for entryes := range ch {
		if len(entryes) == 0 {
			continue
		}

		ohlcv := OHLCV{}
		ohlcv.Open = entryes[0].Last

		for i, entry := range entryes {
			if entry.Last < ohlcv.Low {
				ohlcv.Low = entry.Last
			}

			if entry.Last > ohlcv.High {
				ohlcv.High = entry.Last
			}

			if len(entryes) == i {
				ohlcv.Close = entry.Last
			}

			ohlcv.Volume += entry.Vol
		}

		e.Lock()
		for consumer := range e.consumers {
			consumer <- ohlcv
		}
		e.Unlock()

		log.Printf("ohlcv: %+v", ohlcv)
	}
}

func (e *Exchange) statisticSubscribe(ch chan OHLCV) {
	e.Lock()
	e.consumers[ch] = struct{}{}
	e.Unlock()
}

func (e *Exchange) statisticUnsubscribe(ch chan OHLCV) {
	e.Lock()
	delete(e.consumers, ch)
	e.Unlock()
}

func (e *Exchange) Statistic(
	id *exchange.BrokerID,
	exch exchange.Exchange_StatisticServer) (err error) {

	ch := make(chan OHLCV, 100)
	e.statisticSubscribe(ch)
	defer e.statisticUnsubscribe(ch)

	for ohlcv := range ch {
		err = exch.Send(&exchange.OHLCV{
			ID:       0,
			Time:     0,
			Interval: 0,
			Open:     ohlcv.Open,
			High:     ohlcv.High,
			Low:      ohlcv.Low,
			Close:    ohlcv.Close,
			Volume:   ohlcv.Volume,
			Ticker:   "",
		})
		if err != nil {
			return fmt.Errorf("cant send mesg to broker %v: %w", id.ID, err)
		}
	}

	return nil
}

func logInterceptor(
	ctx context.Context, req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (resp interface{}, err error) {

	log.Info().Msgf("request: %v, method: %v", req, info.FullMethod)

	return handler(ctx, req)
}
func logStreamInterceptor(
	srv interface{},
	stream grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler) (err error) {

	log.Info().Msgf("stream method: %v", info.FullMethod)

	return handler(srv, stream)
}

//Если на биржу ставится заявка на покупку или продажу,
//	то она ставится в очередь
//		и когда цена доходит до неё и хватает объёма -
//			заявка исполняется, брокеру уходит соответствующее уведомление.

//Если не хватает объёма,
//	то заявка исполняется частичсно, брокеру так же уходит уведомление

//Если несколько участников поставилос заявку на одинаковый уровеньт цены,
//	то исполняются в порядке добавления.
