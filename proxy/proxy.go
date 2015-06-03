package proxy

import (
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

func StartProxy(listen string) error {
	backendMultiplexers := make(map[string]*Multiplexer)

	frontend := &FrontendHandler{
		backendMultiplexers: backendMultiplexers,
	}

	backendRegisterChan := make(chan *Multiplexer, 10)
	go func() {
		for {
			multiplexer := <-backendRegisterChan
			backendMultiplexers[multiplexer.backendId] = multiplexer
		}
	}()

	backendHandler := &BackendHandler{
		registerBackEnd: backendRegisterChan,
	}

	router := mux.NewRouter()
	http.Handle("/", router)
	router.Handle("/connectbackend", backendHandler).Methods("GET")
	router.Handle("/{proxy:.*}", frontend).Methods("GET")

	err := http.ListenAndServe(listen, nil)

	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Info("Exiting proxy.")
	}

	return err
}
