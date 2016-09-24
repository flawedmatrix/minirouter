package proxy

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/flawedmatrix/minirouter/route"
)

// LookupRegistry ...
type LookupRegistry interface {
	Lookup(uri string) *route.Pool
}

// Proxy ...
type Proxy interface {
	ServeHTTP(responseWriter http.ResponseWriter, request *http.Request)
}

// Args ...
type Args struct {
	EndpointTimeout time.Duration
	IP              string
	Registry        LookupRegistry
	TLSConfig       *tls.Config
}

type proxy struct {
	ip        string
	registry  LookupRegistry
	transport *http.Transport
}

// NewProxy ...
func NewProxy(args Args) Proxy {
	p := &proxy{
		ip:       args.IP,
		registry: args.Registry,
		transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				conn, err := net.DialTimeout(network, addr, 5*time.Second)
				if err != nil {
					return conn, err
				}
				if args.EndpointTimeout > 0 {
					err = conn.SetDeadline(time.Now().Add(args.EndpointTimeout))
				}
				return conn, err
			},
			DisableKeepAlives:  true,
			DisableCompression: true,
			TLSClientConfig:    args.TLSConfig,
		},
	}

	return p
}

func hostWithoutPort(req *http.Request) string {
	host := req.Host

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	return host
}

func (p *proxy) lookup(request *http.Request) *route.Pool {
	requestPath := request.URL.EscapedPath()

	uri := hostWithoutPort(request) + requestPath

	return p.registry.Lookup(uri)
}

func (p *proxy) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	routePool := p.lookup(request)
	if routePool == nil {
		responseWriter.WriteHeader(404)
		responseWriter.Write([]byte("404 route not found\n"))
		fmt.Printf("404 route not found: %#v\n", request)
		return
	}

	after := func(rsp *http.Response, endpoint *route.Endpoint, err error) {
		if err != nil {
			responseWriter.WriteHeader(502)
			responseWriter.Write([]byte(fmt.Sprintf("Got error %s", err.Error())))
			fmt.Printf("Error serving request: %s\n", err.Error())
			return
		}

		// if ContentType not in response, nil out to suppress Go's autoDetect
		if _, ok := rsp.Header["ContentType"]; !ok {
			responseWriter.Header()["ContentType"] = nil
		}
	}

	roundTripper := NewProxyRoundTripper(p.transport, routePool.Endpoints(), after)

	newReverseProxy(roundTripper, request).ServeHTTP(responseWriter, request)
}

func newReverseProxy(proxyTransport http.RoundTripper, req *http.Request) http.Handler {
	rproxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			setupProxyRequest(req, request)
		},
		Transport:     proxyTransport,
		FlushInterval: 50 * time.Millisecond,
	}

	return rproxy
}

func setupProxyRequest(source *http.Request, target *http.Request) {
	if source.Header.Get("XForwardedProto") == "" {
		scheme := "http"
		if source.TLS != nil {
			scheme = "https"
		}
		target.Header.Set("XForwardedProto", scheme)
	}

	target.URL.Scheme = "http"
	target.URL.Host = source.Host
	target.URL.Opaque = source.RequestURI
	target.URL.RawQuery = ""
}
