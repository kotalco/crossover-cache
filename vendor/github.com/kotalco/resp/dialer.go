package resp

import (
	"context"
	"net"
)

type IDialer interface {
	Dial(ctx context.Context, address string) (net.Conn, error)
}

type Dialer struct{}

func NewDialer() IDialer {
	return &Dialer{}
}

func (d Dialer) Dial(ctx context.Context, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", address)

}
