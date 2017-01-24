package auth

import (
	"encoding/json"
	"errors"
	log "github.com/Sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

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

func init() {
	if len(os.Getenv("PROXY_CATTLE_ADDRESS")) == 0 {
		log.Infof("PROXY_CATTLE_ADDRESS is not set, skipping init of %v API filter", name)
		return
	}
	tokenFilter := &TokenValidationFilter{}
	tokenFilter.rancherURL = "http://" + os.Getenv("PROXY_CATTLE_ADDRESS")

	if err := filters.RegisterAPIFilter(name, tokenFilter); err != nil {
		log.Fatalf("Could not register %s filter", name)
	}

	log.Infof("Configured %s API filter", tokenFilter.GetName())

}

type TokenValidationFilter struct {
	rancherURL string
}

func (*TokenValidationFilter) GetName() string {
	return name
}

func (f *TokenValidationFilter) ProcessFilter(filter model.FilterData, input model.APIRequestData) (model.APIRequestData, error) {
	output := model.APIRequestData{}

	envid := input.EnvID

	log.Debugf("Request => %v", input)

	var cookie []string
	if len(input.Headers["Cookie"]) >= 1 {
		cookie = input.Headers["Cookie"]
	} else {
		log.Debugf("No Cookie found.")
		output.Status = http.StatusOK
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
		log.Debugf("No token found in cookie.")
		output.Status = http.StatusOK
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
		log.Errorf("No token found")
		output.Status = http.StatusOK
		return output, nil
	}
	if tokenValue == "" {
		log.Errorf("No token found")
		output.Status = http.StatusOK
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
			if accountID == "Unauthorized" {
				log.Errorf("Unauthorized")
				output.Status = http.StatusUnauthorized
				return output, nil
			}

			if accountID == "Forbidden" {
				log.Errorf("Forbidden")
				output.Status = http.StatusForbidden
				return output, nil
			}
			if err != nil {
				log.Errorf("Error getting the accountid and projectid: %v", err)
				output.Status = http.StatusNotFound
				return output, nil
			}
		} else {
			accountID, err = getAccountID(f.rancherURL, tokenValue)
			if accountID == "Unauthorized" {
				log.Errorf("Unauthorized")
				output.Status = http.StatusUnauthorized
				return output, nil
			}
			if err != nil {
				log.Errorf("Error getting the accountid : %v", err)
				output.Status = http.StatusNotFound
				return output, nil
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
		log.Errorf("Cannot connect to the rancher server. Please check the rancher server URL")
		return "", "", err
	}
	cookie := http.Cookie{Name: "token", Value: token}
	req.AddCookie(&cookie)
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Cannot connect to the rancher server. Please check the rancher server URL")
		return "", "", err
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Cannot read the reponse body")
		return "", "", err
	}
	authMessage := AuthorizeData{}
	err = json.Unmarshal(bodyText, &authMessage)
	if err != nil {
		log.Errorf("Cannot extract authorization JSON")
		return "", "", err
	}
	if authMessage.Message == "Unauthorized" {
		log.Errorf("Unauthorized token")
		err := errors.New("Unauthorized token")
		return "Unauthorized", "Unauthorized", err
	}

	projectid := resp.Header.Get("X-Api-Account-Id")
	userid := resp.Header.Get("X-Api-User-Id")
	if projectid == "" || userid == "" {
		log.Errorf("Cannot get projectid or userid")
		err := errors.New("Forbidden")
		return "Forbidden", "Forbidden", err

	}
	if projectid == userid {
		log.Errorf("Cannot validate project id")
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
	cookie := http.Cookie{Name: "token", Value: token}
	req.AddCookie(&cookie)
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Cannot connect to the rancher server. Please check the rancher server URL")
		return "", err
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	authMessage := AuthorizeData{}
	err = json.Unmarshal(bodyText, &authMessage)
	log.Debugf("bodyText %v", string(bodyText))
	if err != nil {
		log.Errorf("Unmarshal token fail")
		return "", err
	}
	if authMessage.Message == "Unauthorized" {
		log.Errorf("Unauthorized token")
		err := errors.New("Unauthorized token")
		return "Unauthorized", err
	}
	messageData := MessageData{}
	err = json.Unmarshal(bodyText, &messageData)
	if err != nil {
		log.Errorf("Cannot extract accounts JSON")
		err := errors.New("Cannot extract accounts JSON")
		return "", err
	}
	result := ""
	//get id from the data
	for i := 0; i < len(messageData.Data); i++ {

		idData, suc := messageData.Data[i].(map[string]interface{})
		if suc {
			id, suc := idData["id"].(string)
			kind, namesuc := idData["kind"].(string)
			if suc && namesuc {
				//if the token belongs to admin, only return the admin token
				if kind == "admin" {
					return id, nil
				}
			} else {
				log.Errorf("Cannot extract accounts JSON")
				err := errors.New("Cannot extract accounts JSON")
				return "", err
			}
			result = id

		}

	}

	return result, nil

}
