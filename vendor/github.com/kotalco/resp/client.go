package resp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
)

const (
	SendCmd       = "*3\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n"
	IncrCmd       = "*2\r\n$4\r\nINCR\r\n$%d\r\n%s\r\n"
	ExpireCmd     = "*3\r\n$6\r\nEXPIRE\r\n$%d\r\n%s\r\n$%d\r\n%d\r\n"
	SetWithTTLCmd = "*5\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$2\r\nEX\r\n$%d\r\n%d\r\n"
	GetCmd        = "*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n"
	DeleteCmd     = "*2\r\n$3\r\nDEL\r\n$%d\r\n%s\r\n"
)

type IClient interface {
	GetConnection() (IConnection, error)
	ReleaseConnection(conn IConnection)
	Do(ctx context.Context, command string) (string, error)
	Set(ctx context.Context, key string, value string) error
	SetWithTTL(ctx context.Context, key string, value string, ttl int) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
	Incr(ctx context.Context, key string) (int, error)
	Expire(ctx context.Context, key string, seconds int) (bool, error)
	Close()
}

type Client struct {
	pool     chan IConnection
	address  string
	poolSize int
	mu       sync.Mutex
	auth     string
	dialer   IDialer
}

func NewRedisClient(address string, poolSize int, auth string) (IClient, error) {
	client := &Client{
		pool:     make(chan IConnection, poolSize),
		address:  address,
		poolSize: poolSize,
		auth:     auth,
		dialer:   NewDialer(),
	}
	// pre-populate the pool with connections , authenticated and ready to be used
	for i := 0; i < poolSize; i++ {
		conn, err := NewRedisConnection(client.dialer, address, auth)
		if err != nil {
			log.Println(err.Error())
			continue
		}
		client.pool <- conn
	}
	if len(client.pool) == 0 {
		return nil, errors.New("can't create redis connection")
	}

	return client, nil
}

func (client *Client) GetConnection() (IConnection, error) {
	// make sure that the access to the client.pool is synchronized among concurrent goroutines, make the operation atomic to prevent race conditions
	client.mu.Lock()
	defer client.mu.Unlock()

	select {
	case conn := <-client.pool:
		return conn, nil
	default:
		// Pool is empty now all connection are being used , create a new connection till some connections get released
		conn, err := NewRedisConnection(client.dialer, client.address, client.auth)
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

func (client *Client) ReleaseConnection(conn IConnection) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.pool) >= client.poolSize {
		err := conn.Close()
		if err != nil {
			return
		} //if the pool is full the new conn is closed and discarded
	} else {
		client.pool <- conn //if there is room put into the pool for future use
	}
}

func (client *Client) Do(ctx context.Context, command string) (string, error) {
	conn, err := client.GetConnection()
	if err != nil {
		return "", err
	}
	defer client.ReleaseConnection(conn)

	errChan := make(chan error, 1)
	replyChan := make(chan string, 1)
	go func() {
		err := conn.Send(ctx, command)
		if err != nil {
			errChan <- err
			return
		}

		reply, err := conn.Receive(ctx)
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

func (client *Client) Set(ctx context.Context, key string, value string) error {
	cmd := fmt.Sprintf(SendCmd, len(key), key, len(value), value)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return err
	}
	if response != "OK" {
		return errors.New("unexpected response from server")
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
		return 0, errors.New("unexpected response from server")
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
		return false, errors.New("unexpected response from server")
	}
}

func (client *Client) SetWithTTL(ctx context.Context, key string, value string, ttl int) error {
	cmd := fmt.Sprintf(SetWithTTLCmd, len(key), key, len(value), value, len(strconv.Itoa(ttl)), ttl)
	response, err := client.Do(ctx, cmd)
	if err != nil {
		return err
	}
	if response != "OK" {
		return errors.New("unexpected response from server: " + response)
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
		return errors.New("unexpected response from server")
	}

	return nil
}

func (client *Client) Close() {
	close(client.pool)
	for conn := range client.pool {
		_ = conn.Close()
	}
}
