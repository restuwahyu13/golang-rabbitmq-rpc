# RABBITMQ RPC (Request & Reply Pattern)

Check this tutorial about rpc queue using **rabbitmq** [here](https://www.rabbitmq.com/tutorials/tutorial-six-python.html) and check this tutorial about messaging pattern request & reply [here](https://www.enterpriseintegrationpatterns.com/RequestReply.html), if you need tutorial about rabbitmq check my repo [here](https://github.com/restuwahyu13/node-rabbitmq).

## Server RPC

```go
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
```

## Client RPC

```go
package main

import (
	"fmt"
	"log"

	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue string = "account"
		data         = map[string]interface{}{}
		fk           = faker.New()
	)

	data["id"] = shortuuid.New()
	data["name"] = fk.App().Name()
	data["country"] = fk.Address().Country()
	data["city"] = fk.Address().City()
	data["postcode"] = fk.Address().PostCode()

	rabbit := pkg.NewRabbitMQ()
	delivery, err := rabbit.PublishRpc(queue, data)

	if err != nil {
		log.Fatal(err.Error())
	}

	for d := range delivery {
		fmt.Println("CONSUMER DEBUG RESPONSE: ", string(d.Body))
	}
}
```

## Noted Important!

if your queue name  is not deleted like this image below, after consumers consuming data from queue, because there is problem with your consumers.

![](https://i.imgur.com/NpczUuG.png)
