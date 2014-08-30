package httpretry

import (
	"net/http"
)

type Callback func(*http.Response, error)

// SetCallback sets a function to be called after every attempted HTTP response.
func (g *HttpGetter) SetCallback(f Callback) {
	if f == nil {
		g.OnResponse(nil)
	} else {
		g.OnResponse(ResponseCallback(f))
	}
}
