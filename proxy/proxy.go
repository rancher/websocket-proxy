package proxy

import (
	"fmt"
	"net/http"
	"strings"

	"code.google.com/p/go-uuid/uuid"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func StartProxy() {
	router := mux.NewRouter()
	http.Handle("/", router)

	backends := make(map[string]*Multiplexer)

	frontend := &FrontendHandler{
		backends: backends,
	}

	backendRegisterChan := make(chan *Multiplexer, 10)

	go func() {
		for {
			mx := <-backendRegisterChan
			backends[mx.backendId] = mx
		}
	}()

	backendHandler := &BackendHandler{
		registerBackEnd: backendRegisterChan,
	}

	router.Handle("/connectbackend", backendHandler).Methods("GET")
	router.Handle("/{proxy:.*}", frontend).Methods("GET")

	var listen = "127.0.0.1:1111"
	err := http.ListenAndServe(listen, nil)

	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to start proxy.")
	}
}

type FrontendHandler struct {
	backends map[string]*Multiplexer
}

func (h *FrontendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	hostId := h.getHostId(req)
	backend, ok := h.backends[hostId]
	if !ok {
		http.Error(rw, "Bad hostId", 400)
		return
	}

	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		return
	}

	uid, respChannel := backend.initializeConnection()
	go func() {
		for {
			msg := <-respChannel
			ws.WriteMessage(1, []byte(msg))
		}
	}()

	url := req.URL.String()
	backend.sendConnect(uid, url)
	for {
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			// TODO Send close to backend and other cleanup
			backend.sendClose(uid)
			ws.Close()
			return
		}
		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			backend.send(uid, string(msg))
		}
	}
}

func (h *FrontendHandler) getHostId(req *http.Request) string {
	return req.FormValue("hostId")
}

type BackendHandler struct {
	registerBackEnd chan *Multiplexer
}

func (h *BackendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	hostId := req.Header.Get("X-Cattle-HostId")
	if hostId == "" {
		http.Error(rw, "Missing X-Cattle-HostId Header", 400)
	}

	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	upgrader.CheckOrigin = func(req *http.Request) bool {
		return true
	}

	wsConn, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		// TODO Make this more betterer
		http.Error(rw, "Failed to upgrade", 500)
		return
	}

	mx := newMultiplexer(hostId, wsConn)
	h.registerBackEnd <- mx
}

func newMultiplexer(backendId string, wsConn *websocket.Conn) *Multiplexer {
	msgs := make(chan string, 10)
	clients := make(map[string]chan<- string)
	m := &Multiplexer{
		backendId:         backendId,
		messagesToBackend: msgs,
		clients:           clients,
	}
	m.readAndRouteBackendResponses(wsConn)

	return m
}

const (
	MessageFormat string = "%s||%s||%s"
	ConnectType          = "0"
	BodyType             = "1"
	CloseType            = "2"
)

type Multiplexer struct {
	backendId         string
	messagesToBackend chan string
	clients           map[string]chan<- string
}

func (m *Multiplexer) initializeConnection() (string, <-chan string) {
	uid := uuid.New()
	clientChan := make(chan string)
	m.clients[uid] = clientChan
	return uid, clientChan
}

func (m *Multiplexer) sendConnect(msgKey, url string) {
	message := fmt.Sprintf(MessageFormat, msgKey, ConnectType, url)
	m.messagesToBackend <- message
}

func (m *Multiplexer) send(msgKey, msg string) {
	message := fmt.Sprintf(MessageFormat, msgKey, BodyType, msg)
	m.messagesToBackend <- message
}

func (m *Multiplexer) sendClose(msgKey string) {
	message := fmt.Sprintf(MessageFormat, msgKey, BodyType, "")
	m.messagesToBackend <- message
}

func (m *Multiplexer) readAndRouteBackendResponses(wsConn *websocket.Conn) {
	go func() {
		for {
			_, msg, err := wsConn.ReadMessage()
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Error("Error reading message.")
				continue
			}

			parts := strings.SplitN(string(msg), "||", 2)
			clientKey := parts[0]
			msgString := parts[1]
			if client, ok := m.clients[clientKey]; ok {
				client <- msgString
			} else {
				log.WithFields(log.Fields{
					"key": clientKey,
				}).Warn("Could not find channel for message. Dropping message and sending close to backend.")
				m.sendClose(clientKey)
			}
		}
	}()

	go func() {
		for {
			message := <-m.messagesToBackend
			err := wsConn.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Error("Error writing message.")
			}
		}
	}()
}
