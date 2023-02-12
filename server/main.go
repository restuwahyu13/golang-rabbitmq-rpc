package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lithammer/shortuuid"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue string = "account"
		data         = map[string]interface{}{"id": shortuuid.New(), "name": "max cavalera"}
	)

	bodyByte, err := json.Marshal(data)
	if err != nil {
		log.Fatalf(err.Error())
	}

	rabbit := pkg.NewRabbitMQ()
	rabbit.ConsumerRpc(queue, bodyByte)

	closeChan := make(chan os.Signal, 1)
	signal.Notify(closeChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGALRM)

	for {
		select {
		case sigs := <-closeChan:
			log.Printf("Received Signal %s", sigs.String())
			os.Exit(15)
			break
		default:
			time.Sleep(3 * time.Second)
			fmt.Print(".")
			break
		}
	}
}
