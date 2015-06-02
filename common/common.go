package common

import (
	"fmt"
)

const (
	MessageFormat    = "%s||%s||%s"
	MessageSeparator = "||"
)

type MessageType string

const (
	Connect MessageType = "0"
	Body    MessageType = "1"
	Close   MessageType = "2"
)

func FormatMessage(msgKey string, messageType MessageType, body string) string {
	return fmt.Sprintf(MessageFormat, msgKey, messageType, body)
}
