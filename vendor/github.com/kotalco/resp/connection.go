package resp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type IConnection interface {
	Auth(ctx context.Context, password string) error
	Ping(ctx context.Context) error
	Send(ctx context.Context, command string) error
	Receive(ctx context.Context) (string, error)
	Close() error
}
type Connection struct {
	conn net.Conn
	rw   *bufio.ReadWriter
}

func NewRedisConnection(dialer IDialer, address string, auth string) (IConnection, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialer.Dial(ctx, address)
	if err != nil {
		return nil, err
	}

	rc := &Connection{
		conn: conn,
		rw:   bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
	}

	if auth != "" {
		// Authenticate with Redis using the AUTH command
		if err := rc.Auth(ctx, auth); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return rc, nil
}

func (rc *Connection) Auth(ctx context.Context, password string) error {
	// Check if the context has been canceled before attempting the read operation
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rc.Send(ctx, fmt.Sprintf("AUTH %s", password)); err != nil {
		return err
	}
	reply, err := rc.Receive(ctx)
	if err != nil {
		return err
	}
	if reply != "OK" {
		return errors.New("authentication failed")
	}
	return nil
}

func (rc *Connection) Ping(ctx context.Context) error {
	// Check if the context has been canceled before attempting the operation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Send the PING command to the Redis server
	if err := rc.Send(ctx, "PING"); err != nil {
		return err
	}

	// Receive the reply from the Redis server
	reply, err := rc.Receive(ctx)
	if err != nil {
		return err
	}

	// Check if the reply is a valid PONG response
	if reply != "PONG" {
		return errors.New("did not receive PONG response")
	}

	return nil
}

func (rc *Connection) Send(ctx context.Context, command string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok { // Default deadline if none is set
		deadline = time.Now().Add(5 * time.Second)
	}
	if err := rc.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}

	_, err := rc.rw.WriteString(command + "\r\n")
	if err != nil {
		return err
	}

	return rc.rw.Flush()
}

func (rc *Connection) Receive(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	deadline, ok := ctx.Deadline()
	if !ok { // Default deadline if none is set
		deadline = time.Now().Add(5 * time.Second)
	}

	if err := rc.conn.SetReadDeadline(deadline); err != nil {
		return "", err
	}

	line, err := rc.rw.ReadString('\n')
	if err != nil {
		return "", err
	}

	switch line[0] {
	case '-': // Handle simple error
		return "", fmt.Errorf(strings.TrimSuffix(line[1:], "\r\n"))
	case '$': //Assume the reply is a bulk string ,array serialization ain't supported in this client
		length, _ := strconv.Atoi(strings.TrimSuffix(line[1:], "\r\n")) //trim the CRLF from our response
		if length == -1 {
			// This is a nil reply
			return "", nil
		}
		buf := make([]byte, length+2) // +2 for the CRLF (\r\n)
		_, err = rc.rw.Read(buf)
		if err != nil {
			return "", err
		}
		return string(buf[:length]), nil
	case '+': // Handle simple string, return the string without the '+' prefix
		return strings.TrimSuffix(line[1:], "\r\n"), nil
	default:
		return strings.TrimSuffix(line, "\r\n"), nil

	}

}

func (rc *Connection) Close() error {
	return rc.conn.Close()
}
