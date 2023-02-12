# RABBITMQ RPC (Request & Reply Pattern)

Check this tutorial about rpc queue using **rabbitmq** [here](https://www.rabbitmq.com/tutorials/tutorial-six-python.html) and check this tutorial about messaging pattern request & reply [here](https://www.enterpriseintegrationpatterns.com/RequestReply.html), if you need tutorial about rabbitmq check my repo [here](https://github.com/restuwahyu13/node-rabbitmq).

## Server RPC

```go
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue string = "account"
	)

	rabbit := pkg.NewRabbitMQ()
	rabbit.ConsumerRpc(queue, nil)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGALRM)

	for {
		select {
		case sigs := <-signalChan:
			log.Printf("Received Signal %s", sigs.String())
			os.Exit(15)
			break
		default:
			time.Sleep(3 * time.Second)
			fmt.Println("................................")
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