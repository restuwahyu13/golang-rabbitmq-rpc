package pkg

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/bytedance/sonic"
	"github.com/lithammer/shortuuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/wagslane/go-rabbitmq"
)

const (
	Direct = "direct"
	Fanout = "fanout"
	Topic  = "topic"
	Header = "header"
)

type (
	MessageBrokerHeader struct {
		PrivateKey   string `json:"privateKey,omitempty"`
		ClientKey    string `json:"clientKey"`
		Url          string `json:"url"`
		Method       string `json:"method"`
		AccessToken  string `json:"accessToken"`
		ClientSecret string `json:"clientSecret"`
		TimeStamp    string `json:"timeStamp,omitempty"`
	}

	MessageBrokerReq struct {
		Service string              `json:"service"`
		Header  MessageBrokerHeader `json:"header"`
		Body    interface{}         `json:"body"`
	}

	MessageBrokerRes struct {
		Code     int         `json:"code"`
		BankCode string      `json:"bankCode"`
		Message  string      `json:"message,omitempty"`
		Error    interface{} `json:"error,omitempty"`
		Data     interface{} `json:"data"`
	}

	QueueResponse struct {
		Items interface{}
	}

	RabbitmqInterface interface {
		Connection() *rabbitmq.Conn
		Publisher(queue string, body interface{}) error
		Consumer(eventName string, callback func(d rabbitmq.Delivery) (action rabbitmq.Action))
		PublisherRpc(queue string, body interface{}) ([]byte, error)
		ConsumerRpc(queue string, callback func(d rabbitmq.Delivery) (action rabbitmq.Action))
		ReplyToDeliveryPublisher(deliveryBodyTo []byte, delivery rabbitmq.Delivery)
	}

	publishMetadata struct {
		CorrelationId string    `json:"correlationId"`
		ReplyTo       string    `json:"replyTo"`
		ContentType   string    `json:"contentType"`
		Timestamp     time.Time `json:"timestamp"`
	}

	consumerRpcResponse struct {
		Data          interface{} `json:"data"`
		CorrelationId string      `json:"correlationId"`
		ReplyTo       string      `json:"replyTo"`
		ContentType   string      `json:"contentType"`
		Timestamp     time.Time   `json:"timestamp"`
	}

	ConsumerOverwriteResponse struct {
		Res interface{} `json:"res"`
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
		Expired     string
	}
)

var (
	publishRequest        publishMetadata   = publishMetadata{}
	publishRequests       []publishMetadata = []publishMetadata{}
	url                   string            = "amqp://gues:guest@localhost:5672/"
	exchangeName          string            = "amqp.direct"
	publisherExpired      string            = "1800"
	ack                   bool              = false
	concurrency           int               = runtime.NumCPU()
	shortId               string            = shortuuid.New()
	mutex                 *sync.RWMutex     = &sync.RWMutex{}
	wg                    *sync.WaitGroup   = &sync.WaitGroup{}
	args                  amqp091.Table     = amqp091.Table{}
	flushDeliveryChanTime time.Duration     = time.Duration(30) // throw to request timeout because server not response
	deliveryChan          chan []byte       = make(chan []byte, 1)
	res                   []byte            = []byte{}
)

func NewRabbitMQ(options *RabbitMQOptions) RabbitmqInterface {
	url = options.Url
	exchangeName = options.Exchange
	concurrency, _ = strconv.Atoi(options.Concurrency)
	publisherExpired = options.Expired

	connection, err := rabbitmq.NewConn(url,
		rabbitmq.WithConnectionOptionsLogging,
		rabbitmq.WithConnectionOptionsConfig(rabbitmq.Config{
			Heartbeat:       time.Duration(time.Second * 3),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}))

	if err != nil {
		defer log.Fatalf("NewRabbitMQ - rabbitmq.NewConn error: %s", err.Error())
		connection.Close()
	}

	log.Println("RabbitMQ connection success")
	con := rabbitmqStruct{connection: connection}

	defer con.closeConnection(nil, nil, connection)
	return &con
}

/**
* ===========================================
* HANDLER METHOD - CONNECTION
* ===========================================
**/
func (h *rabbitmqStruct) Connection() *rabbitmq.Conn {
	return h.connection
}

/**
* ===========================================
* HANDLER METHOD - PUBLISHER
* ===========================================
**/

