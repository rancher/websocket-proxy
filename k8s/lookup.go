package k8s

import (
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"encoding/json"

	"github.com/pkg/errors"
	"github.com/rancher/go-rancher/v3"
)

type Lookup struct {
	httpClient           http.Client
	accessKey, secretKey string
	clusterURL           string
}

func NewLookup(clusterURL, accessKey, secretKey string) *Lookup {
	return &Lookup{
		httpClient: http.Client{
			Timeout: 5 * time.Second,
		},
		accessKey:  accessKey,
		secretKey:  secretKey,
		clusterURL: clusterURL,
	}
}

func (c *Lookup) Lookup(input *http.Request) (*client.Cluster, bool, error) {
	clusterID := getClusterID(input)
	if clusterID == "" {
		return nil, false, nil
	}

	req, err := http.NewRequest("GET", c.clusterURL+"/"+clusterID, nil)
	if err != nil {
		return nil, false, err
	}

	req.Header.Set("Authorization", getAuthorizationHeader(req))

	cookie := getTokenCookie(input)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer close(resp)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c, err := parseCluster(resp)
		return c, true, err
	}

	auth := input.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, false, nil
	}

	auth = strings.TrimPrefix(auth, "Bearer ")

	req.Header.Del("Authorization")
	req.SetBasicAuth(c.accessKey, c.secretKey)
	resp2, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer close(resp2)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, false, nil
	}

	cluster, err := parseCluster(resp2)
	if err != nil || cluster.K8sClientConfig == nil || cluster.K8sClientConfig.BearerToken != auth {
		return nil, false, err
	}

	return cluster, false, nil
}

func parseCluster(resp *http.Response) (*client.Cluster, error) {
	cluster := &client.Cluster{}
	if err := json.NewDecoder(resp.Body).Decode(cluster); err != nil {
		return nil, errors.Wrap(err, "Parsing clusters response")
	}
	return cluster, nil
}

func getClusterID(req *http.Request) string {
	parts := strings.Split(req.URL.Path, "/")
	if len(parts) > 3 && strings.HasPrefix(parts[2], "cluster") {
		return parts[3]
	}

	return ""
}

func getAuthorizationHeader(req *http.Request) string {
	return req.Header.Get("Authorization")
}

func getTokenCookie(req *http.Request) *http.Cookie {
	for _, cookie := range req.Cookies() {
		if cookie.Name == "token" {
			return cookie
		}
	}

	return nil
}

func close(resp *http.Response) error {
	io.Copy(ioutil.Discard, resp.Body)
	return resp.Body.Close()
}
