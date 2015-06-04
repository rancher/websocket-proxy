package backend

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

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
	backendWs, _, err := dialer.Dial("ws://localhost:2222/connectbackend", headers)
	if err != nil {
		t.Fatal("Failed to connect to proxy.", err)
	}

	handlers := make(map[string]Handler)
	handlers["/v1/oneanddone"] = &oneAndDoneHandler{}
	go connectToProxyWS(backendWs, handlers)

	ws := getClientConnection("ws://localhost:2222/v1/oneanddone?hostId=1", t)

	if err := ws.WriteMessage(1, []byte("a message")); err != nil {
		t.Fatal(err)
	}

	backendWs.Close()

	if _, _, err := ws.ReadMessage(); err != io.EOF {
		t.Fatal("Expected error indicating websocket was closed.")
	}

	dialer = &websocket.Dialer{}
	ws, _, err = dialer.Dial("ws://localhost:2222/v1/oneanddone?hostId=1", http.Header{})
	if ws != nil {
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

func sendAndAssertReply(ws *websocket.Conn, msg string, t *testing.T) {
	message := []byte(msg)
	err := ws.WriteMessage(1, message)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Sent: %s\n", message)

	_, reply, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Received: %s\n", reply)
	if msg+"-response" != string(reply) {
		t.Fatalf("Unexpected repsonse: [%v]", reply)
	}
}

type oneAndDoneHandler struct {
}

func (e *oneAndDoneHandler) Handle(key string, incomingMessages <-chan string, response chan<- common.Message) {
	defer SignalHandlerClosed(key, response)
	m := <-incomingMessages
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

type echoHandler struct {
}

func (e *echoHandler) Handle(key string, incomingMessages <-chan string, response chan<- common.Message) {
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

type LogsHandler struct {
}

func (l *LogsHandler) Handle(key string, incomingMessages <-chan string, response chan<- common.Message) {
	idx := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	msg := ""
	for {
		select {
		case m, ok := <-incomingMessages:
			if !ok {
				return
			}
			msg += m
		case <-ticker.C:
			if msg == "" {
				msg = "logs"
			}
			data := fmt.Sprintf("%s %d", msg, idx)
			wrap := common.Message{
				Key:  key,
				Body: data,
			}
			response <- wrap
		}
		idx++
	}
}

/*
	count := 0
	for count <= 10 {
		if count >= 10 && count%10 == 0 {
			msg := body + " " + strconv.Itoa(count)
			t.Logf("Sending new message [%s]\n", msg)
			ws.WriteMessage(1, []byte(msg))
		}
		_, msg, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Test Receive: %s\n", msg)
		count++
	}
*/
