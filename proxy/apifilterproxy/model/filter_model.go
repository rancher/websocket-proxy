package model

const SignatureHeader = "X-API-Auth-Signature"

//FilterData defines the properties of a pre/post API filter
type FilterData struct {
	Name        string   `json:"name"`
	Endpoint    string   `json:"endpoint"`
	SecretToken string   `json:"secretToken"`
	Methods     []string `json:"methods"`
	Paths       []string `json:"paths"`
}

//APIRequestData defines the properties of a API Request/Response Body sent to/from a filter
type APIRequestData struct {
	Headers map[string][]string    `json:"headers,omitempty"`
	Body    map[string]interface{} `json:"body,omitempty"`
	UUID    string                 `json:"UUID,omitempty"`
	APIPath string                 `json:"APIPath,omitempty"`
	EnvID   string                 `json:"envID,omitempty"`
	Status  int                    `json:"status,omitempty"`
}
