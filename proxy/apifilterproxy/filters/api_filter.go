package filters

import (
	"errors"
	log "github.com/Sirupsen/logrus"

	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/model"
)

type APIFilter interface {
	GetName() string
	ProcessFilter(filter model.FilterData, input model.APIRequestData) (model.APIRequestData, error)
}

var (
	apiFilters map[string]APIFilter
)

func GetAPIFilter(name string) APIFilter {
	if filter, ok := apiFilters[name]; ok {
		return filter
	}
	return apiFilters["http"]
}

func RegisterAPIFilter(name string, filter APIFilter) error {
	if apiFilters == nil {
		apiFilters = make(map[string]APIFilter)
	}
	if _, exists := apiFilters[name]; exists {
		log.Errorf("apiFilter %v already registered", name)
		return errors.New("Already Registered")
	}
	apiFilters[name] = filter
	return nil
}
