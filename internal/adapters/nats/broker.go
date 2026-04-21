package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/ports"
	"github.com/nats-io/nats.go"
)

// NATSBroker implements ports.MessageBroker using NATS.
type NATSBroker struct {
	conn    *nats.Conn
	timeout time.Duration
}

// natsSubscription wraps a NATS subscription to implement ports.Subscription.
type natsSubscription struct {
	sub *nats.Subscription
}

func (s *natsSubscription) Unsubscribe() error {
	return s.sub.Unsubscribe()
}

// New creates a new NATSBroker connected to the given URL.
func New(url string, opts ...nats.Option) (*NATSBroker, error) {
	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}
	return &NATSBroker{
		conn:    conn,
		timeout: 10 * time.Second,
	}, nil
}

// NewFromConn creates a NATSBroker from an existing connection.
func NewFromConn(conn *nats.Conn) *NATSBroker {
	return &NATSBroker{
		conn:    conn,
		timeout: 10 * time.Second,
	}
}

// SetTimeout overrides the default request timeout.
func (b *NATSBroker) SetTimeout(d time.Duration) {
	b.timeout = d
}

func (b *NATSBroker) Publish(_ context.Context, subject string, data []byte) error {
	return b.conn.Publish(subject, data)
}

func (b *NATSBroker) Subscribe(subject string, handler ports.MessageHandler) (ports.Subscription, error) {
	sub, err := b.conn.Subscribe(subject, func(msg *nats.Msg) {
		if err := handler(msg.Subject, msg.Data); err != nil {
			// Log error but don't crash the subscription.
			_ = err
		}
	})
	if err != nil {
		return nil, err
	}
	return &natsSubscription{sub: sub}, nil
}

func (b *NATSBroker) QueueSubscribe(subject, queue string, handler ports.MessageHandler) (ports.Subscription, error) {
	sub, err := b.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		if err := handler(msg.Subject, msg.Data); err != nil {
			_ = err
		}
	})
	if err != nil {
		return nil, err
	}
	return &natsSubscription{sub: sub}, nil
}

func (b *NATSBroker) Request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	timeout := b.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}
	msg, err := b.conn.Request(subject, data, timeout)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

func (b *NATSBroker) Close() error {
	b.conn.Close()
	return nil
}

// Conn returns the underlying NATS connection for advanced use.
func (b *NATSBroker) Conn() *nats.Conn {
	return b.conn
}