func (h *rabbitmqStruct) Publisher(queue string, body interface{}) error {
	log.Printf("START PUBLISHER -> %s", queue)

	publishRequest.ContentType = "application/json"
	publishRequest.Timestamp = time.Now().Local()

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		defer fmt.Errorf("Publisher - rabbitmq.NewPublisher Error: %s", err.Error())
		defer h.recovery()

		publisher.Close()
		return err
	}

	bodyByte, err := sonic.Marshal(&body)
	if err != nil {
		defer fmt.Errorf("Publisher - sonic.Marshal Error: %s", err.Error())
		defer h.recovery()

		publisher.Close()
		return err
	}

	err = publisher.Publish(bodyByte, []string{queue},
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsExchange(exchangeName),
		rabbitmq.WithPublishOptionsContentType(publishRequest.ContentType),
		rabbitmq.WithPublishOptionsTimestamp(publishRequest.Timestamp),
		rabbitmq.WithPublishOptionsExpiration(publisherExpired),
	)

	if err != nil {
		defer fmt.Errorf("Publisher - publisher.Publish Error: %s", err.Error())
		defer h.recovery()

		publisher.Close()
		return err
	}

	defer h.recovery()
	publisher.Close()

	return nil
}

/**
* ===========================================
* HANDLER METHOD - CONSUMER
* ===========================================
**/

func (h *rabbitmqStruct) Consumer(queue string, callback func(d rabbitmq.Delivery) (action rabbitmq.Action)) {
	log.Printf("START CONSUMER -> %s", queue)

	consumerId := shortuuid.New()
	consumer, err := rabbitmq.NewConsumer(h.connection, callback, queue,
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
		rabbitmq.WithConsumerOptionsConsumerName(consumerId),
		rabbitmq.WithConsumerOptionsConsumerAutoAck(ack),
		rabbitmq.WithConsumerOptionsConcurrency(concurrency),
		rabbitmq.WithConsumerOptionsLogging,
	)

	if err != nil {
		defer fmt.Errorf("Consumer - rabbitmq.NewConsumer Error: %s", err.Error())
		defer h.recovery()

		consumer.Close()
		return
	}

	closeChan := make(chan os.Signal, 1)
	signal.Notify(closeChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGALRM, syscall.SIGINT)

	for {
		select {
		case sigs := <-closeChan:
			log.Printf("Signal Received %s", sigs.String())

			if os.Getenv("GO_ENV") != "development" {
				time.Sleep(10 * time.Second)
			}

			defer consumer.Close()
			os.Exit(15)

			break
		default:
			time.Sleep(3 * time.Second)

			if os.Getenv("GO_ENV") == "development" {
				log.Println(".")
			}

			defer consumer.Close()
			break
		}
	}
}

/**
* ===========================================
* HANDLER METHOD - PUBLISHER RPC
* ===========================================
**/

