package models

type Status struct {
	Body struct {
		Balance   int `json:"balance"`
		Positions []struct {
			Ticker string `json:"ticker"`
			Field2 string `json:"..."`
		} `json:"positions"`
		OpenOrders []struct {
			Id     int    `json:"id"`
			Ticker string `json:"ticker"`
			Field3 string `json:"..."`
		} `json:"open_orders"`
	} `json:"body"`
}
