package proxy

import (
	"sync"
	"time"

	"code.google.com/p/go-uuid/uuid"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type multiplexer struct {
	backendKey        string
	messagesToBackend chan string
	frontendChans     map[string]chan<- common.Message
	proxyManager      proxyManager
	frontendMu        *sync.RWMutex
}

func (m *multiplexer) initializeClient() (string, <-chan common.Message) {
	msgKey := uuid.New()
	frontendChan := make(chan common.Message)
	m.frontendMu.Lock()
	defer m.frontendMu.Unlock()
	m.frontendChans[msgKey] = frontendChan
	return msgKey, frontendChan
}

func (m *multiplexer) connect(msgKey, url string) {
	m.messagesToBackend <- common.FormatMessage(msgKey, common.Connect, url)
}

func (m *multiplexer) send(msgKey, msg string) {
	m.messagesToBackend <- common.FormatMessage(msgKey, common.Body, msg)
}

func (m *multiplexer) sendClose(msgKey string) {
	m.messagesToBackend <- common.FormatMessage(msgKey, common.Close, "")
}

func (m *multiplexer) closeConnection(msgKey string, notifyBackend bool) {
	if notifyBackend {
		m.sendClose(msgKey)
	}

	if frontendChan, ok := m.frontendChans[msgKey]; ok {
		m.frontendMu.Lock()
		defer m.frontendMu.Unlock()
		if _, stillThere := m.frontendChans[msgKey]; stillThere {
			close(frontendChan)
			delete(m.frontendChans, msgKey)
		}
	}
}

func (m *multiplexer) routeMessages(ws *websocket.Conn) {
	stopSignal := make(chan bool, 1)

	// Read messages from backend
	go func(stop chan<- bool) {
		for {
			msgType, msg, err := ws.ReadMessage()
			if err != nil {
				m.shutdown(stop)
				return
			}

			if msgType != websocket.TextMessage {
				continue
			}
			message := common.ParseMessage(string(msg))

			m.frontendMu.RLock()
			frontendChan, ok := m.frontendChans[message.Key]
			if ok {
				frontendChan <- message
			}
			m.frontendMu.RUnlock()

			if !ok && message.Type != common.Close {
				m.sendClose(message.Key)
			}
		}
	}(stopSignal)

	// Write messages to backend
	go func(stop <-chan bool) {
		ticker := time.NewTicker(time.Second * 5)
		defer ticker.Stop()
		for {
			select {
			case message, ok := <-m.messagesToBackend:
				if !ok {
					return
				}
				err := ws.WriteMessage(websocket.TextMessage, []byte(message))
				if err != nil {
					log.WithFields(log.Fields{"error": err, "msg": message}).Error("Could not write message.")
				}

			case <-ticker.C:
				ws.WriteControl(websocket.PingMessage, []byte(""), time.Now().Add(time.Second))

			case <-stop:
				return
			}
		}
	}(stopSignal)
}

func (m *multiplexer) shutdown(stop chan<- bool) {
	m.proxyManager.removeBackend(m.backendKey)
	stop <- true
	for key := range m.frontendChans {
		m.closeConnection(key, false)
	}
}
