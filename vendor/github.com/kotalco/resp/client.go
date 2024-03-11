package resp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

const (
	PingCmd       = "PING\n"
	SendCmd       = "*3\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n"
	IncrCmd       = "*2\r\n$4\r\nINCR\r\n$%d\r\n%s\r\n"
	ExpireCmd     = "*3\r\n$6\r\nEXPIRE\r\n$%d\r\n%s\r\n$%d\r\n%d\r\n"
	SetWithTTLCmd = "*5\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$2\r\nEX\r\n$%d\r\n%d\r\n"
	GetCmd        = "*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n"
	DeleteCmd     = "*2\r\n$3\r\nDEL\r\n$%d\r\n%s\r\n"
)

type IClient interface {
	Do(ctx context.Context, command string) (string, error)
	Ping(ctx context.Context) (string, error)
	Set(ctx context.Context, key string, value string) error
	SetWithTTL(ctx context.Context, key string, value string, ttl int) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
	Incr(ctx context.Context, key string) (int, error)
	Expire(ctx context.Context, key string, seconds int) (bool, error)
	Close() error
}

type Client struct {
	conn    IConnection
	address string
	mu      sync.Mutex
	auth    string
	dialer  IDialer
}

func NewRedisClient(address string, auth string) (IClient, error) {
	client := &Client{
		address: address,
		auth:    auth,
		dialer:  NewDialer(),
	}

	conn, err := NewRedisConnection(client.dialer, address, auth)
	if err != nil {
		return nil, errors.New("can't create redis connection")
	}

	client.conn = conn

	return client, nil
}

func (client *Client) Do(ctx context.Context, command string) (string, error) {
	errChan := make(chan error, 1)
	replyChan := make(chan string, 1)
	go func() {
		err := client.conn.Send(ctx, command)
		if err != nil {
			errChan <- err
			return
		}

		reply, err := client.conn.Receive(ctx)
		if err != nil {
			errChan <- err
		} else {
			replyChan <- reply
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err() // The context was cancelled
	case err := <-errChan:
		return "", err // The redis operation returned an error
	case reply := <-replyChan:
		return reply, nil // The redis operation was successful
	}

}

func (client *Client) Ping(ctx context.Context) (string, error) {
	response, err := client.Do(ctx, PingCmd)
	if err != nil {
		return "", err
	}
	if response != "PONG" {
		return "", errors.New("unexpected response from server")
	}
	return response, nil
}

func (client *Client) Set(ctx context.Context, key string, value string) error {
	cmd := fmt.Sprintf(SendCmd, len(key), key, len(value), value)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("set: unexpected response from server %s", response)
	}
	return nil
}

func (client *Client) Incr(ctx context.Context, key string) (int, error) {
	cmd := fmt.Sprintf(IncrCmd, len(key), key)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return 0, err
	}

	// Parse the response => should be in the format: ":<number>\r\n" for a successful INCR command
	var newValue int
	if _, err := fmt.Sscanf(response, ":%d\r\n", &newValue); err != nil {
		return 0, fmt.Errorf("incr: unexpected response from server %s", response)
	}

	// Return the new value
	return newValue, nil
}

func (client *Client) Expire(ctx context.Context, key string, seconds int) (bool, error) {
	cmd := fmt.Sprintf(ExpireCmd, len(key), key, len(fmt.Sprintf("%d", seconds)), seconds)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return false, err
	}

	// Parse the response => should be in the format: ":1" for a successful EXPIRE command (if the key exists), or ":0" if it does not.
	if response == ":1" {
		return true, nil
	} else if response == ":0" {
		return false, nil
	} else {
		return false, fmt.Errorf("expire: unexpected response from server %s", response)
	}
}

func (client *Client) SetWithTTL(ctx context.Context, key string, value string, ttl int) error {
	cmd := fmt.Sprintf(SetWithTTLCmd, len(key), key, len(value), value, len(strconv.Itoa(ttl)), ttl)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return err
	}
	if response != "OK" {
		return fmt.Errorf("setWithTTL: unexpected response from server %s", response)
	}
	return nil
}

func (client *Client) Get(ctx context.Context, key string) (string, error) {
	cmd := fmt.Sprintf(GetCmd, len(key), key)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return "", err
	}
	return response, nil
}

func (client *Client) Delete(ctx context.Context, key string) error {
	cmd := fmt.Sprintf(DeleteCmd, len(key), key)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return err
	}
	// ":1" for successful deletion of one key.
	// ":0" If the key does not exist
	if response != ":1" && response != ":0" {
		return fmt.Errorf("delete: unexpected response from server %s", response)
	}

	return nil
}

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.conn.Close()
}
