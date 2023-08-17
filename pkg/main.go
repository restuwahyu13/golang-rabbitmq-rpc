package pkg

import (
	"crypto/tls"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/lithammer/shortuuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"github.com/wagslane/go-rabbitmq"
)

const (
	Direct = "direct"
	Fanout = "fanout"
	Topic  = "topic"
	Header = "header"
)

type (
	RabbitmqInterface interface {
		listeningConsumer(metadata *publishMetadata)
		PublisherRpc(queue string, body interface{}) (chan []byte, error)
		ConsumerRpc(queue string, callback func(d rabbitmq.Delivery) (action rabbitmq.Action))
		ReplyDeliveryPublisher(deliveryBodyTo []byte, delivery rabbitmq.Delivery)
	}

	publishMetadata struct {
		CorrelationId string    `json:"correlationId"`
		ReplyTo       string    `json:"replyTo"`
		ContentType   string    `json:"contentType"`
		Timestamp     time.Time `json:"timestamp"`
	}

	rabbitmqStruct struct {
		connection     *rabbitmq.Conn
		rpcQueue       string
		rpcConsumerId  string
		rpcReplyTo     string
		rpcConsumerRes []byte
	}

	RabbitMQOptions struct {
		Url         string
		Exchange    string
		Concurrency string
	}
)

var (
	publishRequest   publishMetadata   = publishMetadata{}
	publishRequests  []publishMetadata = []publishMetadata{}
	url              string            = "amqp://gues:guest@localhost:5672/"
	exchangeName     string            = "amqp.direct"
	publisherExpired string            = "1800"
	ack              bool              = false
	concurrency      int               = runtime.NumCPU()
	mutex            *sync.Mutex       = &sync.Mutex{}
	shortId          string            = shortuuid.New()
	args             amqp091.Table     = amqp091.Table{}
	durationChan     chan float64      = make(chan float64, 1)
	deliveryChan     chan []byte       = make(chan []byte, 1)
)

func NewRabbitMQ(options *RabbitMQOptions) RabbitmqInterface {
	url = options.Url
	exchangeName = options.Exchange
	concurrency, _ = strconv.Atoi(options.Concurrency)

	connection, err := rabbitmq.NewConn(url,
		rabbitmq.WithConnectionOptionsLogging,
		rabbitmq.WithConnectionOptionsConfig(rabbitmq.Config{
			Heartbeat:       time.Duration(time.Second * 3),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}))

	if err != nil {
		defer logrus.Fatalf("NewRabbitMQ - rabbitmq.NewConn error: %s", err.Error())
		connection.Close()
	}

	defer logrus.Info("RabbitMQ connection success")
	return &rabbitmqStruct{connection: connection}
}

/**
* ===========================================
* HANDLER METHOD - PUBLISHER RPC
* ===========================================
**/

func (h *rabbitmqStruct) listeningConsumer(metadata *publishMetadata) {
	logrus.Infof("START CLIENT CONSUMER RPC -> %s", metadata.ReplyTo)

	h.rpcQueue = metadata.ReplyTo
	h.rpcConsumerId = metadata.CorrelationId

	start := time.Now()
	since := time.Since(start)
	durationChan <- float64(since.Nanoseconds())

	d := <-durationChan

	if d <= 100 {
		d = 50
	} else if d >= 600 {
		d = 100
	}

	durationChan <- math.Round((d / 120))

	consumer, err := rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		logrus.Info("=============== START DUMP listeningConsumerRpc REQUEST ================")
		fmt.Print("\n")
		logrus.Infof("PUBLISHER METADATA: %s", publishRequests)
		logrus.Infof("CONSUMER BODY: %s", string(delivery.Body))
		logrus.Infof("CONSUMER CORRELATIONID: %s", string(delivery.CorrelationId))
		logrus.Infof("CONSUMER REPLYTO: %s", string(delivery.ReplyTo))
		fmt.Print("\n")
		logrus.Info("=============== END DUMP listeningConsumerRpc REQUEST =================")
		fmt.Print("\n")

		for _, d := range publishRequests {
			if d.CorrelationId != delivery.CorrelationId {
				deliveryChan <- delivery.Body
				return rabbitmq.Ack
			}
		}

		deliveryChan <- delivery.Body
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

	if err != nil {
		defer logrus.Errorf("RabbitMQ - rabbitmq.NewConsumer Error: %s", err.Error())
		consumer.Close()
		return
	}
}

