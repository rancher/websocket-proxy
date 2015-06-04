package backend

import (
	"net/http"
	"net/url"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

// Implement this iterface and pass implementations into ConnectToProxy() to have messages
// routed to and from the handler.
type Handler interface {
	Handle(string, <-chan string, chan<- common.Message)
}

func ConnectToProxy(proxyUrl, hostId string, handlers map[string]Handler) {
	// TODO Limit number of "worker" responders

	log.WithFields(log.Fields{"url": proxyUrl}).Info("Connecting to proxy.")

	dialer := &websocket.Dialer{}
	headers := http.Header{}
	headers.Add("X-Cattle-HostId", hostId)
	ws, _, err := dialer.Dial(proxyUrl, headers)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to connect to proxy.")
	}

	connectToProxyWS(ws, handlers)
}

func connectToProxyWS(ws *websocket.Conn, handlers map[string]Handler) {
	responders := make(map[string]chan string)
	responseChannel := make(chan common.Message, 10)

	// Write messages to proxy
	go func() {
		for {
			message, ok := <-responseChannel
			if !ok {
				return
			}
			data := common.FormatMessage(message.Key, message.Type, message.Body)
			ws.WriteMessage(1, []byte(data))
		}
	}()

	// Read and route messages from proxy
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.WithFields(log.Fields{"error": err}).Error("Received error reading from socket. Exiting.")
			close(responseChannel)
			return
		}

		message := common.ParseMessage(string(msg))
		switch message.Type {
		case common.Connect:
			requestUrl, err := url.Parse(message.Body)
			if err != nil {
				continue
			}
			handler, ok := handlers[requestUrl.Path]
			if ok {
				msgChan := make(chan string, 10)
				responders[message.Key] = msgChan
				go handler.Handle(message.Key, msgChan, responseChannel)
			} else {
				log.WithFields(log.Fields{"path": requestUrl.Path}).Warn("Could not find appropriate message handler for supplied path.")
				responseChannel <- common.Message{
					Key:  message.Key,
					Type: common.Close,
					Body: ""}
			}
		case common.Body:
			if msgChan, ok := responders[message.Key]; ok {
				msgChan <- message.Body
			} else {
				log.WithFields(log.Fields{"key": message.Key}).Warn("Could not find responder for specified key.")
				responseChannel <- common.Message{
					Key:  message.Key,
					Type: common.Close,
				}
			}
		case common.Close:
			closeHandler(responders, message.Key)
		default:
			log.WithFields(log.Fields{"messageType": message.Type}).Warn("Unrecognized message type. Closing connection.")
			closeHandler(responders, message.Key)
			SignalHandlerClosed(message.Key, responseChannel)
			continue
		}
	}
}

func closeHandler(responders map[string]chan string, msgKey string) {
	if msgChan, ok := responders[msgKey]; ok {
		close(msgChan)
		delete(responders, msgKey)
	}
}

func SignalHandlerClosed(msgKey string, response chan<- common.Message) {
	wrap := common.Message{
		Key:  msgKey,
		Type: common.Close,
	}
	response <- wrap

}
