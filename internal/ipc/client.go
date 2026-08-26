package ipc

import (
	"encoding/json"
	"net"
	"sync"
)

type Client struct {
	mu   sync.Mutex
	conn net.Conn
}

func Connect() (*Client, error) {
	conn, err := net.Dial("unix", SocketPath)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func (c *Client) reconnect() error {
	if c.conn != nil {
		c.conn.Close()
	}
	conn, err := net.Dial("unix", SocketPath)
	if err != nil {
		c.conn = nil
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) Call(method string, params interface{}) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		if err := c.reconnect(); err != nil {
			return nil, err
		}
	}

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

	// Try sending request
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		// Try reconnect once on send failure
		if recErr := c.reconnect(); recErr != nil {
			return nil, err
		}
		if err := json.NewEncoder(c.conn).Encode(req); err != nil {
			return nil, err
		}
	}

	var resp Response
	if err := json.NewDecoder(c.conn).Decode(&resp); err != nil {
		// Connection might have broken mid-flight
		c.conn.Close()
		c.conn = nil
		return nil, err
	}

	return &resp, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}
