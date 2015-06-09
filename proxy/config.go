package proxy

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"io/ioutil"

	log "github.com/Sirupsen/logrus"
	"github.com/rakyll/globalconf"
)

type Config struct {
	PublicKey  interface{}
	ListenAddr string
	CattleAddr string
}

func GetConfig() (*Config, error) {
	c := &Config{}
	var keyFile string
	flag.StringVar(&keyFile, "jwt-public-key-file", "", "Location of the public-key used to validate JWTs.")
	flag.StringVar(&c.ListenAddr, "listen-address", "localhost:8080", "The tcp address to listen on.")
	flag.StringVar(&c.CattleAddr, "cattle-address", "localhost:8081", "The tcp address to forward cattle API requests to.")

	confOptions := &globalconf.Options{
		EnvPrefix: "PROXY_",
	}

	conf, err := globalconf.NewWithOptions(confOptions)

	if err != nil {
		return nil, err
	}

	conf.ParseAll()

	if parsedKey, err := ParsePublicKey(keyFile); err != nil {
		log.WithField("error", err).Error("Couldn't parse public key.")
		return nil, err
	} else {
		c.PublicKey = parsedKey
	}
	return c, nil
}

func ParsePublicKey(keyFile string) (interface{}, error) {
	keyBytes, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(keyBytes)
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return pubKey, nil
}
