package client

import "trading/pkg/models"

// IClient is broker client
type IClient interface {
	Status() (models.Status, error)
	Deal() (models.Status, error)
	Cancel() (models.Status, error)
	History() (models.Status, error)
}

type Client struct {
	BrokerAddr string
}

func NewClient(brokerAddr string) *Client {
	return &Client{BrokerAddr: brokerAddr}
}

type Position struct {
	Ticker string
}

type OpenOrder struct {
	ID     int    `json:"id"`
	Ticker string `json:"ticker"`
	Price  int64  `json:"price"`
}

func (c Client) Status() (models.Status, error) {
	return models.Status{}, nil
}

func (c Client) Deal() (models.Status, error) {
	//TODO implement me
	panic("implement me")
}

func (c Client) Cancel() (models.Status, error) {
	//TODO implement me
	panic("implement me")
}

func (c Client) History() (models.Status, error) {
	//TODO implement me
	panic("implement me")
}
