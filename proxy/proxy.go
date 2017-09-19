package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"sync"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/rancher/websocket-proxy/k8s"
	"github.com/rancher/websocket-proxy/proxy/apiinterceptor"
	"github.com/rancher/websocket-proxy/proxy/proxyprotocol"
	proxyTls "github.com/rancher/websocket-proxy/proxy/tls"
	"github.com/rancher/websocket-proxy/proxy/websocket"
)

var slashRegex = regexp.MustCompile("[/]{2,}")

type Starter struct {
	BackendPaths       []string
	FrontendPaths      []string
	FrontendHTTPPaths  []string
	StatsPaths         []string
	CattleProxyPaths   []string
	CattleWSProxyPaths []string
	Config             *Config
}

func (s *Starter) StartProxy() error {
	switcher := NewSwitcher(s.Config)

	backendMultiplexers := make(map[string]*multiplexer)
	bpm := &backendProxyManager{
		multiplexers: backendMultiplexers,
		mu:           &sync.RWMutex{},
	}

	frontendHandler := switcher.Wrap(&FrontendHandler{
		backend:         bpm,
		parsedPublicKey: s.Config.PublicKey,
	})

	statsHandler := switcher.Wrap(&StatsHandler{
		backend:         bpm,
		parsedPublicKey: s.Config.PublicKey,
	})

	backendHandler := switcher.Wrap(&BackendHandler{
		proxyManager:    bpm,
		parsedPublicKey: s.Config.PublicKey,
	})

	frontendHTTPHandlerInner := &FrontendHTTPHandler{
		FrontendHandler: FrontendHandler{
			backend:         bpm,
			parsedPublicKey: s.Config.PublicKey,
		},
		HTTPSPorts:  s.Config.ProxyProtoHTTPSPorts,
		TokenLookup: NewTokenLookup(s.Config.CattleAddr),
	}

	frontendHTTPHandler := switcher.Wrap(frontendHTTPHandlerInner)

	cattleProxy, cattleWsProxy, err := newCattleProxies(s.Config)
	if err != nil {
		log.Fatalf("Couldn't create cattle proxies: %v", err)
	}

	k8sHandler, err := k8s.Handler(frontendHTTPHandlerInner,
		s.Config.CattleAddr,
		s.Config.CattleAccessKey,
		s.Config.CattleSecretKey)
	if err != nil {
		log.Fatalf("Couldn't create k8s proxies: %v", err)
	}

	k8sHandler = switcher.Wrap(k8sHandler)

	router := mux.NewRouter()

	for _, p := range s.BackendPaths {
		router.Handle(p, backendHandler).Methods("GET")
	}
	for _, p := range s.FrontendPaths {
		router.Handle(p, frontendHandler).Methods("GET")
	}
	for _, p := range s.FrontendHTTPPaths {
		router.Handle(p, frontendHTTPHandler).Methods("GET", "POST", "PUT", "DELETE", "PATCH", "HEAD")
	}
	for _, p := range s.StatsPaths {
		router.Handle(p, statsHandler).Methods("GET")
	}

	for _, p := range s.CattleWSProxyPaths {
		router.Handle(p, cattleWsProxy)
	}

	router.Handle("/k8s/clusters/{clusterId}{path:.*}", k8sHandler)

	for _, p := range s.CattleProxyPaths {
		router.Handle(p, cattleProxy)
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

	listener = &proxyprotocol.Listener{listener}

	if s.Config.TLSListenAddr != "" {
		tlsConfig, err := s.setupTLS()
		if err != nil {
			return err
		}

		if s.Config.TLSListenAddr == s.Config.ListenAddr {
			listener = &proxyTls.SplitListener{
				Listener: listener,
				Config:   tlsConfig,
			}
		} else {
			tlsListener, err := net.Listen("tcp", s.Config.TLSListenAddr)
			if err != nil {
				return err
			}
			tlsListener = &proxyprotocol.Listener{tlsListener}
			go func() {
				defer listener.Close()
				log.Error(server.Serve(tls.NewListener(tlsListener, tlsConfig)))
			}()
		}
	}

	err = server.Serve(listener)
	return err
}

func (s *Starter) setupTLS() (*tls.Config, error) {
	if s.Config.CattleAccessKey == "" {
		return nil, fmt.Errorf("No access key supplied to download cert")
	}

	certs, err := s.Config.GetCerts()
	if err != nil {
		return nil, err
	}

	tlsCert, err := tls.X509KeyPair(certs.Cert, certs.Key)
	if err != nil {
		return nil, err
	}

	clientCas := x509.NewCertPool()
	if !clientCas.AppendCertsFromPEM(certs.CA) {
		return nil, err
	}

	tlsConfig := tlsconfig.ServerDefault()
	tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	tlsConfig.ClientCAs = clientCas
	tlsConfig.Certificates = []tls.Certificate{tlsCert}

	return tlsConfig, nil
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

func newWSProxy(config *Config) http.Handler {
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
		p:          cattleProxy,
		httpsPorts: config.ProxyProtoHTTPSPorts,
	}

	wsProxy := &cattleWSProxy{
		reverseProxy: reverseProxy,
		cattleAddr:   cattleAddr,
	}

	return wsProxy
}

func newCattleProxies(config *Config) (*proxyProtocolConverter, *cattleWSProxy, error) {
	cattleAddr := config.CattleAddr

	apiProxyHandler, err := apiinterceptor.NewInterceptor(config.APIInterceptorConfigFile, cattleAddr)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Couldn't create API interceptor")
	}

	reverseProxy := &proxyProtocolConverter{
		httpsPorts: config.ProxyProtoHTTPSPorts,
		p:          apiProxyHandler,
	}

	wsProxy := &cattleWSProxy{
		reverseProxy: reverseProxy,
		cattleAddr:   cattleAddr,
	}

	return reverseProxy, wsProxy, nil
}

type proxyProtocolConverter struct {
	httpsPorts map[int]bool
	p          http.Handler
}

func (h *proxyProtocolConverter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	proxyprotocol.AddHeaders(req, h.httpsPorts)
	h.p.ServeHTTP(rw, req)
}

type cattleWSProxy struct {
	reverseProxy *proxyProtocolConverter
	cattleAddr   string
}

func (h *cattleWSProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if websocket.ShouldProxy(req) {
		proxyprotocol.AddHeaders(req, h.reverseProxy.httpsPorts)
		websocket.ProxyTCP(h.cattleAddr, rw, req)
	} else {
		h.reverseProxy.ServeHTTP(rw, req)
	}
}
