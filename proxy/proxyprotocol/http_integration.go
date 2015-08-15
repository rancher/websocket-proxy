package proxyprotocol

import (
	"net"
	"net/http"
	"strconv"
)

func AddHeaders(req *http.Request, httpsPorts map[int]bool) {
	proxyProtoInfo := getInfo(req.RemoteAddr)
	if proxyProtoInfo != nil {
		if _, ok := req.Header["X-Forwarded-Proto"]; !ok {
			var proto string
			if _, ok := httpsPorts[proxyProtoInfo.ProxyAddr.Port]; ok {
				proto = "https"
			} else {
				proto = "http"
			}
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		if _, ok := req.Header["X-Forwarded-Port"]; !ok {
			req.Header.Set("X-Forwarded-Port", strconv.Itoa(proxyProtoInfo.ProxyAddr.Port))
		}
	}
}

func StateCleanup(conn net.Conn, connState http.ConnState) {
	if connState == http.StateClosed {
		deleteInfo(conn.RemoteAddr().String())
	}
}
