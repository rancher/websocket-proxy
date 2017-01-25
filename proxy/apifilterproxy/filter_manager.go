package apifilterproxy

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/pborman/uuid"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/filters"
	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/filters/auth"
	httpfilter "github.com/rancher/websocket-proxy/proxy/apifilterproxy/filters/http"
	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/model"
)

//Destination defines the properties of a Destination
type Destination struct {
	DestinationURL string   `json:"destinationURL"`
	Paths          []string `json:"paths"`
}

//ConfigFileFields stores filter config
type ConfigFileFields struct {
	Prefilters   []model.FilterData
	Destinations []Destination
}

//FilterManager encapsulates the framework that configures the apiproxy and processes the prefilters for a given path
type FilterManager struct {
	configFile   string
	cattleURL    string
	configFields ConfigFileFields
	apiFilters   map[string]filters.APIFilter
	//pathPreFilters is the map storing path -> prefilters[]
	pathPreFilters map[string][]model.FilterData
	//pathDestinations is the map storing path -> prefilters[]
	pathDestinations map[string]Destination
	//lock
	filterConfigMu *sync.RWMutex
}

//InitManager sets the parameters necessary and initializes FilterManager
func InitManager(filterConfigFile string, cattleAddr string) (*FilterManager, error) {
	filterManager := &FilterManager{
		filterConfigMu: &sync.RWMutex{},
		configFields:   ConfigFileFields{},
	}
	filterManager.LoadAPIFilters()
	filterManager.configFile = filterConfigFile
	filterManager.pathPreFilters = make(map[string][]model.FilterData)
	filterManager.pathDestinations = make(map[string]Destination)

	cattleURL := "http://" + cattleAddr
	filterManager.cattleURL = cattleURL
	if len(cattleAddr) == 0 {
		log.Warnf("No CattleAddr set to forward the requests to Cattle")
		return filterManager, fmt.Errorf("No CattleAddr set in proxy config to forward the requests to Cattle")
	}

	if filterConfigFile == "" {
		log.Warnf("No path found to the APIfilter config.json file")
		return filterManager, nil
	}
	err := filterManager.Reload()
	if err != nil {
		log.Warnf("Disabling API filter proxy, failed to load the API filter proxy config with error: %v", err)
		return filterManager, nil
	}

	log.Infof("Configured the API filter proxy with config %v", filterConfigFile)
	return filterManager, nil
}

//NewFilterRouter initializes a mux router with paths defined in filter config
func (f *FilterManager) NewFilterRouter() *mux.Router {
	// API framework routes
	router := mux.NewRouter().StrictSlash(false)

	f.filterConfigMu.RLock()
	for _, filter := range f.configFields.Prefilters {
		//build router paths
		for _, path := range filter.Paths {
			for _, method := range filter.Methods {
				log.Debugf("Adding route: %v %v", strings.ToUpper(method), path)
				router.Methods(strings.ToUpper(method)).Path(path).HandlerFunc(http.HandlerFunc(HandleRequest))
			}
		}
	}
	f.filterConfigMu.RUnlock()

	router.Methods("POST").Path("/v1-api-filter-proxy/reload").HandlerFunc(http.HandlerFunc(Reload))
	router.NotFoundHandler = http.HandlerFunc(HandleNotFoundRequest)

	return router
}

func (f *FilterManager) Reload() error {
	if f.configFile != "" {
		var configContent []byte
		if _, err := os.Stat(f.configFile); os.IsNotExist(err) {
			//file does not exist, treating it as empty config, since cattle deletes the file when config is set to empty
			log.Debugf("config.json file not found %v", f.configFile)
			configContent = []byte("{}")
		} else {
			var err error
			configContent, err = ioutil.ReadFile(f.configFile)
			if err != nil {
				log.Debugf("Error reading config.json file at path %v", f.configFile)
				return fmt.Errorf("Error reading config.json file at path %v", f.configFile)
			}
		}
		updatedConfigFields := ConfigFileFields{}
		err := json.Unmarshal(configContent, &updatedConfigFields)
		if err != nil {
			log.Debugf("config.json data format invalid, error : %v\n", err)
			return fmt.Errorf("Proxy config.json data format invalid, error : %v", err)
		}

		updatedPathPreFilters := make(map[string][]model.FilterData)
		for _, filter := range updatedConfigFields.Prefilters {
			//build the pathPreFilters map
			for _, path := range filter.Paths {
				updatedPathPreFilters[path] = append(updatedPathPreFilters[path], filter)
			}
		}

		updatedPathDestinations := make(map[string]Destination)
		for _, destination := range updatedConfigFields.Destinations {
			//build the pathDestinations map
			for _, path := range destination.Paths {
				updatedPathDestinations[path] = destination
			}
		}

		f.filterConfigMu.Lock()

		f.configFields = updatedConfigFields
		f.pathPreFilters = updatedPathPreFilters
		f.pathDestinations = updatedPathDestinations

		f.filterConfigMu.Unlock()
	}
	return nil
}

