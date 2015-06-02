package proxy

import (
	"net/http"

	"github.com/gorilla/websocket"
)

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

	msgKey, respChannel := backend.initializeClient()
	go func() {
		for {
			msg := <-respChannel
			ws.WriteMessage(1, []byte(msg))
		}
	}()

	url := req.URL.String()
	backend.connect(msgKey, url)
	for {
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			backend.closeConnection(msgKey)
			ws.Close()
			return
		}
		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			backend.send(msgKey, string(msg))
		}
	}
}

func (h *FrontendHandler) getHostId(req *http.Request) string {
	return req.FormValue("hostId")
}
