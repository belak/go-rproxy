package main

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/mholt/certmagic"
	"golang.org/x/sync/errgroup"
)

var HTTPSUpgradeHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.URL.Host+"/"+r.URL.Path, http.StatusMovedPermanently)
})

type Server struct {
	cfg *certmagic.Config
}

func NewServer() *Server {
	cfg := certmagic.NewDefault()

	return &Server{
		cfg,
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

			if container.Config != nil {
				log.Println(container.ID, event.Actor.ID)
				log.Println(container.Config.Labels["rproxy.frontend"])
				log.Println(container.Config.Labels["rproxy.backend"])
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
