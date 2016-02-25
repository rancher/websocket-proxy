package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"

	"github.com/rancherio/websocket-proxy/common"
	"github.com/rancherio/websocket-proxy/proxy/proxyprotocol"
)

type FrontendHTTPHandler struct {
	FrontendHandler
	HttpsPorts  map[int]bool
	TokenLookup *TokenLookup
}

func (h *FrontendHTTPHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	token, hostKey, authed := h.auth(req)
	if !authed {
		http.Error(rw, "Failed authentication", 401)
		return
	}

	msgKey, respChannel, err := h.backend.initializeClient(hostKey)
	if err != nil {
		log.Errorf("Error during initialization: [%v]", err)
		return
	}
	defer h.backend.closeConnection(hostKey, msgKey)

	data := token.Claims["proxy"].(map[string]interface{})
	address, _ := data["address"].(string)
	scheme, _ := data["scheme"].(string)

	if err = h.backend.connect(hostKey, msgKey, "/v1/container-proxy/"); err != nil {
		return
	}

	proxyprotocol.AddHeaders(req, h.HttpsPorts)
	proxyprotocol.AddForwardedFor(req)

	vars := mux.Vars(req)
	buf := make([]byte, 4096, 4096)

	// Send request messages to backend
	eof := false
	url := *req.URL
	url.Host = address
	url.Path = vars["path"]
	if !strings.HasPrefix(url.Path, "/") {
		url.Path = "/" + url.Path
	}

	if scheme == "" {
		url.Scheme = "http"
	} else {
		url.Scheme = scheme
	}

	m := common.HttpMessage{
		Host:    req.Host,
		Method:  req.Method,
		URL:     url.String(),
		Headers: map[string][]string(req.Header),
	}

	for !eof {
		count, err := req.Body.Read(buf)
		if err == io.EOF {
			eof = true
		} else if err != nil {
			return
		}

		m.Body = buf[:count]
		m.EOF = eof
		data, err := json.Marshal(&m)
		if err != nil {
			return
		}

		if err = h.backend.send(hostKey, msgKey, string(data)); err != nil {
			return
		}

		m = common.HttpMessage{}
	}

	for message := range respChannel {
		switch message.Type {
		case common.Body:
			if _, err := writeResponse(rw, message.Body); err != nil {
				log.Debugf("Failed to write response, closing", err)
			}
		case common.Close:
			h.backend.closeConnection(hostKey, msgKey)
		}
	}
}

func writeResponse(rw http.ResponseWriter, message string) (bool, error) {
	var response common.HttpMessage
	if err := json.Unmarshal([]byte(message), &response); err != nil {
		return false, err
	}

	for k, v := range response.Headers {
		for _, vp := range v {
			rw.Header().Add(k, vp)
		}
	}

	if response.Code > 0 {
		rw.WriteHeader(response.Code)
	}

	if len(response.Body) > 0 {
		if _, err := rw.Write(response.Body); err != nil {
			return false, err
		}
	}

	return response.EOF, nil
}

func (h *FrontendHTTPHandler) auth(req *http.Request) (*jwt.Token, string, bool) {
	token, hostKey, ok := h.FrontendHandler.auth(req)
	if ok {
		return token, hostKey, ok
	}

	tokenString, err := h.TokenLookup.Lookup(req)
	if err != nil {
		log.WithFields(log.Fields{"error": err}).Error("Error looking up token.")
		return nil, "", false
	}

	token, err = jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return h.parsedPublicKey, nil
	})
	if err != nil {
		log.WithFields(log.Fields{"error": err}).Error("Error parsing token.")
		return nil, "", false
	}

	if !token.Valid {
		return nil, "", false
	}

	hostUuid, found := token.Claims["hostUuid"]
	if found {
		if hostKey, ok := hostUuid.(string); ok && h.backend.hasBackend(hostKey) {
			return token, hostKey, true
		}
	}
	log.WithFields(log.Fields{"hostUuid": hostUuid}).Infof("Invalid backend host requested.")
	return nil, "", false
}
