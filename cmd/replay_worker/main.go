package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	fieldservice "github.com/example/field-service-event-replay-go"
)

func main() {
	listen := flag.String("listen", ":8080", "http listen address")
	webhookID := flag.String("webhook-id", "", "Infrai webhook id to replay")
	queue := flag.String("queue", "", "dead-letter queue name to redrive")
	rate := flag.String("rate", "1/s", "operator note for redrive pacing")
	secret := flag.String("webhook-secret", envOr("FIELDSERVICE_WEBHOOK_SECRET", "dev-secret"), "shared secret for incoming webhook signatures")
	flag.Parse()

	if *webhookID == "" || *queue == "" {
		log.Fatal("-webhook-id and -queue are required")
	}

	client, err := fieldservice.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	service := fieldservice.NewReplayService(client, *secret)
	handler := fieldservice.NewHTTPHandler(service, *webhookID, *queue)

	log.Printf("listening on %s, queue redrive pacing %s", *listen, *rate)
	log.Fatal(http.ListenAndServe(*listen, handler.Routes()))
}

func envOr(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
