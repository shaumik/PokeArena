// Package mq is the RabbitMQ layer: topology declaration, publishers, and
// consumers. It owns connection management — the initial dial and any later
// reconnect are retried transparently, so callers never see a flaky broker.
package mq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"pokearena/internal/messages"

	amqp "github.com/rabbitmq/amqp091-go"
)

// newSourceID returns a short random hex tag for this broker instance. We
// don't need uuid-level uniqueness: collisions across processes within a
// single deployment are vanishingly unlikely and a collision's worst case is
// one dropped local-fan-out, which the client tolerates anyway.
func newSourceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("src-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Broker is a managed RabbitMQ connection with one shared publishing channel.
//
// sourceID is a per-process identifier stamped onto every PublishEvent so that
// in-process consumers (the gateway's Hub) can recognize events they themselves
// published and skip re-delivery — see EventQueue.Consume. It is stable for the
// life of the process; we don't try to round-trip it across reconnects because
// duplicates are at worst harmless (clients dedupe turns by number).
type Broker struct {
	url      string
	sourceID string
	mu       sync.Mutex // guards conn + pubCh
	conn     *amqp.Connection
	pubCh    *amqp.Channel
}

// SourceID returns the broker's per-process publisher tag — used by callers
// that also subscribe to their own event stream and need to filter self-publishes.
func (b *Broker) SourceID() string { return b.sourceID }

// Connect dials RabbitMQ (retrying briefly) and declares the topology.
func Connect(ctx context.Context, url string) (*Broker, error) {
	b := &Broker{url: url, sourceID: newSourceID()}
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		b.mu.Lock()
		err := b.ensureLocked()
		b.mu.Unlock()
		if err == nil {
			return b, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("connect rabbitmq: %w", lastErr)
}

// ensureLocked guarantees a live connection and publishing channel. The caller
// must hold b.mu.
func (b *Broker) ensureLocked() error {
	if b.conn != nil && !b.conn.IsClosed() {
		return nil
	}
	conn, err := amqp.Dial(b.url)
	if err != nil {
		return err
	}
	pub, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}
	if err := declareTopology(pub); err != nil {
		conn.Close()
		return err
	}
	b.conn, b.pubCh = conn, pub
	return nil
}

// declareTopology declares the exchanges and durable work queues. Idempotent.
func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(messages.ExchangeWork, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(messages.ExchangeEvents, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	for _, q := range []string{messages.QueueQuickSim, messages.QueueAI, messages.QueueLiveSession} {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return err
		}
		if err := ch.QueueBind(q, q, messages.ExchangeWork, false, nil); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the connection.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pubCh != nil {
		b.pubCh.Close()
	}
	if b.conn != nil {
		b.conn.Close()
	}
}

// --- publishing ---

// PublishJob sends a work job to a queue via the direct work exchange. Jobs
// are durable: a crash between publish and consume must not drop a battle.
func (b *Broker) PublishJob(ctx context.Context, queue string, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.publish(ctx, messages.ExchangeWork, queue, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	})
}

// PublishEvent sends a domain event with routing key "{eventType}.{battleID}".
//
// Events are transient: RabbitMQ holds them in memory and acks the publish
// without fsync'ing to disk. Per-turn events on the critical path of spectator
// fan-out can't afford the ~5-20ms fsync overhead, and losing them on a broker
// restart is acceptable — late spectators replay from Postgres on SSE attach,
// and an in-flight battle is already lost if the gateway dies.
//
// AppId carries this broker's sourceID so the publishing process can recognize
// its own events when they come back via the events exchange and skip
// re-dispatch (it already injected them locally via Hub.Inject).
func (b *Broker) PublishEvent(ctx context.Context, eventType, battleID string, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.publish(ctx, messages.ExchangeEvents, eventType+"."+battleID, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Transient,
		AppId:        b.sourceID,
		Timestamp:    time.Now(),
		Body:         body,
	})
}

func (b *Broker) publish(ctx context.Context, exchange, key string, pub amqp.Publishing) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLocked(); err != nil {
		return err
	}
	return b.pubCh.PublishWithContext(ctx, exchange, key, false, false, pub)
}

// --- consuming ---

func (b *Broker) consumerChannel() (*amqp.Channel, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLocked(); err != nil {
		return nil, err
	}
	return b.conn.Channel()
}

type (
	setupFunc    func(*amqp.Channel) (queue string, err error)
	deliveryFunc func(context.Context, amqp.Delivery) error
)

