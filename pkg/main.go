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
	PublishRpc(queue string, data interface{}, deliveryChan chan rabbitmq.Delivery) error
	ConsumerRpc(queue string, overwriteResponse []byte)
}

type publishMetadata struct {
	CorrelationId string    `json:"correlationId"`
	ReplyTo       string    `json:"replyTo"`
	ContentType   string    `json:"contentType"`
	Timestamp     time.Time `json:"timestamp"`
}

type consumerRpcResponse struct {
	Data interface{} `json:"data"`
}

type structRabbit struct {
	connection     *rabbitmq.Conn
	rpcQueue       string
	rpcConsumerId  string
	rpcConsumerRes []byte
}

var publishRequests []publishMetadata
var url string = "amqp://admin:qwerty12@localhost:5672/"
var exchangeName string = "rpc.pattern"
var ack bool = false
var cpus int = runtime.NumCPU()

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
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsQueueAutoDelete,
		rabbitmq.WithConsumerOptionsConsumerName(h.rpcConsumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(cpus),
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

func (h *structRabbit) PublishRpc(queue string, data interface{}, deliveryChan chan rabbitmq.Delivery) error {
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
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		return err
	}

	bodyByte, err := json.Marshal(&data)
	if err != nil {
		return err
	}

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
		return err
	}

	defer publisher.Close()
	return nil
}

func (h *structRabbit) ConsumerRpc(queue string, overwriteResponse []byte) {
	log.Printf("START CONSUMER RPC %s", queue)

	h.rpcConsumerId = shortuuid.New()

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		log.Fatalf("Publisher error : %s", err.Error())
	}

	rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		log.Println("CONSUMER REPLY TO: ", delivery.ReplyTo)

		if overwriteResponse != nil {
			bodyByte, _ := json.Marshal(&consumerRpcResponse{Data: string(overwriteResponse)})
			h.rpcConsumerRes = bodyByte
		} else {
			bodyByte, _ := json.Marshal(&consumerRpcResponse{Data: string(delivery.Body)})
			h.rpcConsumerRes = bodyByte
		}

		if len(delivery.ReplyTo) > 0 {
			publisher.Publish(h.rpcConsumerRes, []string{delivery.ReplyTo},
				rabbitmq.WithPublishOptionsPersistentDelivery,
				rabbitmq.WithPublishOptionsMandatory,
				rabbitmq.WithPublishOptionsCorrelationID(delivery.CorrelationId),
			)
		}

		return rabbitmq.Ack
	},
		queue,
		rabbitmq.WithConsumerOptionsExchangeName(exchangeName),
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey: queue,
			BindingOptions: rabbitmq.BindingOptions{
				Declare: true,
				NoWait:  false,
				Args:    nil,
			},
		}),
		rabbitmq.WithConsumerOptionsQueueDurable,
		rabbitmq.WithConsumerOptionsConsumerName(h.rpcConsumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(cpus),
		rabbitmq.WithConsumerOptionsLogging,
	)
}
