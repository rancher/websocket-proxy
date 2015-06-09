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

	p := &proxy.ProxyStarter{
		BackendPaths:       []string{"/v1/connectbackend"},
		FrontendPaths:      []string{"/v1/{logs:logs}/", "/v1/{stats:stats}", "/v1/{stats:stats}/{statsid}"},
		CattleWSProxyPaths: []string{"/v1/{sub:subscribe}"},
		CattleProxyPaths:   []string{"/{cattle-proxy:.*}"},
		Config:             conf,
	}

	err = p.StartProxy()

	log.WithFields(log.Fields{"error": err}).Info("Exiting proxy.")
}
