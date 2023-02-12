package main

import (
	"fmt"
	"log"

	"github.com/lithammer/shortuuid"
	"github.com/wagslane/go-rabbitmq"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue        string = "account"
		deliveryChan        = make(chan rabbitmq.Delivery, 1)
		data                = map[string]interface{}{"id": shortuuid.New(), "name": "jane doe"}
	)

	rabbit := pkg.NewRabbitMQ()
	err := rabbit.PublishRpc(queue, data, deliveryChan)

	if err != nil {
		log.Fatal(err.Error())
	}

	go func() {
		for d := range deliveryChan {
			fmt.Println("CONSUMER DEBUG RESPONSE: ", string(d.Body))
		}
	}()
}
