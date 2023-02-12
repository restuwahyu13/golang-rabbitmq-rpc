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
