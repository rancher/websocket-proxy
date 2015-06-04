package proxy

import (
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
)

type BackendHandler struct {
	proxyManager proxyManager
}

func (h *BackendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	hostKey := req.Header.Get("X-Cattle-HostId")
	if hostKey == "" {
		log.Errorf("No hostKey provided in backend connection request.")
		http.Error(rw, "Missing X-Cattle-HostId Header", 422)
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		log.Errorf("Error during upgrade for host [%v]: [%v]", hostKey, err)
		http.Error(rw, "Failed to upgrade connection.", 500)
		return
	}

	log.Infof("Registering backend for host [%v]", hostKey)
	h.proxyManager.addBackend(hostKey, ws)
}
