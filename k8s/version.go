package k8s

import "net/http"

const response = `{
	"major": "1",
	"minor": "2+",
	"gitVersion": "v1.2.0-alpha.8.15+3f0153c9163cee-dirty",
	"gitCommit": "3f0153c9163cee5aafdcf6fdd21e783495f5bba4",
	"gitTreeState": "dirty"
}`

func Version(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Write([]byte(response))
}
