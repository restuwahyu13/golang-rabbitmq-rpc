package pkg

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/lithammer/shortuuid"
	"github.com/wagslane/go-rabbitmq"
)

type interfaceRabbit interface {
	listeningConsumer(metadata publishMetadata, deliveryChan chan rabbitmq.Delivery)
	listeningConsumerRpc(deliveryChan chan rabbitmq.Delivery, delivery rabbitmq.Delivery)
	PublishRpc(queue string, body interface{}) (chan rabbitmq.Delivery, error)
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
	CorrelationId string      `json:"correlationId"`
	Data          interface{} `json:"data"`
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
	publishRequests []publishMetadata
	url             string = "amqp://admin:qwerty12@localhost:5672/"
	exchangeName    string = "rpc.pattern"
	ack             bool   = false
	concurrency     int    = runtime.NumCPU()
	deliveryChan           = make(chan rabbitmq.Delivery, 1)
)

func NewRabbitMQ() interfaceRabbit {
	connection, err := rabbitmq.NewConn(url, rabbitmq.WithConnectionOptionsLogging)

	if err != nil {
		defer connection.Close()
		log.Fatalf("RabbitMQ connection error: %s", err.Error())
	}

	return &structRabbit{connection: connection}
}

func (h *structRabbit) listeningConsumer(metadata publishMetadata, deliveryChan chan rabbitmq.Delivery) {
	h.rpcQueue = metadata.ReplyTo
	h.rpcConsumerId = metadata.CorrelationId

	log.Printf("START CONSUMER RPC %s", h.rpcQueue)

	rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		h.listeningConsumerRpc(deliveryChan, delivery)
		return rabbitmq.Ack
	},
		h.rpcQueue,
		rabbitmq.WithConsumerOptionsExchangeName(exchangeName),
		rabbitmq.WithConsumerOptionsExchangeKind(Direct),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsQueueAutoDelete,
		rabbitmq.WithConsumerOptionsConsumerName(h.rpcConsumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(concurrency),
		rabbitmq.WithConsumerOptionsLogging,
	)
}

func (h *structRabbit) listeningConsumerRpc(deliveryChan chan rabbitmq.Delivery, delivery rabbitmq.Delivery) {
	for _, d := range publishRequests {
		if d.CorrelationId == delivery.CorrelationId {
			defer close(deliveryChan)
			deliveryChan <- delivery
		} else {
			defer close(deliveryChan)
			deliveryChan <- rabbitmq.Delivery{}
		}
		continue
	}
}

func (h *structRabbit) PublishRpc(queue string, body interface{}) (chan rabbitmq.Delivery, error) {
	log.Printf("START PUBLISHER RPC %s", queue)

	publishRequest := publishMetadata{}
	publishRequest.CorrelationId = shortuuid.New()
	publishRequest.ReplyTo = fmt.Sprintf("rpc.%s", publishRequest.CorrelationId)
	publishRequest.ContentType = "application/json"
	publishRequest.Timestamp = time.Now().Local()

	h.listeningConsumer(publishRequest, deliveryChan)
	publishRequests = append(publishRequests, publishRequest)

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		return deliveryChan, err
	}

	bodyByte, err := json.Marshal(&body)
	if err != nil {
		return deliveryChan, err
	}

	afterTime := time.After(time.Duration(time.Second * 2))
	<-afterTime

	err = publisher.Publish(bodyByte, []string{queue},
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsMandatory,
		rabbitmq.WithPublishOptionsExchange(exchangeName),
		rabbitmq.WithPublishOptionsCorrelationID(publishRequest.CorrelationId),
		rabbitmq.WithPublishOptionsReplyTo(publishRequest.ReplyTo),
		rabbitmq.WithPublishOptionsContentType(publishRequest.ContentType),
		rabbitmq.WithPublishOptionsTimestamp(publishRequest.Timestamp),
	)

	if err != nil {
		return deliveryChan, err
	}

	defer publisher.Close()
	return deliveryChan, nil
}

func (h *structRabbit) ConsumerRpc(queue string, overwriteResponse *ConsumerOverwriteResponse) {
	log.Printf("START CONSUMER RPC %s", queue)

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
		log.Println("CONSUMER REPLY TO: ", delivery.ReplyTo)

		consumerResponse := consumerRpcResponse{}
		overwriteBodyByte, err := json.Marshal(&overwriteResponse.Res)
		if err != nil {
			log.Fatalf(err.Error())
		}

		if overwriteResponse != nil {
			consumerResponse.CorrelationId = delivery.CorrelationId
			consumerResponse.Data = string(overwriteBodyByte)
			consumerResponse.Timestamp = delivery.Timestamp

			bodyByte, err := json.Marshal(&consumerResponse)
			if err != nil {
				log.Fatalf(err.Error())
			}

			h.rpcConsumerRes = bodyByte
		} else {
			consumerResponse.CorrelationId = delivery.CorrelationId
			consumerResponse.Data = string(overwriteBodyByte)
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
				rabbitmq.WithPublishOptionsMandatory,
				rabbitmq.WithPublishOptionsCorrelationID(delivery.CorrelationId),
				rabbitmq.WithPublishOptionsContentType(delivery.ContentType),
				rabbitmq.WithPublishOptionsTimestamp(delivery.Timestamp),
			)
		}

		return rabbitmq.Ack
	},
		queue,
		rabbitmq.WithConsumerOptionsExchangeName(exchangeName),
		rabbitmq.WithConsumerOptionsExchangeKind(Direct),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey: queue,
			BindingOptions: rabbitmq.BindingOptions{
				Declare: true,
				NoWait:  false,
				Args:    nil,
			},
		}),
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsConsumerName(h.rpcConsumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(concurrency),
		rabbitmq.WithConsumerOptionsLogging,
	)
}
