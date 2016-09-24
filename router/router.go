package router

import (
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/flawedmatrix/minirouter/proxy"

	"errors"
	"net"
	"net/http"
	"time"
)

var errDrainTimeout = errors.New("router: Drain timeout")

const (
	emitInterval               = 1 * time.Second
	proxyProtocolHeaderTimeout = 100 * time.Millisecond
)

var noDeadline = time.Time{}

// Router routes connections
type Router struct {
	proxy proxy.Proxy

	listener         net.Listener
	tlsListener      net.Listener
	closeConnections bool
	connLock         sync.Mutex
	idleConns        map[net.Conn]struct{}
	activeConns      map[net.Conn]struct{}
	drainDone        chan struct{}
	serveDone        chan struct{}
	tlsServeDone     chan struct{}
	stopping         bool
	stopLock         sync.Mutex
	errChan          chan error
}

// NewRouter returns a new router
func NewRouter(p proxy.Proxy) (*Router, error) {
	routerErrChan := make(chan error, 2)

	router := &Router{
		proxy:        p,
		serveDone:    make(chan struct{}),
		tlsServeDone: make(chan struct{}),
		idleConns:    make(map[net.Conn]struct{}),
		activeConns:  make(map[net.Conn]struct{}),
		errChan:      routerErrChan,
		stopping:     false,
	}

	return router, nil
}

type gorouterHandler struct {
	handler http.Handler
}

func (h *gorouterHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	h.handler.ServeHTTP(res, req)
}

// Run is the main runner for this server
func (r *Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	handler := gorouterHandler{handler: r.proxy}

	server := &http.Server{
		Handler:   &handler,
		ConnState: r.HandleConnState,
	}

	err := r.serveHTTP(server, r.errChan)
	if err != nil {
		r.errChan <- err
		return err
	}
	close(ready)

	r.OnErrOrSignal(signals, r.errChan)

	return nil
}

// OnErrOrSignal handles a stopping the router
func (r *Router) OnErrOrSignal(signals <-chan os.Signal, errChan chan error) {
	select {
	case err := <-errChan:
		if err != nil {
			r.DrainAndStop()
		}
	case sig := <-signals:
		if sig == syscall.SIGINT {
			r.DrainAndStop()
		} else {
			r.Stop()
		}
	}
}

// DrainAndStop drains connections before stopping
func (r *Router) DrainAndStop() {
	r.Drain(30 * time.Second)

	r.Stop()
}

func (r *Router) serveHTTP(server *http.Server, errChan chan error) error {
	listener, err := net.Listen("tcp", ":8888")
	if err != nil {
		return err
	}

	r.listener = listener

	go func() {
		err := server.Serve(r.listener)
		r.stopLock.Lock()
		if !r.stopping {
			errChan <- err
		}
		r.stopLock.Unlock()

		close(r.serveDone)
	}()
	return nil
}

// Drain drains connections
func (r *Router) Drain(drainTimeout time.Duration) error {
	fmt.Println("Draining connections...")
	r.stopListening()

	drained := make(chan struct{})

	r.connLock.Lock()

	r.closeIdleConns()

	if len(r.activeConns) == 0 {
		close(drained)
	} else {
		r.drainDone = drained
	}

	r.connLock.Unlock()

	select {
	case <-drained:
	case <-time.After(drainTimeout):
		return errDrainTimeout
	}

	return nil
}

// Stop stops the router
func (r *Router) Stop() {
	fmt.Println("Stopping the router...")
	r.stopListening()

	r.connLock.Lock()
	r.closeIdleConns()
	r.connLock.Unlock()
}

// connLock must be locked
func (r *Router) closeIdleConns() {
	r.closeConnections = true

	for conn := range r.idleConns {
		conn.Close()
	}
}

func (r *Router) stopListening() {
	r.stopLock.Lock()
	r.stopping = true
	r.stopLock.Unlock()

	r.listener.Close()

	if r.tlsListener != nil {
		r.tlsListener.Close()
		<-r.tlsServeDone
	}

	<-r.serveDone
}

// HandleConnState handles the state of the connection
func (r *Router) HandleConnState(conn net.Conn, state http.ConnState) {
	endpointTimeout := 30 * time.Second

	r.connLock.Lock()

	switch state {
	case http.StateActive:
		r.activeConns[conn] = struct{}{}
		delete(r.idleConns, conn)

		conn.SetDeadline(noDeadline)
	case http.StateIdle:
		delete(r.activeConns, conn)
		r.idleConns[conn] = struct{}{}

		if r.closeConnections {
			conn.Close()
		} else {
			deadline := noDeadline
			if endpointTimeout > 0 {
				deadline = time.Now().Add(endpointTimeout)
			}

			conn.SetDeadline(deadline)
		}
	case http.StateHijacked, http.StateClosed:
		i := len(r.idleConns)
		delete(r.idleConns, conn)
		if i == len(r.idleConns) {
			delete(r.activeConns, conn)
		}
	}

	if r.drainDone != nil && len(r.activeConns) == 0 {
		close(r.drainDone)
		r.drainDone = nil
	}

	r.connLock.Unlock()
}
