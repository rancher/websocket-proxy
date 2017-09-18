package k8s

import (
	"fmt"
	"net/http"

	"github.com/Sirupsen/logrus"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
)

type BackendAccess interface {
	AuthAndLookup(req *http.Request) (*jwt.Token, string, error)
	ServeRemoteHTTP(token *jwt.Token, hostKey string, rw http.ResponseWriter, req *http.Request) error
}

type handler struct {
	lookup              *Lookup
	accessKey           string
	secretKey           string
	frontendHTTPHandler BackendAccess
	netesProxy          *netesProxy
}

func Handler(frontendHTTPHandler BackendAccess, cattleAddr, accessKey, secretKey string) (http.Handler, error) {
	np, err := newNetesProxy()
	if err != nil {
		return nil, err
	}

	return &handler{
		lookup:              NewLookup(fmt.Sprintf("http://%s/v3/clusters", cattleAddr), accessKey, secretKey),
		accessKey:           accessKey,
		secretKey:           secretKey,
		frontendHTTPHandler: frontendHTTPHandler,
		netesProxy:          np,
	}, nil
}

func (h *handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	cluster, _, err := h.lookup.Lookup(req)
	if err != nil {
		logrus.Errorf("Failed to find cluster: %v", err)
		http.Error(rw, fmt.Sprintf("Failed to find cluster: %v", err), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.Error(rw, "Failed to find cluster", http.StatusNotFound)
		return
	}

	if cluster.Embedded {
		h.netesProxy.Handle(rw, req)
		return
	}

	vars := mux.Vars(req)
	vars["service"] = fmt.Sprintf("k8s-api.%s", cluster.Id)

	//oldAuth := req.Header.Get("Authorization")
	req.SetBasicAuth(h.accessKey, h.secretKey)

	token, hostKey, err := h.frontendHTTPHandler.AuthAndLookup(req)
	if err != nil {
		http.Error(rw, fmt.Sprintf("Failed to authorize cluster: %v", err), http.StatusInternalServerError)
		return
	}

	//req.Header.Set("Authorization", oldAuth)
	req.Header.Set("Authorization", "Bearer "+cluster.K8sClientConfig.BearerToken)
	req.Header.Set("X-API-Cluster-Id", cluster.Id)

	if err := h.frontendHTTPHandler.ServeRemoteHTTP(token, hostKey, rw, req); err != nil {
		logrus.Errorf("Failed to forward request: %v", err)
		http.Error(rw, fmt.Sprintf("Failed to forward request: %v", err), http.StatusInternalServerError)
	}
}
