package ports

import "context"

// MessageHandler processes incoming messages.
type MessageHandler func(subject string, data []byte) error

// Subscription represents an active subscription that can be unsubscribed.
type Subscription interface {
	Unsubscribe() error
}

// MessageBroker abstracts pub/sub and request/reply messaging.
type MessageBroker interface {
	Publish(ctx context.Context, subject string, data []byte) error
	Subscribe(subject string, handler MessageHandler) (Subscription, error)
	QueueSubscribe(subject, queue string, handler MessageHandler) (Subscription, error)
	Request(ctx context.Context, subject string, data []byte) ([]byte, error)
	Close() error
}
