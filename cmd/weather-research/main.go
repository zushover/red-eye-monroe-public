package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
	_ "time/tzdata"
	"weatherbot/internal/domain"
	"weatherbot/internal/research"
)

func main() {
	var stations []domain.Station
	b, e := os.ReadFile("configs/research-stations.json")
	if e != nil {
		log.Fatal(e)
	}
	if e = json.Unmarshal(b, &stations); e != nil {
		log.Fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	s := research.New(stations, "data/weather-research")
	go s.Run(ctx)
	log.Println("Weather research: http://localhost:8789")
	server := &http.Server{Addr: "localhost:8789", Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
