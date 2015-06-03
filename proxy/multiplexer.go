package proxy

import (
	"code.google.com/p/go-uuid/uuid"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type Multiplexer struct {
	backendId         string
	messagesToBackend chan string
	frontendChans     map[string]chan<- common.Message
}

func (m *Multiplexer) initializeClient() (string, <-chan common.Message) {
	msgKey := uuid.New()
	frontendChan := make(chan common.Message)
	m.frontendChans[msgKey] = frontendChan
	return msgKey, frontendChan
}

func (m *Multiplexer) connect(msgKey, url string) {
	message := common.FormatMessage(msgKey, common.Connect, url)
	m.messagesToBackend <- message
}

func (m *Multiplexer) send(msgKey, msg string) {
	message := common.FormatMessage(msgKey, common.Body, msg)
	m.messagesToBackend <- message
}

func (m *Multiplexer) sendClose(msgKey string) {
	message := common.FormatMessage(msgKey, common.Close, "")
	m.messagesToBackend <- message
}

func (m *Multiplexer) closeConnection(msgKey string) {
	m.sendClose(msgKey)
	if frontendChan, ok := m.frontendChans[msgKey]; ok {
		close(frontendChan)
		delete(m.frontendChans, msgKey)
	}
}

func (m *Multiplexer) routeMessages(ws *websocket.Conn) {
	// Read messages from backend
	go func() {
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Error("Error reading message.")
				continue
			}

			message := common.ParseMessage(string(msg))
			if frontendChan, ok := m.frontendChans[message.Key]; ok {
				frontendChan <- message
			} else {
				if message.Type != common.Close {
					log.WithFields(log.Fields{
						"Message": message,
					}).Warn("Could not find channel for message. Dropping message and sending close to backend.")
					m.sendClose(message.Key)
				}
			}
		}
	}()

	// Write messages to backend
	go func() {
		for {
			message := <-m.messagesToBackend
			err := ws.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Error("Error writing message.")
			}
		}
	}()
}
