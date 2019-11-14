package main

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/mholt/certmagic"
	"golang.org/x/sync/errgroup"
)

type proxyDefinition struct {
	Frontend *url.URL
	Backend  *url.URL
}

var HTTPSUpgradeHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.Path, http.StatusMovedPermanently)
})

type Server struct {
	cfg *certmagic.Config

	// Mapping of container ID to a proxy definition
	containers  map[string]proxyDefinition
	hostHandler http.Handler
	lock        sync.RWMutex
}

func NewServer() *Server {
	cfg := certmagic.NewDefault()

	return &Server{
		cfg,
		make(map[string]proxyDefinition),
		http.NewServeMux(),
		sync.RWMutex{},
	}
}

func (s *Server) Run(parent context.Context) error {
	group, ctx := errgroup.WithContext(parent)

	group.Go(func() error {
		return s.runDocker(ctx)
	})

	group.Go(func() error {
		return s.runHTTP(ctx)
	})

	group.Go(func() error {
		return s.runHTTPS(ctx)
	})

	return group.Wait()
}

func (s *Server) runDocker(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatalln(err)
	}

	cli.NegotiateAPIVersion(ctx)

	filters := filters.NewArgs()
	filters.Add("type", events.ContainerEventType)

	// TODO: figure out why filtering on action doesn't seem to work
	filters.Add("action", "start")
	filters.Add("action", "stop")

	eventChan, errChan := cli.Events(ctx, types.EventsOptions{
		Filters: filters,
	})
	for {
		select {
		case event := <-eventChan:
			if event.Action != "start" && event.Action != "stop" {
				continue
			}

			container, err := cli.ContainerInspect(ctx, event.Actor.ID)
			if err != nil {
				return err
			}

			frontendConfig, frontendOk := container.Config.Labels["rproxy.frontend"]
			backendConfig, backendOk := container.Config.Labels["rproxy.backend"]

			if !(frontendOk && backendOk) {
				continue
			}

			if container.Config != nil {
				frontendURL, err := url.Parse(frontendConfig)
				if err != nil {
					log.Println(err)
					continue
				}

				backendURL, err := url.Parse(backendConfig)
				if err != nil {
					log.Println(err)
					continue
				}

				s.lock.Lock()
				if event.Action == "start" {
					s.containers[container.ID] = proxyDefinition{
						Frontend: frontendURL,
						Backend:  backendURL,
					}
					s.regenerateHandler(ctx)
				} else if event.Action == "stop" {
					delete(s.containers, container.ID)
					s.regenerateHandler(ctx)
				}
				s.lock.Unlock()
			} else {
				log.Println("nil container config")
			}
		case err := <-errChan:
			return err
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Server) regenerateHandler(ctx context.Context) {
	mux := http.NewServeMux()

	for _, def := range s.containers {
		spew.Dump(def)

		_ = s.cfg.ManageAsync(ctx, []string{def.Frontend.Hostname()})

		proxyHandler := httputil.NewSingleHostReverseProxy(def.Backend)
		director := proxyHandler.Director
		proxyHandler.Director = func(req *http.Request) {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, strings.TrimSuffix(def.Frontend.Path, "/"))
			log.Println("Before:", req.URL)
			director(req)
			log.Println("After:", req.URL)
		}

		mux.Handle(
			def.Frontend.Hostname()+def.Frontend.Path+"/",
			proxyHandler,
		)
	}

	s.hostHandler = mux
}

func (s *Server) runHTTP(ctx context.Context) error {
	listener, err := net.Listen("tcp", ":80")
	if err != nil {
		return err
	}

	// The http server only handles HTTP challenges and upgrades http to https.
	server := &http.Server{
		Addr:    ":80",
		Handler: s.cfg.HTTPChallengeHandler(HTTPSUpgradeHandler),
	}

	var errChan = make(chan error, 1)

	go func() {
		errChan <- server.Serve(listener)
	}()

	select {
	case err = <-errChan:
	case <-ctx.Done():
		return server.Shutdown(ctx)
	}

	return err
}

func (s *Server) runHTTPS(ctx context.Context) error {
	// TODO: when we set up raw proxying, we can use a net.Listener and then use
	// tls.Server(conn, config)
	listener, err := tls.Listen("tcp", ":443", s.cfg.TLSConfig())
	if err != nil {
		return err
	}

	// The http server only handles HTTP challenges and upgrades http to
	// https.
	server := &http.Server{
		Addr:    ":443",
		Handler: s.cfg.HTTPChallengeHandler(http.HandlerFunc(s.handler)),
	}

	var errChan = make(chan error, 1)

	go func() {
		errChan <- server.Serve(listener)
	}()

	select {
	case err = <-errChan:
	case <-ctx.Done():
		return server.Shutdown(ctx)
	}

	return err
}

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	s.hostHandler.ServeHTTP(w, r)
}
