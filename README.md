# RABBITMQ RPC (Request & Reply Pattern)

Check this tutorial about rpc queue using **rabbitmq** [here](https://www.rabbitmq.com/tutorials/tutorial-six-python.html) and check this tutorial about messaging pattern request & reply [here](https://www.enterpriseintegrationpatterns.com/RequestReply.html), if you need tutorial about rabbitmq check my repo [here](https://github.com/restuwahyu13/node-rabbitmq), or if you need other example rpc pattern using node [here](https://github.com/restuwahyu13/node-rabbitmq-rpc), if you need old example code check branch `old-master` for old version and check `new-master` for style like example like `old-master`.

## Server RPC

```go
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
```

## Client RPC

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue string                 = "account"
		fk    faker.Faker            = faker.New()
		req   map[string]interface{} = make(map[string]interface{})
		user  map[string]interface{} = make(map[string]interface{})
	)

	rabbit := pkg.NewRabbitMQ(&pkg.RabbitMQOptions{
		Url:         "amqp://restuwahyu13:restuwahyu13@localhost:5672/",
		Exchange:    "amqp.direct",
		Concurrency: "5",
	})

	router := http.NewServeMux()

	router.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		req["id"] = shortuuid.New()
		req["name"] = fk.App().Name()
		req["country"] = fk.Address().Country()
		req["city"] = fk.Address().City()
		req["postcode"] = fk.Address().PostCode()

		delivery, err := rabbit.PublisherRpc(queue, req)
		if err != nil {
			statCode := http.StatusUnprocessableEntity

			if strings.Contains(err.Error(), "Timeout") {
				statCode = http.StatusRequestTimeout
			}

			w.WriteHeader(statCode)
			w.Write([]byte(err.Error()))
			return
		}

		if err := sonic.Unmarshal(delivery, &user); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(err.Error()))
			return
		}

		json.NewEncoder(w).Encode(&user)
	})

	err := pkg.Graceful(func() *pkg.GracefulConfig {
		return &pkg.GracefulConfig{Handler: router, Port: "4000"}
	})

	if err != nil {
		log.Fatalf("HTTP Server Shutdown: %s", err.Error())
		os.Exit(1)
	}
}
```
