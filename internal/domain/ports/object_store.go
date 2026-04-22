package ports

import "context"

// ObjectStore transports large crawl payloads outside the event bus.
type ObjectStore interface {
	PutBytes(ctx context.Context, name string, data []byte) (string, error)
	GetBytes(ctx context.Context, name string) ([]byte, error)
	Delete(ctx context.Context, name string) error
}
