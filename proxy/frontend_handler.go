package proxy

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type FrontendHandler struct {
	backend backendProxy
}

func (h *FrontendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	hostKey := h.getHostKey(req)
	if !h.backend.hasBackend(hostKey) {
		http.Error(rw, "Bad hostKey", 400)
		return
	}

	upgrader := websocket.Upgrader{}
	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		return
	}

	msgKey, respChannel, err := h.backend.initializeClient(hostKey)
	if err != nil {
		h.closeConnection(ws)
		return
	}

	// Send response messages to client
	go func() {
		for {
			message, ok := <-respChannel
			if !ok {
				h.closeConnection(ws)
				return
			}
			switch message.Type {
			case common.Body:
				ws.WriteMessage(1, []byte(message.Body))
			case common.Close:
				h.closeConnection(ws)
				return
			}
		}
	}()

	url := req.URL.String()
	if err = h.backend.connect(hostKey, msgKey, url); err != nil {
		h.closeConnection(ws)
		return
	}

	// Send request messages to backend
	for {
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			h.backend.closeConnection(hostKey, msgKey)
			h.closeConnection(ws)
			return
		}
		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			if err = h.backend.send(hostKey, msgKey, string(msg)); err != nil {
				h.closeConnection(ws)
				return
			}
		}
	}
}

func (h *FrontendHandler) getHostKey(req *http.Request) string {
	// TODO UNHACK
	return "1"
	// return req.FormValue("hostId")
}

func (h *FrontendHandler) closeConnection(ws *websocket.Conn) {
	ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	ws.Close()
}
