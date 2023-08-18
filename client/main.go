package main

import (
	"net/http"

	"github.com/bytedance/sonic"
	"github.com/jaswdr/faker"
	"github.com/lithammer/shortuuid"

	"github.com/restuwahyu13/go-rabbitmq-rpc/helpers"
	"github.com/restuwahyu13/go-rabbitmq-rpc/pkg"
)

func main() {
	var (
		queue string                 = "account"
		port  string                 = ":3000"
		fk    faker.Faker            = faker.New()
		req   map[string]interface{} = make(map[string]interface{})
		user  map[string]interface{} = make(map[string]interface{})
		res   helpers.APIResponse    = helpers.APIResponse{}
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
			res.StatCode = http.StatusUnprocessableEntity
			res.ErrMsg = err.Error()

			w.WriteHeader(res.StatCode)
			w.Write(helpers.ApiResponse(&res))
			return
		}

		if err := sonic.Unmarshal(delivery, &user); err != nil {
			res.StatCode = http.StatusUnprocessableEntity
			res.ErrMsg = err.Error()

			w.WriteHeader(res.StatCode)
			w.Write(helpers.ApiResponse(&res))
			return
		}

		res.StatCode = http.StatusOK
		res.StatMsg = "Success"
		res.Data = user

		w.Write(helpers.ApiResponse(&res))
	})

	http.ListenAndServe(port, nil)
}
