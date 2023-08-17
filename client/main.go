package main

import (
	"encoding/json"
	"net/http"

	"github.com/bytedance/sonic"
	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"
	"golang.org/x/sync/errgroup"

	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue string                 = "account"
		port  string                 = ":3000"
		fk    faker.Faker            = faker.New()
		req   map[string]interface{} = make(map[string]interface{})
		res   map[string]interface{} = make(map[string]interface{})
		erg   *errgroup.Group        = &errgroup.Group{}
	)

	rabbit := pkg.NewRabbitMQ(&pkg.RabbitMQOptions{
		Url:         "amqp://restuwahyu13:restuwahyu13@localhost:5672/",
		Exchange:    "amqp.direct",
		Concurrency: "5",
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		req["id"] = shortuuid.New()
		req["name"] = fk.App().Name()
		req["country"] = fk.Address().Country()
		req["city"] = fk.Address().City()
		req["postcode"] = fk.Address().PostCode()

		delivery, err := rabbit.PublisherRpc(queue, req)
		if err != nil {
			res["statusCode"] = http.StatusUnprocessableEntity
			res["errorMessage"] = err.Error()

			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(&res)
			return
		}

		erg.Go(func() error {
			if err := sonic.Unmarshal(<-delivery, &res); err != nil {
				return err
			}

			return nil
		})

		if err := erg.Wait(); err != nil {
			res["statusCode"] = http.StatusUnprocessableEntity
			res["errorMessage"] = err.Error()

			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(&res)
			return
		}

		json.NewEncoder(w).Encode(&res)
	})

	http.ListenAndServe(port, nil)
}
