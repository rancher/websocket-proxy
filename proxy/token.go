package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/patrickmn/go-cache"
)

const (
	authHeader     = "Authorization"
	projectHeader  = "X-API-Project-Id"
	defaultService = "swarm:2375"
)

type TokenLookup struct {
	cache           *cache.Cache
	client          http.Client
	serviceProxyURL string
}

func NewTokenLookup(cattleAddr string) *TokenLookup {
	t := &TokenLookup{
		cache:           cache.New(30*time.Second, 30*time.Second),
		serviceProxyURL: fmt.Sprintf("http://%s/v2-beta/serviceproxies", cattleAddr),
	}
	t.client.Timeout = 60 * time.Second
	return t
}

func (t *TokenLookup) Lookup(r *http.Request) (string, error) {
	cacheKey, token := t.getFromCache(r)
	if token != "" {
		return token, nil
	}

	token, err := t.callRancher(r)
	if err != nil {
		return "", err
	}

	if token != "" {
		t.cache.Set(cacheKey, token, cache.DefaultExpiration)
	}

	return token, err
}

func (t *TokenLookup) getFromCache(r *http.Request) (string, string) {
	key := genKey(r)
	value, ok := t.cache.Get(key)
	if ok {
		return key, value.(string)
	}
	return key, ""
}

func (t *TokenLookup) callRancher(r *http.Request) (string, error) {
	vars := mux.Vars(r)
	service, ok := vars["service"]
	if !ok {
		service = defaultService
	}

	parts := strings.SplitN(service, ":", 2)
	port := 80
	scheme := "http"
	if len(parts) == 2 {
		var err error
		port, err = strconv.Atoi(parts[1])
		if err != nil {
			return "", err
		}

		if strings.HasSuffix(parts[1], "443") {
			scheme = "https"
		}
	}

	body, err := json.Marshal(&ServiceProxyRequest{
		Service: parts[0],
		Port:    port,
		Scheme:  scheme,
	})

	logrus.Debugf("Calling rancher to get token: %s", t.serviceProxyURL)
	newReq, err := http.NewRequest("POST", t.serviceProxyURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	newReq.Header.Set("Content-Type", "application/json")

	// Other forms of auth
	newReq.Header.Set(authHeader, unwrapAuth(r.Header.Get(authHeader)))
	newReq.Header.Set(projectHeader, r.Header.Get(projectHeader))

	if project, ok := vars["project"]; ok {
		newReq.Header.Set(projectHeader, project)
	}

	c, err := r.Cookie("token")
	if err == nil {
		newReq.AddCookie(c)
	}

	resp, err := t.client.Do(newReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return "", noAuthError{}
	} else if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP error: %s, %d", resp.Status, resp.StatusCode)
	}

	respBody := ServiceProxyResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return "", err
	}

	return respBody.Token, nil
}

func unwrapAuth(auth string) string {
	if !strings.HasPrefix(auth, "Bearer ") {
		return auth
	}

	if tmp, err := base64.StdEncoding.DecodeString(auth[7:]); err == nil && len(strings.Split(string(tmp), " ")) == 2 {
		// This is double base64 auth encoding that we do w/ k8s
		return string(tmp)
	}

	return auth
}

func genKey(r *http.Request) string {
	vars := mux.Vars(r)
	hash := sha256.New()

	writeHeader(hash, authHeader, r)
	writeHeader(hash, projectHeader, r)
	for _, v := range []string{"project", "service"} {
		hash.Write([]byte(v))
		hash.Write([]byte(vars[v]))
	}

	hash.Write([]byte("token"))
	c, err := r.Cookie("token")
	if err == nil {
		hash.Write([]byte(c.String()))
	}

	if r.TLS != nil && len(r.TLS.PeerCertificates) == 1 {
		hash.Write([]byte(r.TLS.PeerCertificates[0].Subject.CommonName))
	}

	return hex.EncodeToString(hash.Sum(nil))
}

func writeHeader(h hash.Hash, key string, r *http.Request) {
	h.Write([]byte(key))
	h.Write([]byte(r.Header.Get(key)))
}

type ServiceProxyRequest struct {
	Service string `json:"service"`
	Port    int    `json:"port"`
	Scheme  string `json:"scheme"`
}

type ServiceProxyResponse struct {
	Token string `json:"token"`
}
