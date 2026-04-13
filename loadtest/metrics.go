package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultListenAddress = "0.0.0.0"
	defaultMetricsPort   = 9696
)

type MetricsServer struct {
	server *http.Server
}

func (s *MetricsServer) StartMetricsClient(config Config) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthzHandler)
	mux.Handle("/metrics", promhttp.HandlerFor(loadtestPrometheusGatherer(), promhttp.HandlerOpts{}))

	metricsPort := config.MetricsPort
	if config.MetricsPort == 0 {
		metricsPort = defaultMetricsPort
	}

	listenAddr := fmt.Sprintf("%s:%d", defaultListenAddress, metricsPort)
	log.Printf("Listening for metrics scrapes on %s", listenAddr)

	s.server = &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	err := s.server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func (s *MetricsServer) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	_, err := io.WriteString(w, "ok\n")
	if err != nil {
		panic(err)
	}
}
