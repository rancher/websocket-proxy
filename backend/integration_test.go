package backend

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
	"github.com/rancherio/websocket-proxy/proxy"
)

func TestMain(m *testing.M) {
	go proxy.StartProxy("127.0.0.1:2222")

	os.Exit(m.Run())
}

func TestBackendGoesAway(t *testing.T) {
	dialer := &websocket.Dialer{}
	headers := http.Header{}
	headers.Add("X-Cattle-HostId", "1")
	backendWs, _, err := dialer.Dial("ws://127.0.0.1:2222/connectbackend", headers)
	if err != nil {
		t.Fatal("Failed to connect to proxy.", err)
	}

	handlers := make(map[string]Handler)
	handlers["/v1/echo"] = &echoHandler{}
	go connectToProxyWS(backendWs, handlers)

	ws := getClientConnection("ws://localhost:2222/v1/echo?hostId=1", t)

	if err := ws.WriteMessage(1, []byte("a message")); err != nil {
		t.Fatal(err)
	}

	backendWs.Close()

	if _, _, err := ws.ReadMessage(); err != io.EOF {
		t.Fatal("Expected error indicating websocket was closed.")
	}

	dialer = &websocket.Dialer{}
	ws, _, err = dialer.Dial("ws://127.0.0.1:2222/v1/oneanddone?hostId=1", http.Header{})
	if ws != nil || err != websocket.ErrBadHandshake {
		t.Fatal("Should not have been able to connect.")
	}

}

func getClientConnection(url string, t *testing.T) *websocket.Conn {
	dialer := &websocket.Dialer{}
	ws, _, err := dialer.Dial(url, http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

type echoHandler struct {
}

func (e *echoHandler) Handle(key string, initialMessage string, incomingMessages <-chan string, response chan<- common.Message) {
	defer SignalHandlerClosed(key, response)
	for {
		m, ok := <-incomingMessages
		if !ok {
			return
		}
		if m != "" {
			data := fmt.Sprintf("%s-response", m)
			wrap := common.Message{
				Key:  key,
				Type: common.Body,
				Body: data,
			}
			response <- wrap
		}
	}
}
