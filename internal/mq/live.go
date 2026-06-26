package mq

import (
	"context"
	"encoding/json"
	"time"

	"pokearena/internal/messages"
	"pokearena/internal/protocol"

	amqp "github.com/rabbitmq/amqp091-go"
)

// liveActionQueueTTL is the x-expires on a per-battle action queue: RabbitMQ
// deletes it after this long with no consumers and no pending messages. It's a
// backstop against leaked queues from abandoned battles — during play the owner
// is consuming, so the queue never expires. The owner also deletes it
// explicitly when the battle ends; this just catches the crash case.
const liveActionQueueTTL = time.Hour

// --- session election (work exchange) ---

// PublishLiveSession enqueues a session-start job. Competing consumers on
// QueueLiveSession mean exactly one battle-session instance picks it up and
// becomes the owner of that battle.
//
// It first declares the battle's durable action queue. A player's WS bridge
// publishes its attach/submit the instant the socket opens — which can race
// ahead of the elected owner declaring that queue lazily on consume. The work
// exchange is direct, so an action with no bound queue is silently dropped
// (unroutable, mandatory=false) and the player's first move vanishes. Declaring
// the durable queue up front, at create time, means early actions are retained
// until the owner drains them. The queue survives owner death too, so a failover
// owner inherits any unacked actions.
func (b *Broker) PublishLiveSession(ctx context.Context, start messages.LiveSessionStart) error {
	if err := b.DeclareLiveActionQueue(start.BattleID); err != nil {
		return err
	}
	return b.PublishJob(ctx, messages.QueueLiveSession, start)
}

// DeclareLiveActionQueue idempotently declares and binds a battle's durable
// action queue. Safe to call repeatedly and from any process — the consumer's
// own declare uses identical arguments.
func (b *Broker) DeclareLiveActionQueue(battleID string) error {
	ch, err := b.consumerChannel()
	if err != nil {
		return err
	}
	defer ch.Close()
	_, err = declareLiveActionQueue(ch, battleID)
	return err
}

// declareLiveActionQueue declares the durable per-battle action queue with the
// canonical arguments and binds it to live.action.{battleID} on the work
// exchange. The declaration args MUST match everywhere or RabbitMQ rejects the
// redeclare, so both the publisher-side provisioning and the consumer share it.
func declareLiveActionQueue(ch *amqp.Channel, battleID string) (string, error) {
	key := messages.LiveActionKey(battleID)
	args := amqp.Table{"x-expires": liveActionQueueTTL.Milliseconds()}
	if _, err := ch.QueueDeclare(key, true, false, false, false, args); err != nil {
		return "", err
	}
	return key, ch.QueueBind(key, key, messages.ExchangeWork, false, nil)
}

// ConsumeLiveSession runs a competing consumer on the session work queue. One
// session-start job → one owner. prefetch bounds how many battles one instance
// will pick up before acking.
func (b *Broker) ConsumeLiveSession(ctx context.Context, prefetch int, handler func(context.Context, []byte) error) error {
	return b.ConsumeJobs(ctx, messages.QueueLiveSession, prefetch, handler)
}

// --- inbound actions (work exchange, durable, per battle) ---

// PublishLiveAction relays one player message to a battle's owner. Routing key
// live.action.{battleID}; persistent so a crash between the WS read and the
// owner's ack can't silently drop a move.
func (b *Broker) PublishLiveAction(ctx context.Context, a messages.LiveAction) error {
	return b.PublishJob(ctx, messages.LiveActionKey(a.BattleID), a)
}

// ConsumeLiveActions declares the durable per-battle action queue, binds it to
// live.action.{battleID} on the work exchange, and consumes with manual ack
// until ctx is canceled. The queue survives a brief owner outage (so a failover
// owner can drain unacked actions) and self-deletes after liveActionQueueTTL of
// disuse. The handler must dedup by turn — RabbitMQ may redeliver.
func (b *Broker) ConsumeLiveActions(ctx context.Context, battleID string, prefetch int, handler func(context.Context, []byte) error) error {
	return b.consume(ctx, prefetch,
		func(ch *amqp.Channel) (string, error) {
			return declareLiveActionQueue(ch, battleID)
		},
		func(ctx context.Context, d amqp.Delivery) error { return handler(ctx, d.Body) })
}

// DeleteLiveActionQueue removes a battle's action queue. Called by the owner at
// battle end so a finished battle leaves nothing behind. Best-effort.
func (b *Broker) DeleteLiveActionQueue(battleID string) error {
	ch, err := b.consumerChannel()
	if err != nil {
		return err
	}
	defer ch.Close()
	_, err = ch.QueueDelete(messages.LiveActionKey(battleID), false, false, false)
	return err
}

// --- outbound frames (events exchange, topic, loss-tolerant) ---

// PublishFrame publishes one per-slot server frame on the events exchange with
// routing key live.frame.{battleID}.{slot}. Transient, like domain events:
// dropping a frame on a broker blip is acceptable because the client resyncs
// from the persisted BattleState. The gateway holding that slot's socket binds
// the key (see Hub.SubscribeFrames) and forwards the bytes to the WebSocket.
func (b *Broker) PublishFrame(ctx context.Context, battleID, slot string, u protocol.MatchUpdate) error {
	body, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return b.publish(ctx, messages.ExchangeEvents, messages.LiveFrameKey(battleID, slot), amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Transient,
		AppId:        b.sourceID,
		Timestamp:    time.Now(),
		Body:         body,
	})
}
