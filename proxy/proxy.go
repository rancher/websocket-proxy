package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"

	"github.com/rancherio/websocket-proxy/proxy/proxyprotocol"
)

var slashRegex = regexp.MustCompile("[/]{2,}")

type ProxyStarter struct {
	BackendPaths       []string
	FrontendPaths      []string
	StatsPaths         []string
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

	statsHandler := &StatsHandler{
		backend:         bpm,
		parsedPublicKey: s.Config.PublicKey,
	}

	backendHandler := &BackendHandler{
		proxyManager:    bpm,
		parsedPublicKey: s.Config.PublicKey,
	}

	cattleProxy, cattleWsProxy := newCattleProxies(s.Config)

	router := mux.NewRouter()
	for _, p := range s.BackendPaths {
		router.Handle(p, backendHandler).Methods("GET")
	}
	for _, p := range s.FrontendPaths {
		router.Handle(p, frontendHandler).Methods("GET")
	}

	for _, p := range s.StatsPaths {
		router.Handle(p, statsHandler).Methods("GET")
	}

	if s.Config.CattleAddr != "" {
		for _, p := range s.CattleWSProxyPaths {
			router.Handle(p, cattleWsProxy)
		}

		for _, p := range s.CattleProxyPaths {
			router.Handle(p, cattleProxy)
		}
	}

	if s.Config.ParentPid != 0 {
		go func() {
			for {
				process, err := os.FindProcess(s.Config.ParentPid)
				if err != nil {
					log.Fatalf("Failed to find process: %s\n", err)
				} else {
					err := process.Signal(syscall.Signal(0))
					if err != nil {
						log.Fatal("Parent process went away. Shutting down.")
					}
				}
				time.Sleep(time.Millisecond * 250)
			}
		}()
	}

	pcRouter := &pathCleaner{
		router: router,
	}

	server := &http.Server{
		Handler:   pcRouter,
		Addr:      s.Config.ListenAddr,
		ConnState: proxyprotocol.StateCleanup,
	}

	listener, err := net.Listen("tcp", s.Config.ListenAddr)
	if err != nil {
		log.Fatalf("Couldn't create listener: %s\n", err)
	}

	proxyListener := &proxyprotocol.Listener{listener}

	err = server.Serve(proxyListener)
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

func newCattleProxies(config *Config) (*proxyProtocolConverter, *cattleWSProxy) {
	cattleAddr := config.CattleAddr
	director := func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = cattleAddr
	}
	cattleProxy := &httputil.ReverseProxy{
		Director:      director,
		FlushInterval: time.Millisecond * 100,
	}

	reverseProxy := &proxyProtocolConverter{
		reverseProxy: cattleProxy,
		httpsPorts:   config.ProxyProtoHttpsPorts,
	}

	wsProxy := &cattleWSProxy{
		reverseProxy: reverseProxy,
		cattleAddr:   cattleAddr,
	}

	return reverseProxy, wsProxy
}

type proxyProtocolConverter struct {
	reverseProxy *httputil.ReverseProxy
	httpsPorts   map[int]bool
}

func (h *proxyProtocolConverter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	proxyprotocol.AddHeaders(req, h.httpsPorts)
	h.reverseProxy.ServeHTTP(rw, req)
}

type cattleWSProxy struct {
	reverseProxy *proxyProtocolConverter
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
	// Inspired by https://groups.google.com/forum/#!searchin/golang-nuts/httputil.ReverseProxy$20$2B$20websockets/golang-nuts/KBx9pDlvFOc/01vn1qUyVdwJ
	target := h.cattleAddr
	d, err := net.Dial("tcp", target)
	if err != nil {
		log.WithField("error", err).Error("Error dialing websocket backend.")
		http.Error(rw, "Unable to establish websocket connection: can't dial.", 500)
		return
	}
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