func (h *rabbitmqStruct) listeningConsumer(metadata *publishMetadata) *rabbitmq.Consumer {
	log.Printf("START CLIENT CONSUMER RPC -> %s", metadata.ReplyTo)

	h.rpcQueue = metadata.ReplyTo
	h.rpcConsumerId = metadata.CorrelationId

	consumer, err := rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		log.Println("=============== START DUMP listeningConsumerRpc REQUEST ================")
		fmt.Printf("\n")
		log.Printf("LISTENING RPC CONSUMER QUEUE: %s", h.rpcQueue)
		log.Printf("LISTENING RPC CONSUMER CORRELATION ID: %s", string(delivery.CorrelationId))
		log.Printf("LISTENING RPC CONSUMER REPLY TO: %s", string(delivery.ReplyTo))
		log.Printf("LISTENING RPC CONSUMER BODY: %s", string(delivery.Body))
		fmt.Printf("\n")
		log.Println("info", "=============== END DUMP listeningConsumerRpc REQUEST =================")
		fmt.Printf("\n")

		for _, d := range publishRequests {
			if d.CorrelationId != delivery.CorrelationId {
				deliveryChan <- delivery.Body
				return rabbitmq.NackDiscard
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
		defer fmt.Errorf("RabbitMQ - rabbitmq.NewConsumer Error: %s", err.Error())
		defer h.recovery()

		consumer.Close()
		return consumer
	}

	return consumer
}

func (h *rabbitmqStruct) PublisherRpc(queue string, body interface{}) ([]byte, error) {
	log.Printf("START CLIENT PUBLISHER RPC -> %s", queue)

	if len(publishRequests) > 0 {
		publishRequests = nil
	}

	publishRequest.CorrelationId = shortuuid.New()
	publishRequest.ReplyTo = fmt.Sprintf("rpc.%s", shortId)
	publishRequest.ContentType = "application/json"
	publishRequest.Timestamp = time.Now().Local()

	publishRequests = append(publishRequests, publishRequest)
	consumer := h.listeningConsumer(&publishRequest)

	publisher, err := rabbitmq.NewPublisher(h.connection,
		rabbitmq.WithPublisherOptionsExchangeName(exchangeName),
		rabbitmq.WithPublisherOptionsExchangeKind(Direct),
		rabbitmq.WithPublisherOptionsExchangeDeclare,
		rabbitmq.WithPublisherOptionsExchangeDurable,
		rabbitmq.WithPublisherOptionsLogging,
	)

	if err != nil {
		defer fmt.Errorf("PublisherRpc - rabbitmq.NewPublisher Error: %s", err.Error())
		defer h.recovery()

		publisher.Close()
		return nil, err
	}

	bodyByte, err := sonic.Marshal(&body)
	if err != nil {
		defer fmt.Errorf("PublisherRpc - sonic.Marshal Error: %s", err.Error())
		defer h.recovery()

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
		defer fmt.Errorf("PublisherRpc - publisher.Publish Error: %s", err.Error())
		defer h.recovery()

		publisher.Close()
		return nil, err
	}

	wg.Add(2)
	go func() {
		wg.Done()

		mutex.Lock()
		defer mutex.Unlock()

		res = <-deliveryChan
	}()

	go func() {
		wg.Done()

		time.AfterFunc(time.Second*flushDeliveryChanTime, func() {
			deliveryChan <- nil
			res = <-deliveryChan
		})
	}()
	wg.Wait()

	mutex.RLock()
	defer mutex.RUnlock()

	log.Println("=============== START DUMP publisherRpc OUTPUT ================")
	fmt.Printf("\n")
	log.Printf("PUBLISHER RPC QUEUE: %s", queue)
	log.Printf("PUBLISHER RPC CORRELATION ID: %s", publishRequest.CorrelationId)
	log.Printf("PUBLISHER RPC REQ BODY: %s", string(bodyByte))
	log.Printf("PUBLISHER RPC RES BODY: %s", string(res))
	fmt.Printf("\n")
	log.Println("=============== END DUMP publisherRpc OUTPUT =================")
	fmt.Printf("\n")

	if len(res) <= 0 {
		defer fmt.Errorf("PublisherRpc - publisher.Publish Empty Response")
		defer h.recovery()

		publisher.Close()
		return nil, errors.New("Request Timeout")
	}

	defer h.recovery()
	h.closeConnection(publisher, consumer, nil)

	return res, nil
}

func (h *rabbitmqStruct) ConsumerRpc(queue string, callback func(delivery rabbitmq.Delivery) (action rabbitmq.Action)) {
	log.Printf("START SERVER CONSUMER RPC -> %s", queue)

	h.rpcConsumerId = shortuuid.New()
	h.rpcReplyTo = queue
	ack = false

	consumer, err := rabbitmq.NewConsumer(h.connection, func(delivery rabbitmq.Delivery) (action rabbitmq.Action) {
		log.Println("=============== START DUMP ConsumerRpc REQUEST ================")
		fmt.Printf("\n")
		log.Printf("SERVER CONSUMER RPC QUEUE: %s", queue)
		log.Printf("SERVER CONSUMER RPC CORRELATION ID: %s", delivery.CorrelationId)
		log.Printf("SERVER CONSUMER RPC REPLY TO: %s", delivery.ReplyTo)
		log.Printf("SERVER CONSUMER RPC BODY: %s", string(delivery.Body))
		fmt.Printf("\n")
		log.Println("=============== END DUMP ConsumerRpc REQUEST =================")
		fmt.Printf("\n")

		return callback(delivery)
	},
		queue,
		rabbitmq.WithConsumerOptionsExchangeName(exchangeName),
		rabbitmq.WithConsumerOptionsExchangeKind(Direct),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
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
		defer fmt.Errorf("ConsumerRpc - rabbitmq.NewConsumer Error: %s", err.Error())
		defer h.recovery()

		consumer.Close()
		return
	}
}

func (h *rabbitmqStruct) ReplyToDeliveryPublisher(deliveryBodyTo []byte, delivery rabbitmq.Delivery) {
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
		defer fmt.Errorf("ReplyToDeliveryPublisher - rabbitmq.NewPublisher Error: %s", err.Error())
		defer h.recovery()

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
		defer fmt.Errorf("ReplyToDeliveryPublisher - publisher.Publish Error: %s", err.Error())
		defer h.recovery()

		publisher.Close()
		return
	}

	defer h.recovery()
	publisher.Close()
}

func (h *rabbitmqStruct) closeConnection(publisher *rabbitmq.Publisher, consumer *rabbitmq.Consumer, connection *rabbitmq.Conn) {
	if publisher != nil && consumer != nil && connection != nil {
		defer publisher.Close()
		defer consumer.Close()
		defer connection.Close()
	} else if publisher != nil && consumer != nil && connection == nil {
		defer publisher.Close()
		defer consumer.Close()
	} else if publisher != nil && consumer == nil && connection != nil {
		defer publisher.Close()
		defer connection.Close()
	} else if publisher == nil && consumer != nil && connection != nil {
		defer consumer.Close()
		defer connection.Close()
	} else {
		closeChan := make(chan os.Signal, 1)
		signal.Notify(closeChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGALRM, syscall.SIGINT)

		for {
			select {
			case <-closeChan:
				defer connection.Close()
				os.Exit(15)

				break
			default:
				return
			}
		}
	}
}

func (h *rabbitmqStruct) recovery() {
	if err := recover(); err != nil {
		return
	}
}