func (f *FilterManager) LoadAPIFilters() {
	if f.apiFilters == nil {
		f.apiFilters = make(map[string]filters.APIFilter)
	}

	httpFilter, err := httpfilter.NewFilter()
	if err != nil {
		log.Errorf("Error initalizing APIFilter %v, error %v", httpFilter.GetName(), err)
	} else {
		f.apiFilters[httpFilter.GetName()] = httpFilter
	}

	tokenFilter, err := auth.NewFilter()
	if err != nil {
		log.Errorf("Error initalizing APIFilter %v, error %v", tokenFilter.GetName(), err)
	} else {
		f.apiFilters[tokenFilter.GetName()] = tokenFilter
	}
}

func (f *FilterManager) GetAPIFilter(name string) (filters.APIFilter, error) {
	if filter, ok := f.apiFilters[name]; ok {
		return filter, nil
	}
	return f.apiFilters["http"], fmt.Errorf("APIFilter %v not found", name)
}

func (f *FilterManager) ProcessPreFilters(path string, api string, body map[string]interface{}, headers map[string][]string) (map[string]interface{}, map[string][]string, string, model.ProxyError) {
	//use read lock
	f.filterConfigMu.RLock()

	//copy the filters and destinations
	var prefilters []model.FilterData
	for _, value := range f.pathPreFilters[path] {
		prefilters = append(prefilters, value)
	}

	destination := Destination{}
	matchDestination, ok := f.pathDestinations[path]
	if ok {
		destination.DestinationURL = matchDestination.DestinationURL
	} else {
		destination.DestinationURL = f.cattleURL
	}

	//unlock
	f.filterConfigMu.RUnlock()

	log.Debugf("START -- Processing pre filters for request path %v", path)
	inputBody := body
	inputHeaders := headers
	//add uuid
	UUID := generateUUID()
	//envId
	envID := extractEnvID(api)

	for _, filterData := range prefilters {
		log.Debugf("-- Processing pre filter %v for request path %v --", filterData, path)

		requestData := model.APIRequestData{}
		requestData.Body = inputBody
		requestData.Headers = inputHeaders
		requestData.UUID = UUID
		requestData.APIPath = api
		if envID != "" {
			requestData.EnvID = envID
		}

		apiFilter, err := f.GetAPIFilter(filterData.Name)
		if err != nil {
			log.Errorf("Skipping filter %v, Error loading the filter %v", filterData.Name, err)
			continue
		}
		responseData, err := apiFilter.ProcessFilter(filterData, requestData)
		if err != nil {
			log.Errorf("Error %v processing the filter %v", err, filterData)
			svcErr := model.ProxyError{
				Status:  strconv.Itoa(http.StatusInternalServerError),
				Message: fmt.Sprintf("Error %v processing the filter %v", err, filterData),
			}
			return inputBody, inputHeaders, "", svcErr
		}
		if responseData.Status == 200 {
			if responseData.Body != nil {
				inputBody = responseData.Body
			}
			if responseData.Headers != nil {
				inputHeaders = responseData.Headers
			}
		} else {
			//error
			log.Errorf("Error response %v - %v while processing the filter %v", responseData.Status, responseData.Body, filterData)
			svcErr := model.ProxyError{
				Status:  strconv.Itoa(responseData.Status),
				Message: fmt.Sprintf("Error response while processing the filter name %v, endpoint %v", filterData.Name, filterData.Endpoint),
			}

			return inputBody, inputHeaders, "", svcErr
		}
	}

	//send the final body and headers to destination

	log.Debugf("DONE -- Processing pre filters for request path %v, following to destination %v", path, destination.DestinationURL)

	return inputBody, inputHeaders, destination.DestinationURL, model.ProxyError{}
}

func extractEnvID(requestURL string) string {
	envID := ""
	if strings.Contains(requestURL, "/projects/") {
		parts := strings.Split(requestURL, "/projects/")
		if len(parts) > 1 {
			subParts := strings.Split(parts[1], "/")
			envID = subParts[0]
		}
	}
	return envID
}

func generateUUID() string {
	newUUID := uuid.NewUUID()
	log.Debugf("uuid generated: %v", newUUID)
	time, _ := newUUID.Time()
	log.Debugf("time generated: %v", time)
	return newUUID.String()
}
