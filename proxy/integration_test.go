package proxy

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/backend"
	"github.com/rancherio/websocket-proxy/common"
	"github.com/rancherio/websocket-proxy/test_utils"
)

var privateKey interface{}

func TestMain(m *testing.M) {
	c := getTestConfig()
	privateKey = test_utils.ParseTestPrivateKey()

	ps := &ProxyStarter{
		BackendPaths:       []string{"/v1/connectbackend"},
		FrontendPaths:      []string{"/v1/echo", "/v1/oneanddone"},
		CattleWSProxyPaths: []string{"/v1/subscribe"},
		CattleProxyPaths:   []string{"/{cattle-proxy:.*}"},
		Config:             c,
	}
	go ps.StartProxy()

	handlers := make(map[string]backend.Handler)
	handlers["/v1/echo"] = &echoHandler{}
	handlers["/v1/oneanddone"] = &oneAndDoneHandler{}
	signedToken := test_utils.CreateBackendToken("1", privateKey)
	url := "ws://localhost:1111/v1/connectbackend?token=" + signedToken
	go backend.ConnectToProxy(url, handlers)

	router := mux.NewRouter()
	router.HandleFunc("/v1/subscribe", getWsHandler())
	router.HandleFunc("/{foo:.*}", func(rw http.ResponseWriter, req *http.Request) {
		rw.Write([]byte("SUCCESS"))
	})
	go http.ListenAndServe("127.0.0.1:3333", router)

	time.Sleep(50 * time.Millisecond) // Give front and back a chance to initialize

	os.Exit(m.Run())
}

func getWsHandler() func(rw http.ResponseWriter, req *http.Request) {
	return func(rw http.ResponseWriter, req *http.Request) {
		if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			rw.Write([]byte("SUCCESS"))
			return
		}

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		ws, err := upgrader.Upgrade(rw, req, nil)
		if err != nil {
			log.Fatal(err)
		}

		ws.WriteMessage(websocket.TextMessage, []byte("WSSUCCESS"))
		ws.Close()
	}
}

func TestEndToEnd(t *testing.T) {
	signedToken := test_utils.CreateToken("1", privateKey)
	ws := getClientConnection("ws://localhost:1111/v1/echo?token="+signedToken, t)
	sendAndAssertReply(ws, strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10), t)
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	sendAndAssertReply(ws, strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10), t)
}

func TestAuthHeaderBearerToken(t *testing.T) {
	signedToken := test_utils.CreateToken("1", privateKey)
	dialer := &websocket.Dialer{}
	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+signedToken)
	ws, _, err := dialer.Dial("ws://localhost:1111/v1/echo", headers)
	if err != nil {
		t.Fatal(err)
	}
	sendAndAssertReply(ws, strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10), t)
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	sendAndAssertReply(ws, strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10), t)
}

func TestBackendClosesConnection(t *testing.T) {
	signedToken := test_utils.CreateToken("1", privateKey)
	ws := getClientConnection("ws://localhost:1111/v1/oneanddone?token="+signedToken, t)

	if err := ws.WriteMessage(1, []byte("a message")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ws.ReadMessage(); err != nil {
		t.Fatal(err)
	}

	if msgType, msgBytes, err := ws.ReadMessage(); err != io.EOF {
		t.Fatalf("Expected an EOF error to indicate connection was closed. [%v] [%s] [%v]", msgType, msgBytes, err)
	}
}

func TestFrontendClosesConnection(t *testing.T) {
	signedToken := test_utils.CreateToken("1", privateKey)
	ws := getClientConnection("ws://localhost:1111/v1/oneanddone?token="+signedToken, t)
	if err := ws.WriteControl(websocket.CloseMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("Expecrted error indicating websocket was closed.")
	}
}

func TestCattleProxy(t *testing.T) {
	resp, err := http.Get("http://localhost:1111/v1/foo1")
	assertProxyResponse(resp, err, t)

	req, err := http.NewRequest("PUT", "http://localhost:1111/v1///foo2", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{}
	resp, err = client.Do(req)
	assertProxyResponse(resp, err, t)

	resp, err = http.Get("http://localhost:1111/v1/subscribe")
	assertProxyResponse(resp, err, t)
}

func TestCattleWsProxy(t *testing.T) {
	ws := getClientConnection("ws://localhost:1111/v1/subscribe", t)
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "WSSUCCESS" {
		t.Fatal("Unexpected message ", msg)
	}

}

func assertProxyResponse(resp *http.Response, err error, t *testing.T) {
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("Bad response. ", resp, err)
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "SUCCESS" {
		t.Fatal("Unexpected body ", b)
	}
}

func getClientConnection(url string, t *testing.T) *websocket.Conn {
	dialer := &websocket.Dialer{}
	headers := http.Header{}
	ws, _, err := dialer.Dial(url, headers)
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

func (e *oneAndDoneHandler) Handle(key string, initialMessage string, incomingMessages <-chan string, response chan<- common.Message) {
	defer backend.SignalHandlerClosed(key, response)
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

func (e *echoHandler) Handle(key string, initialMessage string, incomingMessages <-chan string, response chan<- common.Message) {
	defer backend.SignalHandlerClosed(key, response)
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

func getTestConfig() *Config {

	pubKey, err := ParsePublicKey("../test_utils/public.pem")
	if err != nil {
		log.Fatal("Failed to parse key. ", err)
	}
	config := &Config{
		PublicKey:  pubKey,
		ListenAddr: "127.0.0.1:1111",
		CattleAddr: "127.0.0.1:3333",
	}
	return config
}

/*

TODO Add a test that utilizes this handler and creates a ton of connections

type LogsHandler struct {
}

func (l *LogsHandler) Handle(key string, initialMessage string, incomingMessages <-chan string, response chan<- common.Message) {
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
