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

	"github.com/rancher/websocket-proxy/proxy/apiinterceptor/filters"
	"github.com/rancher/websocket-proxy/proxy/apiinterceptor/model"
)

const (
	interceptorType = "authTokenValidator"
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

func (*TokenValidationFilter) GetType() string {
	return interceptorType
}

func NewFilter() (filters.APIFilter, error) {
	tokenFilter := &TokenValidationFilter{}

	var addr string
	if os.Getenv("PROXY_CATTLE_ADDRESS") == "" {
		log.Infof("PROXY_CATTLE_ADDRESS is not set, defaulting to localhost:8081")
		addr = "localhost:8081"
	} else {
		addr = os.Getenv("PROXY_CATTLE_ADDRESS")
	}

	tokenFilter.rancherURL = "http://" + addr

	log.Infof("Configured %s API filter", tokenFilter.GetType())
	return tokenFilter, nil
}

func (f *TokenValidationFilter) ProcessFilter(filter model.FilterData, input model.APIRequestData) (model.APIRequestData, error) {
	output := model.APIRequestData{}

	envid := input.EnvID

	log.Debugf("Request => %v", input)

	var cookie, authHeader []string
	if input.Headers["Cookie"] == nil && input.Headers["Authorization"] == nil {
		output.Status = http.StatusOK
		log.Debug("No Cookie or Auth headers found in request")
		return output, nil

	}
	usingTokenCookie := false

	if len(input.Headers["Cookie"]) >= 1 {
		cookie = input.Headers["Cookie"]
		usingTokenCookie = true
	} else if len(input.Headers["Authorization"]) >= 1 {
		authHeader = input.Headers["Authorization"]
	} else {
		output.Status = http.StatusOK
		log.Debug("No Cookie or Auth headers found in request")
		return output, nil
	}

	var cookieString, tokenValue string
	if usingTokenCookie {
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
	}

	//check if the token value is empty or not
	if tokenValue != "" || len(authHeader) >= 1 {
		log.Debugf("token:" + tokenValue)
		log.Debugf("envid:" + envid)

		projectID, accountID, kind, name := "", "", "", ""
		var err error
		if envid != "" {
			projectID, accountID, kind, name, err = getAccountAndProject(f.rancherURL, envid, tokenValue, authHeader)
			if err != nil {
				output.Status = http.StatusNotFound
				return output, fmt.Errorf("error getting the accountid and projectid: %v", err)
			}
			if accountID == "Unauthorized" {
				output.Status = http.StatusUnauthorized
				return output, fmt.Errorf("token or Auth keys expired or unauthorized")
			}

			if accountID == "" {
				output.Status = http.StatusForbidden
				return output, fmt.Errorf("token or Auth keys forbidden to access the projectid")
			}

		} else {
			accountID, kind, name, err = getAccountID(f.rancherURL, tokenValue, authHeader)
			if err != nil {
				output.Status = http.StatusNotFound
				return output, fmt.Errorf("error getting the accountid : %v", err)
			}
			if accountID == "Unauthorized" {
				output.Status = http.StatusUnauthorized
				return output, fmt.Errorf("token or Auth keys  expired or unauthorized")
			}

		}

		//construct the responseBody
		var headerBody = make(map[string][]string)

		requestHeader := input.Headers
		for k, v := range requestHeader {
			headerBody[k] = v
		}

		headerBody["X-API-Account-Id"] = []string{accountID}
		headerBody["X-API-Account-Kind"] = []string{kind}
		if projectID != "" {
			headerBody["X-API-Project-Id"] = []string{projectID}
		}
		if name != "" {
			headerBody["X-API-Account-Name"] = []string{name}
		}

		output.Headers = headerBody
		output.Status = http.StatusOK

		log.Debugf("Response <= %v", output)
	}

	return output, nil
}

//get the projectID and accountID from rancher API
func getAccountAndProject(host string, envid string, token string, authHeaders []string) (string, string, string, string, error) {
	client := &http.Client{}
	requestURL := host + "/v2-beta/projects/" + envid + "/accounts"
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return "", "", "", "", fmt.Errorf("can not connect to the rancher server. Please check the rancher server URL")
	}
	if token != "" {
		cookie := http.Cookie{Name: "token", Value: token}
		req.AddCookie(&cookie)
	} else {
		req.Header["Authorization"] = authHeaders
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", "", fmt.Errorf("can not connect to the rancher server. Please check the rancher server URL")
	}
	defer resp.Body.Close()

	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", "", fmt.Errorf("can not read the reponse body")
	}

	err = checkIfAuthorized(bodyText)

	if err != nil {
		return "Unauthorized", "Unauthorized", "Unauthorized", "Unauthorized", err
	}

	projectid := resp.Header.Get("X-Api-Account-Id")
	userid := resp.Header.Get("X-Api-User-Id")
	kind := resp.Header.Get("X-Api-Account-Kind")
	name := resp.Header.Get("X-Api-Account-Name")
	if projectid == "" || userid == "" {
		err := errors.New("token is forbidden to access the projectid")
		return "Forbidden", "Forbidden", "Forbidden", "Forbidden", err

	}
	log.Debugf("projectid: %v, userid: %v, kind %v, name %v", projectid, userid, kind, name)

	return projectid, userid, kind, name, nil
}

//get the accountID from rancher API
func getAccountID(host string, token string, authHeaders []string) (string, string, string, error) {
	client := &http.Client{}
	requestURL := host + "/v2-beta/accounts"
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("can not get the account api [%v]", err)
	}

	if token != "" {
		cookie := http.Cookie{Name: "token", Value: token}
		req.AddCookie(&cookie)
	} else {
		req.Header["Authorization"] = authHeaders
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("can not setup HTTP client [%v]", err)
	}
	defer resp.Body.Close()

	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("can not read data from response body:[%v]", err)
	}
	err = checkIfAuthorized(bodyText)

	if err != nil {
		return "Unauthorized", "Unauthorized", "Unauthorized", err
	}

	messageData := MessageData{}
	err = json.Unmarshal(bodyText, &messageData)
	if err != nil {
		err := errors.New("can not extract accounts JSON")
		return "", "", "", err
	}
	result := ""
	accKind := ""
	accName := ""
	//get id from the data
	for i := 0; i < len(messageData.Data); i++ {

		idData, suc := messageData.Data[i].(map[string]interface{})
		if suc {
			if idData["id"] == "" || idData["id"] == nil {
				return "", "", "", fmt.Errorf("can not extract user id")
			}
			id, suc := idData["id"].(string)
			if idData["kind"] == "" || idData["kind"] == nil {
				return "", "", "", fmt.Errorf("can not extract user kind")
			}
			kind, namesuc := idData["kind"].(string)
			name, _ := idData["name"].(string)
			if suc && namesuc {
				//if the token belongs to admin, only return the admin token
				if kind == "admin" {
					return id, kind, name, nil
				}
			} else {
				err := errors.New("can not extract accounts from account api")
				return "", "", "", err
			}
			result = id
			accKind = kind
			accName = name
		}

	}

	return result, accKind, accName, nil

}

//check the AuthorizeData
func checkIfAuthorized(bodyText []byte) error {

	authMessage := AuthorizeData{}
	err := json.Unmarshal(bodyText, &authMessage)
	if err != nil {
		return fmt.Errorf("can not read the reponse body")
	}
	if authMessage.Message == "Unauthorized" {
		err = errors.New("token is expired or unauthorized")
		return err
	}
	return nil
}
