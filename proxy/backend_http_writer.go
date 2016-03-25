package proxy

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/rancherio/websocket-proxy/common"
)

type BackendHttpWriter struct {
	hostKey, msgKey string
	backend         backendProxy
}

func (b *BackendHttpWriter) Close() error {
	logrus.Debugf("BACKEND WRITE EOF %s", b.msgKey)
	return b.writeMessage(&common.HttpMessage{
		EOF: true,
	})
}

func (b *BackendHttpWriter) WriteRequest(req *http.Request, hijack bool, address, scheme string) error {
	vars := mux.Vars(req)

	url := *req.URL
	url.Host = address
	if path, ok := vars["path"]; ok {
		url.Path = path
	}
	if !strings.HasPrefix(url.Path, "/") {
		url.Path = "/" + url.Path
	}

	if scheme == "" {
		url.Scheme = "http"
	} else {
		url.Scheme = scheme
	}

	return b.writeMessage(&common.HttpMessage{
		Hijack:  hijack,
		Host:    req.Host,
		Method:  req.Method,
		URL:     url.String(),
		Headers: map[string][]string(req.Header),
	})
}

func (b *BackendHttpWriter) writeMessage(message *common.HttpMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	logrus.Debugf("BACKEND WRITE %s,%s: %s", b.hostKey, b.msgKey, data)
	return b.backend.send(b.hostKey, b.msgKey, string(data))
}

func (b *BackendHttpWriter) Write(buffer []byte) (int, error) {
	return len(buffer), b.writeMessage(&common.HttpMessage{
		Body: buffer,
	})
}
