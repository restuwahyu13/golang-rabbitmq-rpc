package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"
	"github.com/wagslane/go-rabbitmq"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue    string                 = "account"
		data                            = map[string]interface{}{}
		fk                              = faker.New()
		ctx      context.Context        = context.Background()
		delivery chan rabbitmq.Delivery = make(chan rabbitmq.Delivery, 1)
	)

	data["id"] = shortuuid.New()
	data["name"] = fk.App().Name()
	data["country"] = fk.Address().Country()
	data["city"] = fk.Address().City()
	data["postcode"] = fk.Address().PostCode()

	rabbit := pkg.NewRabbitMQ()
	_, err := rabbit.PublishRpc(ctx, delivery, queue, data)

	if err != nil {
		log.Fatal(err.Error())
	}

	for d := range delivery {
		close(delivery)
		fmt.Println("CONSUMER DEBUG RESPONSE: ", string(d.Body))
	}
}
