// Package promexport provides an opt-in, unauthenticated metrics pull endpoint.
package promexport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Exporter owns a private registry. The metric provider owns Reader after registration.
type Exporter struct {
	Reader  sdkmetric.Reader
	handler http.Handler
}

func New() (*Exporter, error) {
	registry := prometheus.NewRegistry()
	reader, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutScopeInfo(),
		otelprom.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
	)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return &Exporter{Reader: reader, handler: mux}, nil
}

// Listen binds synchronously, so startup cannot claim success on an occupied port.
func (e *Exporter) Listen(address string) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &Server{listener: listener, http: &http.Server{
		Handler:           e.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}}, nil
}

type Server struct {
	listener net.Listener
	http     *http.Server
}

// Close also releases a listener if startup fails before Run.
func (s *Server) Close() error   { return s.listener.Close() }
func (s *Server) Addr() net.Addr { return s.listener.Addr() }

// Run serves until cancellation or a serving error, then bounds graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- s.http.Serve(s.listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.Join(err, s.http.Close())
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := s.http.Shutdown(shutdown)
		if err != nil {
			err = errors.Join(err, s.http.Close())
		}
		serveErr := <-done
		if !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
		return err
	}
}
