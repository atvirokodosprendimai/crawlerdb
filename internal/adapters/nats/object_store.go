package broker

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// JetStreamObjectStore persists large payloads outside the pub/sub message bus.
type JetStreamObjectStore struct {
	store jetstream.ObjectStore
}

// NewObjectStore creates or updates a JetStream object store bucket.
func NewObjectStore(conn *nats.Conn, cfg jetstream.ObjectStoreConfig) (*JetStreamObjectStore, error) {
	if conn == nil {
		return nil, fmt.Errorf("nats connection is nil")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("object store bucket is empty")
	}

	js, err := jetstream.New(conn)
	if err != nil {
		return nil, fmt.Errorf("create jetstream client: %w", err)
	}
	store, err := js.CreateOrUpdateObjectStore(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("create or update object store %q: %w", cfg.Bucket, err)
	}
	return &JetStreamObjectStore{store: store}, nil
}

func (s *JetStreamObjectStore) PutBytes(ctx context.Context, name string, data []byte) (string, error) {
	if _, err := s.store.PutBytes(ctx, name, data); err != nil {
		return "", err
	}
	return name, nil
}

func (s *JetStreamObjectStore) GetBytes(ctx context.Context, name string) ([]byte, error) {
	return s.store.GetBytes(ctx, name)
}

func (s *JetStreamObjectStore) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}
