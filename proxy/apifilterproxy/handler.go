package apifilterproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/manager"
	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/model"
)

var filterHandler *APIFiltersHandler

//APIFiltersHandler is a wrapper over the mux router that does the path<->filters matching
type APIFiltersHandler struct {
	filterRouter *mux.Router
}

func (h *APIFiltersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.filterRouter.ServeHTTP(w, r)
}

//InitHandler sets the parameters necessary and initializes API Proxy handler
func InitHandler(filterConfigFile string, cattleAddr string) (*APIFiltersHandler, bool) {
	manager.SetConfig(filterConfigFile, cattleAddr)

	router := newFilterRouter(manager.ConfigFields)
	filterHandler = &APIFiltersHandler{filterRouter: router}

	return filterHandler, manager.APIFilterProxyReady
}

func newFilterRouter(configFields manager.ConfigFileFields) *mux.Router {
	// API framework routes
	router := mux.NewRouter().StrictSlash(false)

	for _, filter := range configFields.Prefilters {
		//build router paths
		for _, path := range filter.Paths {
			for _, method := range filter.Methods {
				log.Debugf("Adding route: %v %v", strings.ToUpper(method), path)
				router.Methods(strings.ToUpper(method)).Path(path).HandlerFunc(http.HandlerFunc(handleRequest))
			}
		}
	}
	router.Methods("POST").Path("/v1-api-filter-proxy/reload").HandlerFunc(http.HandlerFunc(reload))
	router.NotFoundHandler = http.HandlerFunc(handleNotFoundRequest)

	return router
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	path, _ := mux.CurrentRoute(r).GetPathTemplate()

	log.Debugf("Request Path matched: %v", path)

	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorf("Error reading request Body %v for path %v", r, path)
		returnHTTPError(w, r, http.StatusBadRequest, fmt.Sprintf("Error reading json request body, err: %v", err))
		return
	}

	var jsonInput map[string]interface{}
	if len(bodyBytes) > 0 {
		err = json.Unmarshal(bodyBytes, &jsonInput)
		if err != nil {
			log.Errorf("Error unmarshalling json request body: %v", err)
			returnHTTPError(w, r, http.StatusBadRequest, fmt.Sprintf("Error reading json request body: %v", err))
			return
		}
	}

	headerMap := make(map[string][]string)
	for key, value := range r.Header {
		headerMap[key] = value
	}

	api := r.URL.Path

	inputBody, inputHeaders, destination, proxyErr := manager.ProcessPreFilters(path, api, jsonInput, headerMap)
	if proxyErr.Status != "" {
		//error from some filter
		log.Debugf("Error from proxy filter %v", proxyErr)
		writeError(w, proxyErr)
		return
	}

	jsonStr, err := json.Marshal(inputBody)
	r.Body = ioutil.NopCloser(bytes.NewReader(jsonStr))
	r.ContentLength = int64(len(jsonStr))

	for key, value := range inputHeaders {
		for _, singleVal := range value {
			r.Header.Add(key, singleVal)
		}
	}

	destProxy, err := newProxy(destination)
	if err != nil {
		log.Errorf("Error creating a reverse proxy for destination %v", destination)
		returnHTTPError(w, r, http.StatusInternalServerError, fmt.Sprintf("Error creating a reverse proxy for destination %v", destination))
		return
	}
	destProxy.reverseProxy.ServeHTTP(w, r)
}

func handleNotFoundRequest(w http.ResponseWriter, r *http.Request) {
	destProxy, err := newProxy(manager.DefaultDestination)
	if err != nil {
		log.Errorf("Error creating a reverse proxy for destination %v", manager.DefaultDestination)
		returnHTTPError(w, r, http.StatusInternalServerError, fmt.Sprintf("Error creating a reverse proxy for destination %v", manager.DefaultDestination))
		return
	}
	destProxy.reverseProxy.ServeHTTP(w, r)
}

func reload(w http.ResponseWriter, r *http.Request) {
	log.Info("Reload proxy config")
	err := manager.Reload()
	if err != nil {
		//failed to reload the config from the config.json
		log.Debugf("Reload failed with error %v", err)
		returnHTTPError(w, r, http.StatusInternalServerError, "Failed to reload the proxy config")
		return
	}
	filterHandler.filterRouter = newFilterRouter(manager.ConfigFields)
}

//Proxy is our ReverseProxy object
type Proxy struct {
	// target url of reverse proxy
	target       *url.URL
	reverseProxy *httputil.ReverseProxy
}

func newProxy(target string) (*Proxy, error) {
	url, err := url.Parse(target)
	if err != nil {
		log.Errorf("Error reading destination URL %v", target)
		return nil, err
	}
	newProxy := httputil.NewSingleHostReverseProxy(url)
	newProxy.FlushInterval = time.Millisecond * 100
	return &Proxy{target: url, reverseProxy: newProxy}, nil
}

func returnHTTPError(w http.ResponseWriter, r *http.Request, httpStatus int, errorMessage string) {
	svcError := model.ProxyError{
		Status:  strconv.Itoa(httpStatus),
		Message: errorMessage,
	}
	writeError(w, svcError)
}

func writeError(w http.ResponseWriter, svcError model.ProxyError) {
	status, err := strconv.Atoi(svcError.Status)
	if err != nil {
		log.Errorf("Error writing error response %v", err)
		w.Write([]byte(svcError.Message))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	jsonStr, err := json.Marshal(svcError)
	if err != nil {
		log.Errorf("Error writing error response %v", err)
		w.Write([]byte(svcError.Message))
		return
	}
	w.Write([]byte(jsonStr))
}
