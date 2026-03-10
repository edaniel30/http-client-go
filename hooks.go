package httpclient

import "net/http"

// Hook defines lifecycle callbacks for HTTP requests.
// All methods are optional — implement only the ones you need.
type Hook interface {
	// OnRequestStart is called before the request is sent.
	OnRequestStart(req *http.Request)

	// OnRequestEnd is called after a successful response is received.
	OnRequestEnd(req *http.Request, resp *http.Response)

	// OnError is called when the request fails with an error.
	OnError(req *http.Request, err error)
}
