package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

var slashRegex = regexp.MustCompile("[/]{2,}")

type ProxyStarter struct {
	BackendPaths       []string
	FrontendPaths      []string
	CattleProxyPaths   []string
	CattleWSProxyPaths []string
	Config             *Config
}

func (s *ProxyStarter) StartProxy() error {
	backendMultiplexers := make(map[string]*multiplexer)
	bpm := &backendProxyManager{
		multiplexers: backendMultiplexers,
		mu:           &sync.RWMutex{},
	}

	frontendHandler := &FrontendHandler{
		backend:         bpm,
		parsedPublicKey: s.Config.PublicKey,
	}

	backendHandler := &BackendHandler{
		proxyManager: bpm,
	}

	cattleProxy, cattleWsProxy := newCattleProxies(s.Config.CattleAddr)

	router := mux.NewRouter()
	for _, p := range s.BackendPaths {
		router.Handle(p, backendHandler).Methods("GET")
	}
	for _, p := range s.FrontendPaths {
		router.Handle(p, frontendHandler).Methods("GET")
	}

	for _, p := range s.CattleWSProxyPaths {
		router.Handle(p, cattleWsProxy)
	}

	for _, p := range s.CattleProxyPaths {
		router.Handle(p, cattleProxy)
	}

	pcRouter := &pathCleaner{
		router: router,
	}

	server := &http.Server{
		Handler: pcRouter,
		Addr:    s.Config.ListenAddr,
	}
	err := server.ListenAndServe()
	return err
}

type pathCleaner struct {
	router *mux.Router
}

func (p *pathCleaner) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if cleanedPath := p.cleanPath(req.URL.Path); cleanedPath != req.URL.Path {
		req.URL.Path = cleanedPath
		req.URL.Scheme = "http"
	}
	p.router.ServeHTTP(rw, req)
}

func (p *pathCleaner) cleanPath(path string) string {
	return slashRegex.ReplaceAllString(path, "/")
}

func newCattleProxies(cattleAddr string) (*httputil.ReverseProxy, *cattleWSProxy) {
	director := func(req *http.Request) {
		// TODO Do I need to set X-Forwarded-For and X-Forwarded-Host?
		req.URL.Scheme = "http"
		req.URL.Host = cattleAddr
	}
	cattleProxy := &httputil.ReverseProxy{
		Director: director,
	}

	wsProxy := &cattleWSProxy{
		reverseProxy: cattleProxy,
		cattleAddr:   cattleAddr,
	}

	return cattleProxy, wsProxy
}

type cattleWSProxy struct {
	reverseProxy *httputil.ReverseProxy
	cattleAddr   string
}

func (h *cattleWSProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		h.serveWebsocket(rw, req)
	} else {
		h.reverseProxy.ServeHTTP(rw, req)
	}
}

func (h *cattleWSProxy) serveWebsocket(rw http.ResponseWriter, req *http.Request) {
	// TODO Document where I found this.
	target := h.cattleAddr
	// target := "[localhost]:8081"
	d, err := net.Dial("tcp", target)
	if err != nil {
		log.WithField("error", err).Error("Error dialing websocket backend.")
		http.Error(rw, "Unable to establish websocket connection.", 500)
		return
	}
	hj, ok := rw.(http.Hijacker)
	if !ok {
		http.Error(rw, "Unable to establish websocket connection.", 500)
		return
	}
	nc, _, err := hj.Hijack()
	if err != nil {
		log.WithField("error", err).Error("Hijack error.")
		http.Error(rw, "Unable to establish websocket connection.", 500)
		return
	}
	defer nc.Close()
	defer d.Close()

	err = req.Write(d)
	if err != nil {
		log.WithField("error", err).Error("Error copying request to target.")
		return
	}

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}
	go cp(d, nc)
	go cp(nc, d)
	<-errc
}
