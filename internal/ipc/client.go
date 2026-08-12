package ipc

import (
	"encoding/json"
	"net"
)

type Client struct {
	conn net.Conn
}

func Connect() (*Client, error) {
	conn, err := net.Dial("unix", SocketPath)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Call(method string, params interface{}) (*Response, error) {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		rawParams = b
	}

	req := Request{
		Method: method,
		Params: rawParams,
	}
	
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return nil, err
	}
	
	var resp Response
	if err := json.NewDecoder(c.conn).Decode(&resp); err != nil {
		return nil, err
	}
	
	return &resp, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}
