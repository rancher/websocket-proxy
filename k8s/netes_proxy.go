package k8s

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/rancher/websocket-proxy/proxy/websocket"
)

const (
	scheme = "http"
	addr   = "localhost:8089"
)

type netesProxy struct {
	httpProxy *httputil.ReverseProxy
}

func newNetesProxy() (*netesProxy, error) {
	u, err := url.Parse(scheme + "://" + addr)
	if err != nil {
		return nil, err
	}

	httpProxy := httputil.NewSingleHostReverseProxy(u)
	httpProxy.FlushInterval = 100 + time.Millisecond
	return &netesProxy{
		httpProxy: httpProxy,
	}, nil
}

func (n *netesProxy) Handle(rw http.ResponseWriter, req *http.Request) {
	if websocket.ShouldProxy(req) {
		websocket.Proxy(scheme, addr, rw, req)
	} else {
		n.httpProxy.ServeHTTP(rw, req)
	}
}
