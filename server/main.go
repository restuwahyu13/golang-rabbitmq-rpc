package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bytedance/sonic"
	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"
	"github.com/sirupsen/logrus"
	"github.com/wagslane/go-rabbitmq"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

type Person struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	City     string `json:"city"`
	PostCode string `json:"postcode"`
}

func main() {
	var (
		queue string      = "account"
		data  Person      = Person{}
		fk    faker.Faker = faker.New()
	)

	rabbit := pkg.NewRabbitMQ(&pkg.RabbitMQOptions{
		Url:         "amqp://restuwahyu13:restuwahyu13@localhost:5672/",
		Exchange:    "amqp.direct",
		Concurrency: "5",
	})

	rabbit.ConsumerRpc(queue, func(d rabbitmq.Delivery) (action rabbitmq.Action) {
		data.ID = shortuuid.New()
		data.Name = fk.App().Name()
		data.Country = fk.Address().Country()
		data.City = fk.Address().City()
		data.PostCode = fk.Address().PostCode()

		dataByte, err := sonic.Marshal(&data)
		if err != nil {
			logrus.Fatal(err.Error())
			return
		}

		defer rabbit.ReplyToDeliveryPublisher(dataByte, d)
		return rabbitmq.Ack
	})

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGALRM, syscall.SIGINT)

	for {
		select {
		case sigs := <-signalChan:
			log.Printf("Received Signal %s", sigs.String())
			os.Exit(15)

			break
		default:
			time.Sleep(time.Duration(time.Second * 3))
			log.Println("...........................")

			break
		}
	}
}
