package main

import (
	log "github.com/Sirupsen/logrus"

	"github.com/rancherio/websocket-proxy/proxy"
)

func main() {
	log.Info("Starting proxy...")

	err := proxy.StartProxy("127.0.0.1:9345")

	log.WithFields(log.Fields{"error": err}).Info("Exiting proxy.")
}
