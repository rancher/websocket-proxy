package proxy

import (
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

func StartProxy(listen string) {
	router := mux.NewRouter()
	http.Handle("/", router)

	backends := make(map[string]*Multiplexer)

	frontend := &FrontendHandler{
		backends: backends,
	}

	backendRegisterChan := make(chan *Multiplexer, 10)
	go func() {
		for {
			multiplexer := <-backendRegisterChan
			backends[multiplexer.backendId] = multiplexer
		}
	}()

	backendHandler := &BackendHandler{
		registerBackEnd: backendRegisterChan,
	}

	router.Handle("/connectbackend", backendHandler).Methods("GET")
	router.Handle("/{proxy:.*}", frontend).Methods("GET")

	err := http.ListenAndServe(listen, nil)

	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to start proxy.")
	}
}
