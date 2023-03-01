package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/lithammer/shortuuid"
	"github.com/wagslane/go-rabbitmq"
)

type interfaceRabbit interface {
	listeningConsumer(ctx context.Context, metadata *publishMetadata, isMatchChan chan bool, deliveryChan chan rabbitmq.Delivery)
	listeningConsumerRpc(ctx context.Context, isMatchChan chan bool, deliveryChan chan rabbitmq.Delivery, delivery rabbitmq.Delivery) context.Context
	PublishRpc(ctx context.Context, deliveryChan chan rabbitmq.Delivery, queue string, body interface{}) (bool, error)
	ConsumerRpc(queue string, consumerOverwriteResponse *ConsumerOverwriteResponse)
}

const (
	Direct = "direct"
	Fanout = "fanout"
	Topic  = "topic"
	Header = "header"
)

type publishMetadata struct {
	CorrelationId string    `json:"correlationId"`
	ReplyTo       string    `json:"replyTo"`
	ContentType   string    `json:"contentType"`
	Timestamp     time.Time `json:"timestamp"`
}

type consumerRpcResponse struct {
	Data          interface{} `json:"data"`
	CorrelationId string      `json:"correlationId"`
	ReplyTo       string      `json:"replyTo"`
	ContentType   string      `json:"contentType"`
	Timestamp     time.Time   `json:"timestamp"`
}

type ConsumerOverwriteResponse struct {
	Res interface{} `json:"res"`
}

type structRabbit struct {
	connection     *rabbitmq.Conn
	rpcQueue       string
	rpcConsumerId  string
	rpcConsumerRes []byte
}

var (
	publishRequest  publishMetadata   = publishMetadata{}
	publishRequests []publishMetadata = []publishMetadata{}
	url             string            = "amqp://admin:qwerty12@localhost:5672/"
	exchangeName    string            = "rpc.pattern"
	ack             bool              = false
	concurrency     int               = runtime.NumCPU()
	mutex           sync.Mutex        = sync.Mutex{}
	isMatchChan                       = make(chan bool, 1)
)

func NewRabbitMQ() interfaceRabbit {
	connection, err := rabbitmq.NewConn(url,
		rabbitmq.WithConnectionOptionsLogging,
		rabbitmq.WithConnectionOptionsConfig(rabbitmq.Config{
			Heartbeat: time.Duration(time.Second * 3),
		}))

	if err != nil {
		defer connection.Close()
		log.Fatalf("RabbitMQ connection error: %s", err.Error())
	}

	return &structRabbit{connection: connection}
}

func (h *structRabbit) listeningConsumer(ctx context.Context, metadata *publishMetadata, isMatchChan chan bool, deliveryChan chan rabbitmq.Delivery) {
	h.rpcQueue = metadata.ReplyTo
	h.rpcConsumerId = metadata.CorrelationId

	log.Printf("START CLIENT CONSUMER RPC -> %s", h.rpcQueue)

	rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		for _, d := range publishRequests {
			if d.CorrelationId != delivery.CorrelationId {
				isMatchChan <- false
				h.listeningConsumerRpc(ctx, isMatchChan, deliveryChan, delivery)

				return rabbitmq.NackRequeue
			}
		}

		isMatchChan <- true

		ctx = h.listeningConsumerRpc(ctx, isMatchChan, deliveryChan, delivery)
		deliveryChan <- ctx.Value("queue").(rabbitmq.Delivery)

		return rabbitmq.Ack
	},
		h.rpcQueue,
		rabbitmq.WithConsumerOptionsExchangeName(exchangeName),
		rabbitmq.WithConsumerOptionsExchangeKind(Direct),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsExchangeNoWait,
		rabbitmq.WithConsumerOptionsQueueNoWait,
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsQueueAutoDelete,
		rabbitmq.WithConsumerOptionsConsumerNoWait,
		rabbitmq.WithConsumerOptionsConsumerName(h.rpcConsumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(concurrency),
		rabbitmq.WithConsumerOptionsLogging,
	)
}

func (h *structRabbit) listeningConsumerRpc(ctx context.Context, isMatchChan chan bool, deliveryChan chan rabbitmq.Delivery, delivery rabbitmq.Delivery) context.Context {
	for _, d := range publishRequests {
		select {
		case ok := <-isMatchChan:
			if ok && d.CorrelationId == delivery.CorrelationId {
				ctx = context.WithValue(ctx, "queue", delivery)
			} else {
				ctx = context.WithValue(ctx, "queue", rabbitmq.Delivery{})
			}
		default:
			ctx = context.WithValue(ctx, "queue", rabbitmq.Delivery{})
		}
	}
	return ctx
}

