package docker

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/health"
	"github.com/distribution/distribution/v3/registry"
	"github.com/distribution/distribution/v3/registry/handlers"
	"github.com/distribution/distribution/v3/registry/listener"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem" // register filesystem for registry
	"github.com/oliverkofoed/dogo/version"
)

var registryPort = 52929
var registryAddr = "unconfigured"

var registryStartOnce sync.Once
var registryStartErr error

func StartDockerRegistry(logLevel string) error {
	registryStartOnce.Do(func() {
		// setup context
		ctx := dcontext.WithVersion(dcontext.Background(), version.Version)
		config := &configuration.Configuration{
			Storage: configuration.Storage{
				"filesystem": configuration.Parameters{
					"rootdirectory": ".dogocache/dockerregistry",
				},
			},
		}
		config.HTTP.Net = "tcp"
		config.HTTP.Addr = registryAddr
		config.HTTP.Secret = "abcedef12345"
		config.Log.Level = "panic"
		config.Loglevel = "panic"
		config.Log.AccessLog.Disabled = true
		registry.NewRegistry(ctx, config) // sets log level to null

		app := handlers.NewApp(ctx, config)
		app.RegisterHealthChecks()
		handler := alive("/", app)
		handler = health.Handler(handler)
		handler = panicHandler(handler)
		server := &http.Server{Handler: handler}

		// Find the addresses to listen to. (try not to use loopback, since that is not reachable from
		// docker running in boot2docker and other vms on mac)
		listenAddr := make(map[string]bool)
		ifaces, err := net.Interfaces()
		if err != nil {
			registryStartErr = err
			return
		}
		for _, i := range ifaces {
			addrs, err := i.Addrs()
			if err != nil {
				registryStartErr = err
				return
			}
			for _, addr := range addrs {
				switch v := addr.(type) {
				case *net.IPNet:
					if ipv4 := v.IP.To4(); ipv4 != nil {
						listenAddr[fmt.Sprintf("%v:%v", ipv4.String(), registryPort)] = true
					}
				}
			}
		}

		set := false
		for addr := range listenAddr {
			ln, err := listener.NewListener(config.HTTP.Net, addr)
			if err != nil {
				registryStartErr = err
				return
			}
			if !set && !strings.HasPrefix(ln.Addr().String(), "127.") {
				registryAddr = ln.Addr().String()
				set = true
			}
			go server.Serve(ln)
		}
	})
	return registryStartErr
}

func panicHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Panic(fmt.Sprintf("%v", err))
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

func alive(path string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			return
		}

		handler.ServeHTTP(w, r)
	})
}
