package proxy

import (
	"net/http"

	"github.com/Sirupsen/logrus"
)

func NewHttpPipe(rw http.ResponseWriter, backend backendProxy, hostKey string) (*BackendHttpReader, *BackendHttpWriter, error) {
	msgKey, respChannel, err := backend.initializeClient(hostKey)
	if err != nil {
		return nil, nil, err
	}

	logrus.Debugf("BACKEND PIPE %s %s", hostKey, msgKey)

	if err = backend.connect(hostKey, msgKey, "/v1/container-proxy/"); err != nil {
		backend.closeConnection(hostKey, msgKey)
		return nil, nil, err
	}

	return NewBackendHttpReader(rw, hostKey, msgKey, backend, respChannel), &BackendHttpWriter{
		hostKey: hostKey,
		msgKey:  msgKey,
		backend: backend,
	}, nil
}
