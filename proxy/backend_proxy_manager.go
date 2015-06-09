package proxy

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type backendProxy interface {
	initializeClient(backendKey string) (string, <-chan common.Message, error)
	connect(backendKey, msgKey, url string) error
	send(backendKey, msgKey, msg string) error
	closeConnection(backendKey, msgKey string) error
	hasBackend(backendKey string) bool
}

type proxyManager interface {
	addBackend(backendKey string, ws *websocket.Conn)
	removeBackend(backendKey string)
}

type backendProxyManager struct {
	multiplexers map[string]*multiplexer
	mu           *sync.RWMutex
}

func (b *backendProxyManager) initializeClient(backendKey string) (string, <-chan common.Message, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	multiplexer, ok := b.multiplexers[backendKey]
	if !ok {
		return "", nil, fmt.Errorf("No backend for key [%v]", backendKey)
	}
	msgKey, msgChan := multiplexer.initializeClient()
	return msgKey, msgChan, nil
}

func (b *backendProxyManager) connect(backendKey, msgKey, url string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	multiplexer, ok := b.multiplexers[backendKey]
	if !ok {
		return fmt.Errorf("No backend for key [%v]", backendKey)
	}
	multiplexer.connect(msgKey, url)
	return nil
}

func (b *backendProxyManager) send(backendKey, msgKey, msg string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	multiplexer, ok := b.multiplexers[backendKey]
	if !ok {
		return fmt.Errorf("No backend for key [%v]", backendKey)
	}
	multiplexer.send(msgKey, msg)
	return nil
}

func (b *backendProxyManager) closeConnection(backendKey, msgKey string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	multiplexer, ok := b.multiplexers[backendKey]
	if !ok {
		return fmt.Errorf("No backend for key [%v]", backendKey)
	}
	multiplexer.closeConnection(msgKey, true)
	return nil
}

func (b *backendProxyManager) hasBackend(backendKey string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.multiplexers[backendKey]
	return ok
}

func (b *backendProxyManager) addBackend(backendKey string, ws *websocket.Conn) {
	msgs := make(chan string, 10)
	clients := make(map[string]chan<- common.Message)
	m := &multiplexer{
		backendKey:        backendKey,
		messagesToBackend: msgs,
		frontendChans:     clients,
		proxyManager:      b,
		frontendMu:        &sync.RWMutex{},
	}
	m.routeMessages(ws)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.multiplexers[backendKey] = m
}

func (b *backendProxyManager) removeBackend(backendKey string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.multiplexers, backendKey)
}
