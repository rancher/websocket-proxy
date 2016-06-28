package backend

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"

	"github.com/rancher/websocket-proxy/common"
)

// Implement this iterface and pass implementations into ConnectToProxy() to have messages
// routed to and from the handler.
type Handler interface {
	Handle(messageKey string, initialMessage string, incomingMessages <-chan string, response chan<- common.Message)
}

func ConnectToProxy(proxyUrl string, handlers map[string]Handler) {
	log.WithFields(log.Fields{"url": proxyUrl}).Info("Connecting to proxy.")

	dialer := &websocket.Dialer{}
	headers := http.Header{}
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
			select {
			case message, ok := <-responseChannel:
				if !ok {
					return
				}
				data := common.FormatMessage(message.Key, message.Type, message.Body)
				ws.WriteMessage(1, []byte(data))
			case <-time.After(time.Second * 5):
				ws.WriteControl(websocket.PingMessage, []byte(""), time.Now().Add(time.Second))
			}
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

			handler, ok := getHandler(requestUrl.Path, handlers)
			if ok {
				msgChan := make(chan string, 10)
				responders[message.Key] = msgChan
				go handler.Handle(message.Key, message.Body, msgChan, responseChannel)
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

// Returns the handler that best matches the provided path and true if one is found,
// otherwise returns nil and false. This function is not robust enough to handle
// pattern matching. If it can't find an exact match, it will look for a handler that
// is a prefix for path. So, '/v1/stats/' will be a match for '/v1/stats/id-123'
func getHandler(path string, handlers map[string]Handler) (Handler, bool) {
	if handler, ok := handlers[path]; ok {
		return handler, true
	}

	path = strings.TrimSuffix(path, "/")
	for key, handler := range handlers {
		key = strings.TrimSuffix(key, "/")
		if strings.HasPrefix(path, key) {
			return handler, true
		}
	}
	return nil, false
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
