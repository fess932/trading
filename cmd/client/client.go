package main

type Client struct {
	BrokerAddr string
}

func NewClient(brokerAddr string) *Client {
	return &Client{BrokerAddr: brokerAddr}
}

func (c Client) Status() {
	//TODO implement me
	panic("implement me")
}

func (c Client) Deal() {
	//TODO implement me
	panic("implement me")
}

func (c Client) Cancel() {
	//TODO implement me
	panic("implement me")
}

func (c Client) History() {
	//TODO implement me
	panic("implement me")
}
