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
	defer closeConnection(ws)

	msgKey, respChannel, err := h.backend.initializeClient(hostKey)
	if err != nil {
		log.Errorf("Error during initialization: [%v]", err)
		closeConnection(ws)
		return
	}
	defer h.backend.closeConnection(hostKey, msgKey)

	// Send response messages to client
	go func() {
		for {
			message, ok := <-respChannel
			if !ok {
				return
			}
			switch message.Type {
			case common.Body:
				ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := ws.WriteMessage(1, []byte(message.Body)); err != nil {
					closeConnection(ws)
				}
			case common.Close:
				closeConnection(ws)
			}
		}
	}()

	url := req.URL.String()
	if err = h.backend.connect(hostKey, msgKey, url); err != nil {
		return
	}

	// Send request messages to backend
	for {
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			if err = h.backend.send(hostKey, msgKey, string(msg)); err != nil {
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
	log.WithFields(log.Fields{"hostUuid": hostUuid}).Infof("Invalid backend host requested.")
	return "", false
}

func closeConnection(ws *websocket.Conn) {
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
