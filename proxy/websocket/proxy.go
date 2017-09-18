package websocket

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"

	log "github.com/Sirupsen/logrus"
)

func ShouldProxy(req *http.Request) bool {
	return len(req.Header.Get("Upgrade")) > 0
}

func Proxy(scheme string, targetAddr string, rw http.ResponseWriter, req *http.Request) {
	if scheme == "https" {
		ProxyTLS(targetAddr, rw, req)
	} else {
		ProxyTCP(targetAddr, rw, req)
	}
}

func ProxyTLS(targetAddr string, rw http.ResponseWriter, req *http.Request) {
	d, err := tls.Dial("tcp", targetAddr, nil)
	if err != nil {
		log.WithField("error", err).Error("Error dialing websocket backend.")
		http.Error(rw, "Unable to establish websocket connection: can't dial.", 500)
		return
	}
	defer d.Close()

	proxy(d, rw, req)
}

func ProxyTCP(targetAddr string, rw http.ResponseWriter, req *http.Request) {
	d, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.WithField("error", err).Error("Error dialing websocket backend.")
		http.Error(rw, "Unable to establish websocket connection: can't dial.", 500)
		return
	}
	defer d.Close()

	proxy(d, rw, req)
}

func proxy(conn net.Conn, rw http.ResponseWriter, req *http.Request) {
	// Inspired by https://groups.google.com/forum/#!searchin/golang-nuts/httputil.ReverseProxy$20$2B$20websockets/golang-nuts/KBx9pDlvFOc/01vn1qUyVdwJ

	hj, ok := rw.(http.Hijacker)
	if !ok {
		http.Error(rw, "Unable to establish websocket connection: no hijacker.", 500)
		return
	}
	nc, _, err := hj.Hijack()
	if err != nil {
		log.WithField("error", err).Error("Hijack error.")
		http.Error(rw, "Unable to establish websocket connection: can't hijack.", 500)
		return
	}
	defer nc.Close()

	err = req.Write(conn)
	if err != nil {
		log.WithField("error", err).Error("Error copying request to target.")
		return
	}

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}
	go cp(conn, nc)
	go cp(nc, conn)
	<-errc

	return
}
