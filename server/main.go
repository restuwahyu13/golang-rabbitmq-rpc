package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"

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
		queue string = "account"
		data         = Person{}
		fk           = faker.New()
	)

	data.ID = shortuuid.New()
	data.Name = fk.App().Name()
	data.Country = fk.Address().Country()
	data.City = fk.Address().City()
	data.PostCode = fk.Address().PostCode()

	replyTo := pkg.ConsumerOverwriteResponse{}
	replyTo.Res = data

	rabbit := pkg.NewRabbitMQ()
	rabbit.ConsumerRpc(queue, &replyTo)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGALRM)

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
