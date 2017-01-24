package util

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	log "github.com/Sirupsen/logrus"
	"github.com/pborman/uuid"
)

func SignString(stringToSign []byte, sharedSecret []byte) string {
	h := hmac.New(sha512.New, sharedSecret)
	h.Write(stringToSign)

	signature := h.Sum(nil)
	encodedSignature := base64.URLEncoding.EncodeToString(signature)

	log.Debugf("Signature generated: %v", encodedSignature)

	return encodedSignature
}

func GenerateUUID() string {
	newUUID := uuid.NewUUID()
	log.Debugf("uuid generated: %v", newUUID)
	time, _ := newUUID.Time()
	log.Debugf("time generated: %v", time)
	return newUUID.String()
}
