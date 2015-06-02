package proxy

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/backend"
)

func TestEndToEnd(t *testing.T) {
	go StartProxy("127.0.0.1:1111")

	handlers := make(map[string]backend.Handler)
	handlers["/v1/echo"] = &EchoHandler{}

	go backend.ConnectToProxy("ws://localhost:1111/connectbackend", handlers)
	time.Sleep(300 * time.Millisecond)

	url := "ws://localhost:1111/v1/echo?hostId=1"
	t.Logf("Connecting URL: %s\n", url)
	dialer := &websocket.Dialer{}
	headers := http.Header{}
	ws, _, err := dialer.Dial(url, headers)
	if err != nil {
		t.Fatal(err)
	}

	sendAndAssertReply(ws, strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10), t)
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	sendAndAssertReply(ws, strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10), t)

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

type EchoHandler struct {
}

func (e *EchoHandler) Handle(key string, incomingMessages <-chan string, response chan<- *backend.MessageWrapper) {
	for {
		m, ok := <-incomingMessages
		if !ok {
			return
		}
		data := fmt.Sprintf("%s-response", m)
		wrap := &backend.MessageWrapper{
			Key:     key,
			Message: data,
		}
		response <- wrap
	}
}

type LogsHandler struct {
}

func (l *LogsHandler) Handle(key string, incomingMessages <-chan string, response chan<- *backend.MessageWrapper) {
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
			wrap := &backend.MessageWrapper{
				Key:     key,
				Message: data,
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
