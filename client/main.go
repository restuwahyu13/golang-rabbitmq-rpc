package main

import (
	"fmt"
	"log"

	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"
	"github.com/wagslane/go-rabbitmq"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue        string = "account"
		deliveryChan        = make(chan rabbitmq.Delivery, 1)
		data                = map[string]interface{}{}
		fk                  = faker.New()
	)

	data["id"] = shortuuid.New()
	data["name"] = fk.App().Name()
	data["country"] = fk.Address().Country()
	data["city"] = fk.Address().City()
	data["postcode"] = fk.Address().PostCode()

	rabbit := pkg.NewRabbitMQ()
	err := rabbit.PublishRpc(queue, data, deliveryChan)

	if err != nil {
		log.Fatal(err.Error())
	}

	for d := range deliveryChan {
		fmt.Println("CONSUMER DEBUG RESPONSE: ", string(d.Body))
	}
}
