package test_utils

import (
	"io/ioutil"

	log "github.com/Sirupsen/logrus"
	jwt "github.com/dgrijalva/jwt-go"
)

func CreateTokenWithPayload(payload map[string]interface{}, privateKey interface{}) string {
	token := jwt.New(jwt.GetSigningMethod("RS256"))
	token.Claims = payload
	signed, err := token.SignedString(privateKey)
	if err != nil {
		log.Fatal("Failed to parse private key.", err)
	}
	return signed
}

func CreateToken(hostUuid string, privateKey interface{}) string {
	token := jwt.New(jwt.GetSigningMethod("RS256"))
	token.Claims["hostUuid"] = hostUuid
	signed, err := token.SignedString(privateKey)
	if err != nil {
		log.Fatal("Failed to parse private key.", err)
	}
	return signed
}

func CreateBackendToken(reportedUuid string, privateKey interface{}) string {
	token := jwt.New(jwt.GetSigningMethod("RS256"))
	token.Claims["reportedUuid"] = reportedUuid
	signed, err := token.SignedString(privateKey)
	if err != nil {
		log.Fatal("Failed to parse private key.", err)
	}
	return signed
}

func ParseTestPrivateKey() interface{} {
	keyBytes, err := ioutil.ReadFile("../test_utils/private.pem")
	if err != nil {
		log.Fatal("Failed to parse private key.", err)
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		log.Fatal("Failed to parse private key.", err)
	}

	return privateKey
}
