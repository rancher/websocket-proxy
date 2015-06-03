package proxy

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type FrontendHandler struct {
	backendMultiplexers map[string]*Multiplexer
}

func (h *FrontendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	hostId := h.getHostId(req)
	multiplexer, ok := h.backendMultiplexers[hostId]
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

	msgKey, respChannel := multiplexer.initializeClient()

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
	multiplexer.connect(msgKey, url)

	// Send request messages to backend
	for {
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			multiplexer.closeConnection(msgKey)
			h.closeConnection(ws)
			return
		}
		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			multiplexer.send(msgKey, string(msg))
		}
	}
}

func (h *FrontendHandler) getHostId(req *http.Request) string {
	return req.FormValue("hostId")
}

func (h *FrontendHandler) closeConnection(ws *websocket.Conn) {
	ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	ws.Close()
}