func (h *structRabbit) PublishRpc(ctx context.Context, deliveryChan chan rabbitmq.Delivery, queue string, body interface{}) (bool, error) {
	log.Printf("START PUBLISHER RPC -> %s", queue)

	if len(publishRequests) > 0 {
		publishRequests = nil
	}

	publishRequest.CorrelationId = shortuuid.New()
	publishRequest.ReplyTo = fmt.Sprintf("rpc.%s", publishRequest.CorrelationId)
	publishRequest.ContentType = "application/json"
	publishRequest.Timestamp = time.Now().Local()

	defer mutex.Unlock()
	mutex.Lock()
	publishRequests = append(publishRequests, publishRequest)

	h.listeningConsumer(ctx, &publishRequest, isMatchChan, deliveryChan)

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		return false, err
	}

	bodyByte, err := json.Marshal(&body)
	if err != nil {
		return false, err
	}

	afterTime := time.After(time.Duration(time.Second * 2))
	<-afterTime

	err = publisher.Publish(bodyByte, []string{queue},
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsExchange(exchangeName),
		rabbitmq.WithPublishOptionsCorrelationID(publishRequest.CorrelationId),
		rabbitmq.WithPublishOptionsReplyTo(publishRequest.ReplyTo),
		rabbitmq.WithPublishOptionsContentType(publishRequest.ContentType),
		rabbitmq.WithPublishOptionsTimestamp(publishRequest.Timestamp),
	)

	if err != nil {
		return false, err
	}

	defer publisher.Close()
	return true, nil
}

func (h *structRabbit) ConsumerRpc(queue string, overwriteResponse *ConsumerOverwriteResponse) {
	log.Printf("START SERVER CONSUMER RPC -> %s", queue)

	h.rpcConsumerId = shortuuid.New()

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		log.Fatalf("Publisher error : %s", err.Error())
	}

	rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		log.Println("SERVER CONSUMER RPC CORRELATION ID: ", delivery.CorrelationId)
		log.Println("SERVER CONSUMER RPC REPLY TO: ", delivery.ReplyTo)

		consumerResponse := consumerRpcResponse{}
		overwriteBodyByte, err := json.Marshal(&overwriteResponse.Res)
		if err != nil {
			log.Fatalf(err.Error())
		}

		if overwriteResponse != nil {
			consumerResponse.Data = string(delivery.Body)
			consumerResponse.CorrelationId = delivery.CorrelationId
			consumerResponse.ReplyTo = delivery.ReplyTo
			consumerResponse.ContentType = delivery.ContentType
			consumerResponse.Timestamp = delivery.Timestamp

			bodyByte, err := json.Marshal(&consumerResponse)
			if err != nil {
				log.Fatalf(err.Error())
			}

			h.rpcConsumerRes = bodyByte
		} else {
			consumerResponse.Data = string(overwriteBodyByte)
			consumerResponse.CorrelationId = delivery.CorrelationId
			consumerResponse.ReplyTo = delivery.ReplyTo
			consumerResponse.ContentType = delivery.ContentType
			consumerResponse.Timestamp = delivery.Timestamp

			bodyByte, err := json.Marshal(&consumerResponse)
			if err != nil {
				log.Fatalf(err.Error())
			}

			h.rpcConsumerRes = bodyByte
		}

		if len(delivery.ReplyTo) > 0 {
			publisher.Publish(h.rpcConsumerRes, []string{delivery.ReplyTo},
				rabbitmq.WithPublishOptionsPersistentDelivery,
				rabbitmq.WithPublishOptionsCorrelationID(delivery.CorrelationId),
				rabbitmq.WithPublishOptionsContentType(delivery.ContentType),
				rabbitmq.WithPublishOptionsTimestamp(delivery.Timestamp),
			)

			return rabbitmq.Ack
		}

		return rabbitmq.NackRequeue
	},
		queue,
		rabbitmq.WithConsumerOptionsExchangeName(exchangeName),
		rabbitmq.WithConsumerOptionsExchangeKind(Direct),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey: queue,
			BindingOptions: rabbitmq.BindingOptions{
				Declare: true,
				NoWait:  true,
				Args:    nil,
			},
		}),
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsExchangeNoWait,
		rabbitmq.WithConsumerOptionsQueueNoWait,
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsConsumerNoWait,
		rabbitmq.WithConsumerOptionsConsumerName(h.rpcConsumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(concurrency),
		rabbitmq.WithConsumerOptionsLogging,
	)
}
