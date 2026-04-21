package broker_test

import (
	"context"
	"sync"
	"testing"
	"time"

	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestServer(t *testing.T) *server.Server {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1 // random port
	s := natsserver.RunServer(&opts)
	t.Cleanup(s.Shutdown)
	return s
}

func newBroker(t *testing.T, s *server.Server) *broker.NATSBroker {
	t.Helper()
	b, err := broker.New(s.ClientURL(), nats.Name("test"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestPublishSubscribe(t *testing.T) {
	s := startTestServer(t)
	b := newBroker(t, s)
	ctx := context.Background()

	var received []byte
	var wg sync.WaitGroup
	wg.Add(1)

	sub, err := b.Subscribe("test.subject", func(subject string, data []byte) error {
		received = data
		wg.Done()
		return nil
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	err = b.Publish(ctx, "test.subject", []byte("hello"))
	require.NoError(t, err)

	wg.Wait()
	assert.Equal(t, []byte("hello"), received)
}

func TestQueueSubscribe(t *testing.T) {
	s := startTestServer(t)

	// Two brokers in same queue group.
	b1 := newBroker(t, s)
	b2 := newBroker(t, s)
	ctx := context.Background()

	var mu sync.Mutex
	counts := map[string]int{}
	var wg sync.WaitGroup
	wg.Add(10)

	handler := func(name string) func(string, []byte) error {
		return func(_ string, _ []byte) error {
			mu.Lock()
			counts[name]++
			mu.Unlock()
			wg.Done()
			return nil
		}
	}

	sub1, err := b1.QueueSubscribe("work.queue", "workers", handler("b1"))
	require.NoError(t, err)
	defer sub1.Unsubscribe()

	sub2, err := b2.QueueSubscribe("work.queue", "workers", handler("b2"))
	require.NoError(t, err)
	defer sub2.Unsubscribe()

	// Publish 10 messages.
	publisher := newBroker(t, s)
	for range 10 {
		require.NoError(t, publisher.Publish(ctx, "work.queue", []byte("task")))
	}

	wg.Wait()
	mu.Lock()
	total := counts["b1"] + counts["b2"]
	mu.Unlock()
	assert.Equal(t, 10, total, "all messages delivered")
	// Both should get some (probabilistic, but with 10 messages very likely).
}

func TestRequestReply(t *testing.T) {
	s := startTestServer(t)
	b := newBroker(t, s)
	ctx := context.Background()

	// Set up responder.
	conn := b.Conn()
	_, err := conn.Subscribe("service.echo", func(msg *nats.Msg) {
		_ = msg.Respond([]byte("echo:" + string(msg.Data)))
	})
	require.NoError(t, err)

	reply, err := b.Request(ctx, "service.echo", []byte("ping"))
	require.NoError(t, err)
	assert.Equal(t, []byte("echo:ping"), reply)
}

func TestRequestTimeout(t *testing.T) {
	s := startTestServer(t)
	b := newBroker(t, s)
	b.SetTimeout(100 * time.Millisecond)

	ctx := context.Background()
	_, err := b.Request(ctx, "no.responder", []byte("ping"))
	assert.Error(t, err) // Should timeout.
}

func TestSubjects(t *testing.T) {
	assert.Equal(t, "crawl.dispatch.job123", broker.CrawlDispatchSubject("job123"))
	assert.Equal(t, "crawl.result.job123", broker.CrawlResultSubject("job123"))
	assert.Equal(t, "metrics.job123", broker.MetricsSubject("job123"))
	assert.Equal(t, "gui.push.job123", broker.GUIPushSubject("job123"))
	assert.Equal(t, "job.command.job123", broker.JobCommandSubject("job123"))
	assert.Equal(t, "webhook.url.blocked", broker.WebhookSubject("url.blocked"))
}
