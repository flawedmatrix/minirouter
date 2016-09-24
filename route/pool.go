package route

// Registry ...
type Registry struct{}

// Lookup looks up an endpoint pool by URI
func (r *Registry) Lookup(uri string) *Pool {
	return &Pool{}
}

// Pool stores routes
type Pool struct{}

// Endpoints returns all of the endpoints in this pool
func (p *Pool) Endpoints() EndpointIterator {
	return newEndpointIterator()
}

// EndpointIterator iterates
type EndpointIterator interface {
	Next() *Endpoint
}

type endpointIterator struct {
	endpoint *Endpoint
}

func newEndpointIterator() *endpointIterator {
	return &endpointIterator{
		endpoint: &Endpoint{},
	}
}

func (e *endpointIterator) Next() *Endpoint {
	return e.endpoint
}

// Endpoint describes a backend endpoint
type Endpoint struct{}

// CanonicalAddr returns the canonical address
func (e *Endpoint) CanonicalAddr() string {
	return "127.0.0.1:8081"
}
