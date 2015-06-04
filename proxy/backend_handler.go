package proxy

import (
	"net/http"

	"github.com/gorilla/websocket"
)

type BackendHandler struct {
	proxyManager proxyManager
}

func (h *BackendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	hostKey := req.Header.Get("X-Cattle-HostId")
	if hostKey == "" {
		http.Error(rw, "Missing X-Cattle-HostId Header", 400)
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		http.Error(rw, "Failed to upgrade connection.", 500)
		return
	}

	h.proxyManager.addBackend(hostKey, ws)
}
