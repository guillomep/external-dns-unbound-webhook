package main

import (
	"github.com/codingconcepts/env"
	"github.com/guillomep/external-dns-unbound-webhook/internal/server"
	"github.com/guillomep/external-dns-unbound-webhook/internal/unbound"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"sigs.k8s.io/external-dns/provider/webhook/api"
	"syscall"
)

// loop waits for a SIGTERM or a SIGINT and then shuts down the server.
func loop(status *server.HealthStatus) {
	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, syscall.SIGINT, syscall.SIGTERM)
	signal := <-exitSignal

	log.Infof("Signal %s received. Shutting down the webhook.", signal.String())
	status.SetHealth(false)
	status.SetReady(false)
}

func main() {
	// Read server options
	serverOptions := &server.ServerOptions{}
	if err := env.Set(serverOptions); err != nil {
		log.Fatal(err)
	}

	// Start health server
	log.Infof("Starting liveness and readiness server on %s", serverOptions.GetHealthAddress())
	healthStatus := server.HealthStatus{}
	healthServer := server.HealthServer{}
	go healthServer.Start(&healthStatus, nil, *serverOptions)

	// Read provider configuration
	providerConfig := &unbound.Configuration{}
	if err := env.Set(providerConfig); err != nil {
		log.Fatal(err)
	}

	// instantiate the Unbound provider
	provider, err := unbound.NewProvider(providerConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Start the webhook
	log.Infof("Starting webhook server on %s", serverOptions.GetWebhookAddress())
	startedChan := make(chan struct{})
	go api.StartHTTPApi(
		provider, startedChan,
		serverOptions.GetReadTimeout(),
		serverOptions.GetWriteTimeout(),
		serverOptions.GetWebhookAddress(),
	)

	// Wait for the HTTP server to start and then set the healthy and ready flags
	<-startedChan
	healthStatus.SetHealth(true)
	healthStatus.SetReady(true)

	// Loops until a signal tells us to exit
	loop(&healthStatus)
}
