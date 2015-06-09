package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type FrontendHandler struct {
	backend         backendProxy
	parsedPublicKey interface{}
}

func (h *FrontendHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	startTime := time.Now()
	defer func() {
		finishTime := time.Now()
		elapsedTime := finishTime.Sub(startTime)
		logAccess(rw, req, elapsedTime)
	}()

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
		log.Errorf("Error during upgrade: [%v]", err)
		http.Error(rw, "Failed to upgrade connection.", 500)
		return
	}

	msgKey, respChannel, err := h.backend.initializeClient(hostKey)
	if err != nil {
		log.Errorf("Error during initialization: [%v]", err)
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

func (h *FrontendHandler) auth(req *http.Request) (string, bool) {
	token, err := parseToken(req, h.parsedPublicKey)
	if err != nil {
		log.WithFields(log.Fields{"error": err}).Error("Error parsing token.")
		return "", false
	}

	if !token.Valid {
		return "", false
	}

	hostUuid, found := token.Claims["hostUuid"]
	if found {
		if hostKey, ok := hostUuid.(string); ok && h.backend.hasBackend(hostKey) {
			return hostKey, true
		}
	}
	log.WithFields(log.Fields{"hostUuid": hostUuid}).Errorf("Invalid backend host requested.")
	return "", false
}

func (h *FrontendHandler) closeConnection(ws *websocket.Conn) {
	ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	ws.Close()
}

func parseToken(req *http.Request, parsedPublicKey interface{}) (*jwt.Token, error) {
	tokenString := ""
	if authHeader := req.Header.Get("Authorization"); authHeader != "" {
		if len(authHeader) > 6 && strings.EqualFold("bearer", authHeader[0:6]) {
			tokenString = strings.Trim(authHeader[7:], " ")
		}
	}

	if tokenString == "" {
		tokenString = req.URL.Query().Get("token")
	}

	if tokenString == "" {
		return nil, fmt.Errorf("No JWT provided")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return parsedPublicKey, nil
	})
	return token, err
}
