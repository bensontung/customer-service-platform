package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Rabbit struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	queue      string
}

func OpenRabbit(url, queue string) (*Rabbit, error) {
	if url == "" {
		return nil, fmt.Errorf("AMQP_URL is required")
	}
	connection, err := amqp.DialConfig(url, amqp.Config{Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return nil, err
	}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if _, err = channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, err
	}
	if err = channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return nil, err
	}
	return &Rabbit{connection: connection, channel: channel, queue: queue}, nil
}
func (r *Rabbit) Publish(ctx context.Context, eventID string, payload []byte) error {
	confirmation, err := r.channel.PublishWithDeferredConfirmWithContext(ctx, "", r.queue, true, false, amqp.Publishing{MessageId: eventID, DeliveryMode: amqp.Persistent, ContentType: "application/json", Timestamp: time.Now().UTC(), Body: payload})
	if err != nil {
		return err
	}
	if confirmation == nil {
		return fmt.Errorf("RabbitMQ publisher confirmation is unavailable")
	}
	confirmed, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("RabbitMQ rejected message")
	}
	return nil
}
func (r *Rabbit) Consume(ctx context.Context, handler func(context.Context, string, []byte) error) error {
	delivery, err := r.channel.Consume(r.queue, "customer-service", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-delivery:
			if !ok {
				return fmt.Errorf("RabbitMQ consumer closed")
			}
			var payload json.RawMessage = message.Body
			if err = handler(ctx, message.MessageId, payload); err != nil {
				if nackErr := message.Nack(false, true); nackErr != nil {
					return nackErr
				}
			} else if err = message.Ack(false); err != nil {
				return err
			}
		}
	}
}
func (r *Rabbit) Close() error {
	if err := r.channel.Close(); err != nil {
		_ = r.connection.Close()
		return err
	}
	return r.connection.Close()
}
