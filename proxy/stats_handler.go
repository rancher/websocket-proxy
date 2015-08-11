package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/websocket"

	"github.com/rancherio/websocket-proxy/common"
)

type StatsHandler struct {
	backend         backendProxy
	parsedPublicKey interface{}
}

type statsInfo struct {
	hostKey     string
	url         string
	msgKey      string
	respChannel <-chan common.Message
}

func (s *statsInfo) initializeClient(h *StatsHandler) error {
	if s.hostKey == "" {
		return fmt.Errorf("hostKey is empty")
	}
	msgKey, respChannel, err := h.backend.initializeClient(s.hostKey)
	if err != nil {
		return err
	}
	s.msgKey = msgKey
	s.respChannel = respChannel
	return nil
}

func (s *statsInfo) closeClient(h *StatsHandler) {
	h.backend.closeConnection(s.hostKey, s.msgKey)
}

func (s *statsInfo) connect(h *StatsHandler) error {
	return h.backend.connect(s.hostKey, s.msgKey, s.url)
}

func (h *StatsHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	multiHost := false

	if strings.HasSuffix(req.URL.Path, "project") || strings.HasSuffix(req.URL.Path, "project/") || strings.HasSuffix(req.URL.Path, "service") || strings.HasSuffix(req.URL.Path, "service/") {
		multiHost = true
	}

	statsInfoStructs, authed := h.auth(req, multiHost)
	if !authed {
		http.Error(rw, "Failed authentication", 401)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		http.Error(rw, "Failed to upgrade connection.", 500)
		return
	}

	var mutex sync.Mutex
	var countMutex sync.Mutex

	doneCounter := len(statsInfoStructs)

	defer func() {
		for _, statsInfoStruct := range statsInfoStructs {
			statsInfoStruct.closeClient(h)
		}
		closeConnection(ws)
	}()

	for _, statsInfoStruct := range statsInfoStructs {
		err := statsInfoStruct.initializeClient(h)
		if err != nil {
			return
		}

		// Send response messages to client
		go func(s *statsInfo) {
			errStatus := false
			for {
				message, ok := <-s.respChannel
				if !ok {
					return
				}
				switch message.Type {
				case common.Body:
					mutex.Lock()
					ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := ws.WriteMessage(1, []byte(message.Body)); err != nil {
						errStatus = true
					}
					mutex.Unlock()
				case common.Close:
					countMutex.Lock()
					errStatus = true
					doneCounter--
					countMutex.Unlock()
				}
				if errStatus && doneCounter == 0 {
					closeConnection(ws)
				}
			}
		}(statsInfoStruct)

		if err = statsInfoStruct.connect(h); err != nil {
			return
		}
	}
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			return
		}
	}
}

func (h *StatsHandler) auth(req *http.Request, multiHost bool) ([]*statsInfo, bool) {
	tokenString := req.URL.Query().Get("token")
	token, err := parseRequestToken(tokenString, h.parsedPublicKey)
	if err != nil {
		log.WithFields(log.Fields{"error": err}).Error("Error parsing token.")
		return nil, false
	}

	if !token.Valid {
		return nil, false
	}

	var statsInfoStructs []*statsInfo

	if multiHost {
		projectsOrServices, err := getProjectOrService(token)
		if err != nil {
			log.WithFields(log.Fields{"error": err}).Error("Error parsing token")
			return nil, false
		}
		for _, projectOrService := range projectsOrServices {
			data := projectOrService
			innerTokenString, ok := data["token"]
			if !ok {
				log.Error("Empty set of hosts or containers in project/service")
				return nil, false
			}
			innerJwtToken, err := parseRequestToken(innerTokenString, h.parsedPublicKey)
			if err != nil {
				log.WithFields(log.Fields{"error": err}).Error("Error parsing token")
				return nil, false
			}
			hostUuid, found := h.extractHostUuid(innerJwtToken)
			if !found {
				return nil, false
			}
			urlString, ok := data["url"]
			if !ok {
				log.WithFields(log.Fields{"error": err}).Error("Error parsing token")
				return nil, false
			}
			urlString = urlString + "?token=" + innerTokenString
			statsInfoStructs = append(statsInfoStructs, &statsInfo{hostKey: hostUuid, url: urlString})
		}
	} else {
		hostUuid, found := h.extractHostUuid(token)
		if !found {
			log.Error("could not find host uuid")
			return nil, false
		}
		statsInfoStructs = append(statsInfoStructs, &statsInfo{hostKey: hostUuid, url: req.URL.String()})
	}
	return statsInfoStructs, true
}

func getProjectOrService(token *jwt.Token) ([]map[string]string, error) {
	data, ok := token.Claims["project"]
	if !ok {
		data, ok = token.Claims["service"]
	}
	if ok {
		if interfaceList, isList := data.([]interface{}); isList {
			projectList := []map[string]string{}
			for _, inter := range interfaceList {
				projectInterfaceMap, ok := inter.(map[string]interface{})
				if ok {
					projectMap := map[string]string{}
					for key, value := range projectInterfaceMap {
						valueString, ok := value.(string)
						if ok {
							projectMap[key] = valueString
						} else {
							return nil, fmt.Errorf("invalid project/service input data type")
						}
					}
					projectList = append(projectList, projectMap)
				} else {
					return nil, fmt.Errorf("invalid project/service input data type")
				}
			}
			return projectList, nil
		}
		return nil, fmt.Errorf("invalid project/service input data type")
	}
	return nil, fmt.Errorf("empty token")
}

func (h *StatsHandler) extractHostUuid(token *jwt.Token) (string, bool) {
	hostUuid, found := token.Claims["hostUuid"]
	if !found {
		log.WithFields(log.Fields{"hostUuid": hostUuid}).Infof("HostUuid not found in token.")
		return "", false
	}
	hostKey, ok := hostUuid.(string)
	if !ok || !h.backend.hasBackend(hostKey) {
		log.WithFields(log.Fields{"hostUuid": hostUuid}).Infof("Invalid HostUuid.")
		return "", false
	}
	return hostKey, true
}

func parseRequestToken(tokenString string, parsedPublicKey interface{}) (*jwt.Token, error) {
	if tokenString == "" {
		return nil, fmt.Errorf("No JWT provided")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return parsedPublicKey, nil
	})
	return token, err
}