func (h *rabbitmqStruct) PublisherRpc(queue string, body interface{}) (chan []byte, error) {
	defer logrus.Infof("START CLIENT PUBLISHER RPC -> %s", queue)

	if len(publishRequests) > 0 {
		publishRequests = nil
	}

	publishRequest.CorrelationId = shortuuid.New()
	publishRequest.ReplyTo = fmt.Sprintf("rpc.%s", shortId)
	publishRequest.ContentType = "application/json"
	publishRequest.Timestamp = time.Now().Local()

	mutex.Lock()
	defer mutex.Unlock()

	publishRequests = append(publishRequests, publishRequest)
	go h.listeningConsumer(&publishRequest)

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		defer logrus.Errorf("PublisherRpc - rabbitmq.NewPublisher Error: %s", err.Error())
		publisher.Close()
		return nil, err
	}

	bodyByte, err := sonic.Marshal(&body)
	if err != nil {
		defer logrus.Errorf("PublisherRpc - json.Marshal Error: %s", err.Error())
		publisher.Close()
		return nil, err
	}

	err = publisher.Publish(bodyByte, []string{queue},
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsExchange(exchangeName),
		rabbitmq.WithPublishOptionsCorrelationID(publishRequest.CorrelationId),
		rabbitmq.WithPublishOptionsReplyTo(publishRequest.ReplyTo),
		rabbitmq.WithPublishOptionsContentType(publishRequest.ContentType),
		rabbitmq.WithPublishOptionsTimestamp(publishRequest.Timestamp),
		rabbitmq.WithPublishOptionsExpiration(publisherExpired),
	)

	if err != nil {
		defer logrus.Errorf("PublisherRpc - publisher.Publish Error: %s", err.Error())
		publisher.Close()
		return nil, err
	}

	d := <-durationChan
	<-time.After(time.Duration(time.Second * time.Duration(d)))

	defer publisher.Close()
	return deliveryChan, nil
}

func (h *rabbitmqStruct) ConsumerRpc(queue string, callback func(delivery rabbitmq.Delivery) (action rabbitmq.Action)) {
	logrus.Infof("START SERVER CONSUMER RPC -> %s", queue)

	h.rpcConsumerId = shortuuid.New()
	h.rpcReplyTo = queue
	ack = false

	consumer, err := rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		logrus.Info("=============== START DUMP ConsumerRpc REQUEST ================")
		fmt.Print("\n")
		logrus.Infof("SERVER CONSUMER RPC BODY: %s", string(delivery.Body))
		logrus.Infof("SERVER CONSUMER RPC CORRELATION ID: %s", delivery.CorrelationId)
		logrus.Infof("SERVER CONSUMER RPC REPLY TO: %s", delivery.ReplyTo)
		fmt.Print("\n")
		logrus.Info("=============== END DUMP ConsumerRpc REQUEST =================")
		fmt.Print("\n")

		return callback(delivery)
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

	if err != nil {
		defer logrus.Errorf("ConsumerRpc - rabbitmq.NewConsumer Error: %s", err.Error())
		consumer.Close()
		return
	}
}

func (h *rabbitmqStruct) ReplyDeliveryPublisher(deliveryBodyTo []byte, delivery rabbitmq.Delivery) {
	if deliveryBodyTo != nil {
		h.rpcConsumerRes = deliveryBodyTo
	} else {
		h.rpcConsumerRes = delivery.Body
	}

	if len(delivery.ReplyTo) > 0 {
		h.rpcReplyTo = delivery.ReplyTo
	}

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsExchangeNoWait,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		defer logrus.Errorf("ReplyDeliveryPublisher - rabbitmq.NewPublisher Error: %s", err.Error())
		publisher.Close()
		return
	}

	err = publisher.Publish(h.rpcConsumerRes, []string{h.rpcReplyTo},
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsCorrelationID(delivery.CorrelationId),
		rabbitmq.WithPublishOptionsContentType(delivery.ContentType),
		rabbitmq.WithPublishOptionsTimestamp(delivery.Timestamp),
		rabbitmq.WithPublishOptionsExpiration(publisherExpired),
	)

	if err != nil {
		defer logrus.Errorf("ReplyDeliveryPublisher - publisher.Publish Error: %s", err.Error())
		publisher.Close()
		return
	}
}
