package proxy

import (
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

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
		// TODO Make this better
		http.Error(rw, "Failed to upgrade", 500)
		return
	}

	multiplexer := newMultiplexer(hostId, wsConn)
	h.registerBackEnd <- multiplexer
}

func newMultiplexer(backendId string, wsConn *websocket.Conn) *Multiplexer {
	msgs := make(chan string, 10)
	clients := make(map[string]chan<- common.Message)
	m := &Multiplexer{
		backendId:         backendId,
		messagesToBackend: msgs,
		frontendChans:     clients,
	}
	m.routeMessages(wsConn)

	return m
}
