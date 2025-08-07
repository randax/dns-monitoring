package exporters

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randax/dns-monitoring/internal/config"
)

type prometheusServer struct {
	config   *config.PrometheusConfig
	server   *http.Server
	mux      *http.ServeMux
	registry *prometheus.Registry
}

func newPrometheusServer(cfg *config.PrometheusConfig, registry *prometheus.Registry) (*prometheusServer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("prometheus config is nil")
	}
	
	if registry == nil {
		return nil, fmt.Errorf("prometheus registry is nil")
	}
	
	mux := http.NewServeMux()
	
	ps := &prometheusServer{
		config:   cfg,
		mux:      mux,
		registry: registry,
	}
	
	ps.setupRoutes()
	
	ps.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      ps.withMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	return ps, nil
}

func (s *prometheusServer) setupRoutes() {
	promHandler := promhttp.HandlerFor(
		s.registry,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
			Registry:         s.registry,
			Timeout:          10 * time.Second,
			MaxRequestsInFlight: 10,
		},
	)
	
	s.mux.Handle(s.config.Path, promHandler)
	
	s.mux.HandleFunc("/health", s.healthHandler)
	
	s.mux.HandleFunc("/ready", s.readyHandler)
	
	s.mux.HandleFunc("/", s.rootHandler)
}

func (s *prometheusServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK\n")
}

func (s *prometheusServer) readyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "READY\n")
}

func (s *prometheusServer) rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>DNS Monitoring Prometheus Exporter</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            margin: 40px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            border-bottom: 2px solid #e74c3c;
            padding-bottom: 10px;
        }
        .endpoints {
            margin: 20px 0;
        }
        .endpoint {
            margin: 15px 0;
            padding: 15px;
            background: #f8f8f8;
            border-left: 4px solid #3498db;
            border-radius: 4px;
        }
        .endpoint a {
            color: #3498db;
            text-decoration: none;
            font-weight: bold;
        }
        .endpoint a:hover {
            text-decoration: underline;
        }
        .description {
            color: #666;
            margin-top: 5px;
        }
        .config {
            margin-top: 30px;
            padding: 20px;
            background: #f0f0f0;
            border-radius: 4px;
        }
        .config h2 {
            color: #555;
            margin-top: 0;
        }
        .config-item {
            margin: 10px 0;
        }
        .config-label {
            font-weight: bold;
            color: #666;
        }
        .config-value {
            color: #333;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>DNS Monitoring Prometheus Exporter</h1>
        
        <div class="endpoints">
            <div class="endpoint">
                <a href="%s">%s</a>
                <div class="description">Prometheus metrics endpoint - scrape this endpoint for DNS monitoring metrics</div>
            </div>
            
            <div class="endpoint">
                <a href="/health">/health</a>
                <div class="description">Health check endpoint - returns OK if the exporter is running</div>
            </div>
            
            <div class="endpoint">
                <a href="/ready">/ready</a>
                <div class="description">Readiness check endpoint - returns READY when metrics are available</div>
            </div>
        </div>
        
        <div class="config">
            <h2>Current Configuration</h2>
            <div class="config-item">
                <span class="config-label">Port:</span>
                <span class="config-value">%d</span>
            </div>
            <div class="config-item">
                <span class="config-label">Metrics Path:</span>
                <span class="config-value">%s</span>
            </div>
            <div class="config-item">
                <span class="config-label">Update Interval:</span>
                <span class="config-value">%s</span>
            </div>
            <div class="config-item">
                <span class="config-label">Server Labels:</span>
                <span class="config-value">%v</span>
            </div>
            <div class="config-item">
                <span class="config-label">Protocol Labels:</span>
                <span class="config-value">%v</span>
            </div>
            <div class="config-item">
                <span class="config-label">Metric Prefix:</span>
                <span class="config-value">%s</span>
            </div>
        </div>
        
        <div style="margin-top: 40px; padding-top: 20px; border-top: 1px solid #ddd; color: #999; text-align: center;">
            DNS Monitoring Tool - Prometheus Exporter
        </div>
    </div>
</body>
</html>`,
		s.config.Path, s.config.Path,
		s.config.Port,
		s.config.Path,
		s.config.UpdateInterval,
		s.config.IncludeServerLabels,
		s.config.IncludeProtocolLabels,
		s.config.MetricPrefix)
}

func (s *prometheusServer) withMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "dns-monitoring-prometheus")
		
		handler.ServeHTTP(w, r)
	})
}

func (s *prometheusServer) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("prometheus server failed: %w", err)
		}
	}()
	
	select {
	case err := <-errCh:
		return err
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func (s *prometheusServer) Stop() error {
	if s.server == nil {
		return nil
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown prometheus server: %w", err)
	}
	
	return nil
}

func (s *prometheusServer) IsRunning() bool {
	if s.server == nil {
		return false
	}
	
	url := fmt.Sprintf("http://localhost:%d/health", s.config.Port)
	client := &http.Client{Timeout: 1 * time.Second}
	
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}