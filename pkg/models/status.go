package models

type Status struct {
	Body struct {
		Balance   int `json:"balance"`
		Positions []struct {
			Ticker string `json:"ticker"`
		} `json:"positions"`
		OpenOrders []struct {
			ID     int    `json:"id"`
			Ticker string `json:"ticker"`
		} `json:"open_orders"`
	} `json:"body"`
}
