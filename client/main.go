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

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