// consume runs a consumer until ctx is canceled, reconnecting on any failure.
func (b *Broker) consume(ctx context.Context, prefetch int, setup setupFunc, handle deliveryFunc) error {
	for ctx.Err() == nil {
		if err := b.consumeOnce(ctx, prefetch, setup, handle); err != nil && ctx.Err() == nil {
			log.Printf("mq: consumer error: %v — reconnecting", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
	return ctx.Err()
}

func (b *Broker) consumeOnce(ctx context.Context, prefetch int, setup setupFunc, handle deliveryFunc) error {
	ch, err := b.consumerChannel()
	if err != nil {
		return err
	}
	defer ch.Close()

	queue, err := setup(ch)
	if err != nil {
		return err
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries channel closed")
			}
			if err := handle(ctx, d); err != nil {
				log.Printf("mq: handler error on %s: %v", queue, err)
				_ = d.Nack(false, true) // requeue for another attempt
			} else {
				_ = d.Ack(false)
			}
		}
	}
}

// ConsumeJobs consumes a durable work queue with the given prefetch, calling
// handler per message; a handler error requeues the message.
func (b *Broker) ConsumeJobs(ctx context.Context, queue string, prefetch int, handler func(context.Context, []byte) error) error {
	return b.consume(ctx, prefetch,
		func(ch *amqp.Channel) (string, error) {
			if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
				return "", err
			}
			return queue, ch.QueueBind(queue, queue, messages.ExchangeWork, false, nil)
		},
		func(ctx context.Context, d amqp.Delivery) error { return handler(ctx, d.Body) })
}

// ConsumeEvents consumes a durable named queue bound to the events exchange
// with the given routing-key patterns — used by the leaderboard worker.
func (b *Broker) ConsumeEvents(ctx context.Context, queue string, routingKeys []string, prefetch int, handler func(ctx context.Context, routingKey string, body []byte) error) error {
	return b.consume(ctx, prefetch,
		func(ch *amqp.Channel) (string, error) {
			if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
				return "", err
			}
			for _, rk := range routingKeys {
				if err := ch.QueueBind(queue, rk, messages.ExchangeEvents, false, nil); err != nil {
					return "", err
				}
			}
			return queue, nil
		},
		func(ctx context.Context, d amqp.Delivery) error { return handler(ctx, d.RoutingKey, d.Body) })
}

// EventQueue is an exclusive, auto-delete queue on the events exchange whose
// bindings are managed dynamically — the gateway binds "*.{battleID}" while a
// client is connected and unbinds on disconnect, so an instance receives
// events only for battles it currently serves.
//
// selfSourceID matches Broker.sourceID for the broker that opened this queue.
// When a gateway publishes an event, the events exchange routes a copy back
// to this same queue (because of the "*.{battleID}" binding). The publisher
// has already fanned that event out in-process via Hub.Inject, so the Rabbit
// round-trip would be a duplicate — Consume drops deliveries whose AppId
// matches selfSourceID to avoid that.
type EventQueue struct {
	ch           *amqp.Channel
	name         string
	selfSourceID string
	mu           sync.Mutex // amqp.Channel is not safe for concurrent RPCs
}

// NewEventQueue declares the gateway's per-instance event queue.
func (b *Broker) NewEventQueue() (*EventQueue, error) {
	ch, err := b.consumerChannel()
	if err != nil {
		return nil, err
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil) // server-named, auto-delete, exclusive
	if err != nil {
		ch.Close()
		return nil, err
	}
	return &EventQueue{ch: ch, name: q.Name, selfSourceID: b.sourceID}, nil
}

// Bind starts routing events matching routingKey to this queue.
func (eq *EventQueue) Bind(routingKey string) error {
	eq.mu.Lock()
	defer eq.mu.Unlock()
	return eq.ch.QueueBind(eq.name, routingKey, messages.ExchangeEvents, false, nil)
}

// Unbind stops routing events matching routingKey to this queue.
func (eq *EventQueue) Unbind(routingKey string) error {
	eq.mu.Lock()
	defer eq.mu.Unlock()
	return eq.ch.QueueUnbind(eq.name, routingKey, messages.ExchangeEvents, nil)
}

// Consume delivers events to handler until ctx is canceled. Events are
// auto-acked: a missed live-push event is recoverable via the REST API.
//
// Deliveries whose AppId matches this queue's selfSourceID are skipped —
// they are the Rabbit round-trip of an event this process already injected
// locally via Hub.Inject.
func (eq *EventQueue) Consume(ctx context.Context, handler func(routingKey string, body []byte)) error {
	deliveries, err := eq.ch.Consume(eq.name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("event queue closed")
			}
			if d.AppId != "" && d.AppId == eq.selfSourceID {
				continue
			}
			handler(d.RoutingKey, d.Body)
		}
	}
}

// Close releases the event queue's channel.
func (eq *EventQueue) Close() error { return eq.ch.Close() }
