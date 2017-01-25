package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"

	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/filters"
	"github.com/rancher/websocket-proxy/proxy/apifilterproxy/model"
)

const (
	name = "authTokenValidator"
)

//AuthorizeData is for the JSON output
type AuthorizeData struct {
	Message string `json:"message,omitempty"`
}

//MessageData is for the JSON output
type MessageData struct {
	Data []interface{} `json:"data,omitempty"`
}

type TokenValidationFilter struct {
	rancherURL string
}

func (*TokenValidationFilter) GetName() string {
	return name
}

func NewFilter() (filters.APIFilter, error) {
	tokenFilter := &TokenValidationFilter{}

	if len(os.Getenv("PROXY_CATTLE_ADDRESS")) == 0 {
		log.Infof("PROXY_CATTLE_ADDRESS is not set, skipping init of %v API filter", name)
		return tokenFilter, fmt.Errorf("PROXY_CATTLE_ADDRESS is not set, skipping init of %v API filter", name)
	}

	tokenFilter.rancherURL = "http://" + os.Getenv("PROXY_CATTLE_ADDRESS")

	log.Infof("Configured %s API filter", tokenFilter.GetName())
	return tokenFilter, nil
}

func (f *TokenValidationFilter) ProcessFilter(filter model.FilterData, input model.APIRequestData) (model.APIRequestData, error) {
	output := model.APIRequestData{}

	envid := input.EnvID

	log.Debugf("Request => %v", input)

	var cookie []string
	if input.Headers["Cookie"] == nil {
		output.Status = http.StatusOK
		log.Debug("No Cookie found in request")
		return output, nil

	}
	if len(input.Headers["Cookie"]) >= 1 {
		cookie = input.Headers["Cookie"]
	} else {
		output.Status = http.StatusOK
		log.Debug("No Cookie found in request")
		return output, nil
	}
	var cookieString string
	if len(cookie) >= 1 {
		for i := range cookie {
			if strings.Contains(cookie[i], "token") {
				cookieString = cookie[i]
			}
		}
	} else {
		output.Status = http.StatusOK
		log.Debug("No token found in cookie")
		return output, nil
	}

	tokens := strings.Split(cookieString, ";")
	tokenValue := ""
	if len(tokens) >= 1 {
		for i := range tokens {
			if strings.Contains(tokens[i], "token") {
				if len(strings.Split(tokens[i], "=")) > 1 {
					tokenValue = strings.Split(tokens[i], "=")[1]
				}
			}

		}
	} else {
		output.Status = http.StatusOK
		log.Debug("No token found in cookie")
		return output, nil
	}
	if tokenValue == "" {
		output.Status = http.StatusOK
		log.Debug("No token found in cookie")
		return output, nil
	}

	//check if the token value is empty or not
	if tokenValue != "" {
		log.Debugf("token:" + tokenValue)
		log.Debugf("envid:" + envid)
		projectID, accountID := "", ""
		var err error
		if envid != "" {
			projectID, accountID, err = getAccountAndProject(f.rancherURL, envid, tokenValue)
			if err != nil {
				output.Status = http.StatusNotFound
				return output, fmt.Errorf("Error getting the accountid and projectid: %v", err)
			}
			if accountID == "Unauthorized" {
				output.Status = http.StatusUnauthorized
				return output, fmt.Errorf("Token is expired or unauthorized")
			}

			if accountID == "" {
				output.Status = http.StatusForbidden
				return output, fmt.Errorf("Token is forbidden to access the projectid")
			}

		} else {
			accountID, err = getAccountID(f.rancherURL, tokenValue)
			if err != nil {
				output.Status = http.StatusNotFound
				return output, fmt.Errorf("Error getting the accountid : %v", err)
			}
			if accountID == "Unauthorized" {
				output.Status = http.StatusUnauthorized
				return output, fmt.Errorf("Token is expired or unauthorized")
			}

		}

		//construct the responseBody
		var headerBody = make(map[string][]string)

		requestHeader := input.Headers
		for k, v := range requestHeader {
			headerBody[k] = v
		}

		headerBody["X-API-Account-Id"] = []string{accountID}
		if projectID != "" {
			headerBody["X-API-Project-Id"] = []string{projectID}
		}

		output.Headers = headerBody
		output.Status = http.StatusOK

		log.Debugf("Response <= %v", output)
	}
	return output, nil
}

//get the projectID and accountID from rancher API
func getAccountAndProject(host string, envid string, token string) (string, string, error) {

	client := &http.Client{}
	requestURL := host + "/v2-beta/projects/" + envid + "/accounts"
	log.Debugf("requestURL %v", requestURL)
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("Cannot connect to the rancher server. Please check the rancher server URL")
	}
	cookie := http.Cookie{Name: "token", Value: token}
	req.AddCookie(&cookie)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("Cannot connect to the rancher server. Please check the rancher server URL")
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("Cannot read the reponse body")
	}

	err = checkIfAuthorized(bodyText)

	if err != nil {
		return "Unauthorized", "Unauthorized", err
	}

	projectid := resp.Header.Get("X-Api-Account-Id")
	userid := resp.Header.Get("X-Api-User-Id")
	if projectid == "" || userid == "" {
		err := errors.New("Token is forbidden to access the projectid")
		return "Forbidden", "Forbidden", err

	}
	if projectid == userid {
		err := errors.New("Cannot validate project id")
		return "", "", err
	}

	log.Debugf("projectid: %v, userid: %v", projectid, userid)

	return projectid, userid, nil
}

//get the accountID from rancher API
func getAccountID(host string, token string) (string, error) {

	client := &http.Client{}
	requestURL := host + "/v2-beta/accounts"
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("Cannot get the account api [%v]", err)
	}
	cookie := http.Cookie{Name: "token", Value: token}
	req.AddCookie(&cookie)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Cannot setup HTTP client [%v]", err)
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Cannot read data from response body:[%v]", err)
	}
	err = checkIfAuthorized(bodyText)

	if err != nil {
		return "Unauthorized", err
	}

	messageData := MessageData{}
	err = json.Unmarshal(bodyText, &messageData)
	if err != nil {
		err := errors.New("Cannot extract accounts JSON")
		return "", err
	}
	result := ""
	//get id from the data
	for i := 0; i < len(messageData.Data); i++ {

		idData, suc := messageData.Data[i].(map[string]interface{})
		if suc {
			if idData["id"] == "" || idData["id"] == nil {
				return "", fmt.Errorf("Cannot extract user id")
			}
			id, suc := idData["id"].(string)
			if idData["kind"] == "" || idData["kind"] == nil {
				return "", fmt.Errorf("Cannot extract user kind")
			}
			kind, namesuc := idData["kind"].(string)
			if suc && namesuc {
				//if the token belongs to admin, only return the admin token
				if kind == "admin" {
					return id, nil
				}
			} else {
				err := errors.New("Cannot extract accounts from account api")
				return "", err
			}
			result = id

		}

	}

	return result, nil

}

//check the AuthorizeData
func checkIfAuthorized(bodyText []byte) error {

	authMessage := AuthorizeData{}
	err := json.Unmarshal(bodyText, &authMessage)
	if err != nil {
		return fmt.Errorf("Cannot read the reponse body")
	}
	if authMessage.Message == "Unauthorized" {
		err = errors.New("Token is expired or unauthorized")
		return err
	}
	return nil
}
