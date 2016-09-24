package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/flawedmatrix/minirouter/proxy"
	"github.com/flawedmatrix/minirouter/route"
	"github.com/flawedmatrix/minirouter/router"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
)

func main() {
	registry := &route.Registry{}
	proxy := buildProxy(registry)

	router, err := router.NewRouter(proxy)
	if err != nil {
		log.Fatalf("Exited with failure: %s", err.Error())
	}

	members := grouper.Members{
		{"router", router},
	}

	group := grouper.NewOrdered(os.Interrupt, members)

	monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT))

	fmt.Println("Router started at 127.0.0.1:8888")

	err = <-monitor.Wait()
	if err != nil {
		log.Fatalf("Exited with failure: %s", err.Error())
	}

	os.Exit(0)
}

func buildProxy(registry proxy.LookupRegistry) proxy.Proxy {
	args := proxy.Args{
		EndpointTimeout: 60 * time.Second,
		IP:              "127.0.0.1",
		Registry:        registry,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	return proxy.NewProxy(args)
}
