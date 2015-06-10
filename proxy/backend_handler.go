package proxy

import (
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
)

type BackendHandler struct {
	proxyManager    proxyManager
	parsedPublicKey interface{}
}

func (h *BackendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	hostKey, authed := h.auth(req)
	if !authed {
		http.Error(rw, "Failed authentication", 401)
		return
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

func (h *BackendHandler) auth(req *http.Request) (string, bool) {
	token, err := parseToken(req, h.parsedPublicKey)
	if err != nil {
		log.WithFields(log.Fields{"error": err}).Error("Error parsing token.")
		return "", false
	}

	reportedUuid, found := token.Claims["reportedUuid"]
	if !found {
		return "", false
	}

	hostKey, ok := reportedUuid.(string)
	if !ok || hostKey == "" {
		return "", false
	}

	return hostKey, true
}
