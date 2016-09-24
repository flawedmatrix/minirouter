package proxy

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"

	"github.com/flawedmatrix/minirouter/route"
)

// AfterRoundTrip ...
type AfterRoundTrip func(rsp *http.Response, endpoint *route.Endpoint, err error)

// NewProxyRoundTripper ...
func NewProxyRoundTripper(transport http.RoundTripper, endpointIterator route.EndpointIterator, afterRoundTrip AfterRoundTrip) http.RoundTripper {
	return &BackendRoundTripper{
		transport: transport,
		iter:      endpointIterator,
		after:     afterRoundTrip,
	}
}

// BackendRoundTripper ...
type BackendRoundTripper struct {
	iter      route.EndpointIterator
	transport http.RoundTripper
	after     AfterRoundTrip
}

// RoundTrip ...
func (rt *BackendRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	if request.Body != nil {
		closer := request.Body
		request.Body = ioutil.NopCloser(request.Body)
		defer func() {
			closer.Close()
		}()
	}

	for retry := 0; retry < 5; retry++ {
		endpoint, err = rt.selectEndpoint(request)
		if err != nil {
			return nil, err
		}

		rt.setupRequest(request, endpoint)

		res, err = rt.transport.RoundTrip(request)
		if err == nil || !retryableError(err) {
			break
		}
		fmt.Printf("Retrying because of %s\n", err.Error())
	}

	if rt.after != nil {
		rt.after(res, endpoint, err)
	}

	return res, err
}

func (rt *BackendRoundTripper) selectEndpoint(request *http.Request) (*route.Endpoint, error) {
	endpoint := rt.iter.Next()

	if endpoint == nil {
		fmt.Println("Could not find endpoint...")
		return nil, errors.New("No available endpoints")
	}
	return endpoint, nil
}

func (rt *BackendRoundTripper) setupRequest(request *http.Request, endpoint *route.Endpoint) {
	request.URL.Host = endpoint.CanonicalAddr()
}

func retryableError(err error) bool {
	ne, netErr := err.(*net.OpError)
	if netErr && ne.Op == "dial" {
		return true
	}

	return false
}
