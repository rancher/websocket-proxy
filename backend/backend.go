package backend

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
)

var responders = make(map[string]chan string)

type Handler interface {
	Handle(string, <-chan string, chan<- *MessageWrapper)
}

type MessageWrapper struct {
	Key     string
	Message string
}

func ConnectToProxy(proxyUrl string, handlers map[string]Handler) {
	log.WithFields(log.Fields{
		"url": proxyUrl,
	}).Info("Connecting to proxy.")

	dialer := &websocket.Dialer{}
	headers := http.Header{}
	headers.Add("X-Cattle-HostId", "1")
	ws, _, err := dialer.Dial(proxyUrl, headers)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to connect to proxy.")
	}

	responseChannel := make(chan *MessageWrapper, 10)

	go func() {
		for {
			wrap := <-responseChannel
			data := fmt.Sprintf("%s||%s", wrap.Key, wrap.Message)

			ws.WriteMessage(1, []byte(data))
		}
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Warn("Error reading message.")
			continue
		}

		parts := strings.SplitN(string(msg), "||", 3)
		if len(parts) != 3 {
			continue
		}

		responderKey := parts[0]
		msgType, err := strconv.Atoi(parts[1])
		if err != nil {
			log.WithFields(log.Fields{
				"error":       err,
				"messageType": parts[1],
			}).Warn("Error parsing message type.")
			continue
		}

		if msgType == 0 {
			requestUrl, err := url.Parse(parts[2])
			if err != nil {
				continue
			}
			handler, ok := handlers[requestUrl.Path]
			if !ok {
				// TODO Tell proxy we quit
				log.WithFields(log.Fields{
					"path": requestUrl.Path,
				}).Warn("Could not find appropriate message handler for supplied path.")
				continue
			}
			msgChan := make(chan string, 10)
			responders[responderKey] = msgChan
			go handler.Handle(responderKey, msgChan, responseChannel)

		} else if msgType == 2 {
			if msgChan, ok := responders[responderKey]; ok {
				close(msgChan)
			}
		} else {
			if msgChan, ok := responders[responderKey]; ok {
				msgString := parts[2]
				msgChan <- msgString
			} else {
				// TODO Coudln't find reply channel. Tell proxy we quite.
			}
		}
	}
}
