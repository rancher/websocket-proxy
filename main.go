package main

import (
	log "github.com/Sirupsen/logrus"

	"github.com/rancherio/websocket-proxy/proxy"
)

func main() {
	log.Info("Starting proxy...")

	conf, err := proxy.GetConfig()
	if err != nil {
		log.WithField("error", err).Fatal("Error getting config.")
	}

	err = proxy.StartProxy(conf.ListenAddr, conf)

	log.WithFields(log.Fields{"error": err}).Info("Exiting proxy.")
}
