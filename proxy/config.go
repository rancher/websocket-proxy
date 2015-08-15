package proxy

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/rakyll/globalconf"
)

type Config struct {
	PublicKey            interface{}
	ListenAddr           string
	CattleAddr           string
	ParentPid            int
	ProxyProtoHttpsPorts map[int]bool
}

func GetConfig() (*Config, error) {
	c := &Config{}
	var keyFile string
	var keyContents string
	var proxyProtoHttpsPorts string
	flag.StringVar(&keyFile, "jwt-public-key-file", "", "Location of the public-key used to validate JWTs.")
	flag.StringVar(&keyContents, "jwt-public-key-contents", "", "An alternative to jwt-public-key-file. The contents of the key.")
	flag.StringVar(&c.ListenAddr, "listen-address", ":8080", "The tcp address to listen on.")
	flag.StringVar(&c.CattleAddr, "cattle-address", "", "The tcp address to forward cattle API requests to. Will not proxy to cattle api if this option is not provied.")
	flag.IntVar(&c.ParentPid, "parent-pid", 0, "If provided, this process will exit when the specified parent process stops running.")
	flag.StringVar(&proxyProtoHttpsPorts, "https-proxy-protocol-ports", "", "If proxy protocol is used, a list of proxy ports that will allow us to recognize that the connection was over https.")

	confOptions := &globalconf.Options{
		EnvPrefix: "PROXY_",
	}

	conf, err := globalconf.NewWithOptions(confOptions)

	if err != nil {
		return nil, err
	}

	conf.ParseAll()

	if keyFile != "" && keyContents != "" {
		return nil, fmt.Errorf("Can't specify both jwt-public-key-file and jwt-public-key-contents")
	}
	var parsedKey interface{}
	var parseErr error
	if keyFile != "" {
		parsedKey, parseErr = ParsePublicKey(keyFile)
	} else if keyContents != "" {
		parsedKey, parseErr = ParsePublicKeyFromMemory(keyContents)
	} else {
		parseErr = fmt.Errorf("Must specify one of jwt-public-key-file and jwt-public-key-contents")
	}
	if parseErr != nil {
		return nil, parseErr
	}

	c.PublicKey = parsedKey

	portMap := make(map[int]bool)
	ports := strings.Split(proxyProtoHttpsPorts, ",")
	for _, port := range ports {
		if p, err := strconv.Atoi(port); err == nil {
			portMap[p] = true
		}
	}
	c.ProxyProtoHttpsPorts = portMap

	return c, nil
}

func ParsePublicKey(keyFile string) (interface{}, error) {
	keyBytes, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}

	return publicKeyDecode(keyBytes)
}

func ParsePublicKeyFromMemory(keyFileContents string) (interface{}, error) {
	return publicKeyDecode([]byte(keyFileContents))
}

func publicKeyDecode(keyBytes []byte) (interface{}, error) {
	block, _ := pem.Decode(keyBytes)
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return pubKey, nil
}
