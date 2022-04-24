package main

import (
	"bufio"
	"io"
	"os"
	"time"
)

func main() {
	f, err := os.Open("./SPFB.RTS_190517_190517.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	readPerTick(time.Second, f)
}

type Entry struct {
	Ticker string
	Date   *time.Time
	Last   int64
	Vol    int64
}

func readPerTick(tickTime time.Duration, r io.Reader) chan string {
	//123030.000000000 - replace . with ""
	ch := make(chan string)
	go func() {
		ticker := time.NewTicker(tickTime)
		s := bufio.NewScanner(r)

		for {
			select {
			case <-ticker.C:
				ch <- "tick"
			}
		}
	}()

	return ch
}

//Если на биржу ставится заявка на покупку или продажу,
//	то она ставится в очередь
//		и когда цена доходит до неё и хватает объёма -
//			заявка исполняется, брокеру уходит соответствующее уведомление.

//Если не хватает объёма,
//	то заявка исполняется частичсно, брокеру так же уходит уведомление

//Если несколько участников поставилос заявку на одинаковый уровеньт цены,
//	то исполняются в порядке добавления.
