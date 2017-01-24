package http

import (
	"bytes"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"io/ioutil"
	"net/http"

	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/filters"
	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/model"
	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/util"
)

const (
	name = "http"
)

func init() {
	httpFilter := &GenericHTTPFilter{}
	if err := filters.RegisterAPIFilter(name, httpFilter); err != nil {
		log.Fatalf("Could not register %s filter", name)
	}

	log.Infof("Configured %s API filter", httpFilter.GetName())

}

type GenericHTTPFilter struct {
}

func (*GenericHTTPFilter) GetName() string {
	return name
}

func (f *GenericHTTPFilter) ProcessFilter(filter model.FilterData, input model.APIRequestData) (model.APIRequestData, error) {
	output := model.APIRequestData{}
	bodyContent, err := json.Marshal(input)
	if err != nil {
		return output, err
	}

	log.Debugf("Request => " + string(bodyContent))

	client := &http.Client{}
	req, err := http.NewRequest("POST", filter.Endpoint, bytes.NewBuffer(bodyContent))
	if err != nil {
		return output, err
	}
	//sign the body
	if filter.SecretToken != "" {
		signature := util.SignString(bodyContent, []byte(filter.SecretToken))
		req.Header.Set(model.SignatureHeader, signature)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", string(len(bodyContent)))

	resp, err := client.Do(req)
	if err != nil {
		return output, err
	}
	log.Debugf("Response Status <= " + resp.Status)
	defer resp.Body.Close()

	byteContent, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return output, err
	}

	log.Debugf("Response <= " + string(byteContent))
	json.Unmarshal(byteContent, &output)
	output.Status = resp.StatusCode

	return output, nil
}
