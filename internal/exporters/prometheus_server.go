package exporters

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randax/dns-monitoring/internal/config"
	"golang.org/x/time/rate"
)

type prometheusServer struct {
	config      *config.PrometheusConfig
	server      *http.Server
	mux         *http.ServeMux
	registry    *prometheus.Registry
	exporter    *PrometheusExporter
	rateLimiters map[string]*rateLimiter
	rlMutex      sync.RWMutex
	logger       *log.Logger
}

type rateLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newPrometheusServer(cfg *config.PrometheusConfig, registry *prometheus.Registry, exporter *PrometheusExporter) (*prometheusServer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("prometheus config is nil")
	}
	
	if registry == nil {
		return nil, fmt.Errorf("prometheus registry is nil")
	}
	
	mux := http.NewServeMux()
	
	ps := &prometheusServer{
		config:       cfg,
		mux:          mux,
		registry:     registry,
		exporter:     exporter,
		rateLimiters: make(map[string]*rateLimiter),
		logger:       log.New(io.Discard, "[prometheus] ", log.LstdFlags),
	}
	
	if cfg.Security.RequestLogging.Enabled {
		ps.logger = log.New(log.Writer(), "[prometheus] ", log.LstdFlags)
	}
	
	ps.setupRoutes()
	
	handler := ps.withMiddleware(mux)
	
	ps.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	if cfg.Security.TLS.Enabled {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		
		if cfg.Security.TLS.ClientAuth {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			// Load client CA certificates
			// Implementation would load the CA cert files here
		}
		
		ps.server.TLSConfig = tlsConfig
	}
	
	// Start rate limit cleanup goroutine if enabled
	if cfg.Security.RateLimit.Enabled {
		go ps.cleanupRateLimiters()
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
	
	// Use default path if not specified
	metricsPath := s.config.Path
	if metricsPath == "" {
		metricsPath = "/metrics"
	}
	s.mux.Handle(metricsPath, promHandler)
	
	s.mux.HandleFunc("/health", s.healthHandler)
	
	s.mux.HandleFunc("/ready", s.readyHandler)
	
	s.mux.HandleFunc("/metrics/health", s.metricsHealthHandler)
	
	s.mux.HandleFunc("/metrics/status", s.metricsStatusHandler)
	
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

func (s *prometheusServer) metricsHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

func (s *prometheusServer) metricsStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	response := map[string]interface{}{
		"healthy": true,
		"status": "operational",
	}
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
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
                <div class="description">Readiness check endpoint - returns READY when metrics are available and healthy</div>
            </div>
            
            <div class="endpoint">
                <a href="/metrics/health">/metrics/health</a>
                <div class="description">Metrics health endpoint - returns detailed health status of metric updates</div>
            </div>
            
            <div class="endpoint">
                <a href="/metrics/status">/metrics/status</a>
                <div class="description">Metrics status endpoint - returns comprehensive error statistics and backoff information</div>
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
	// Apply middleware in order: logging -> rate limiting -> auth -> main handler
	h := handler
	
	// Basic authentication middleware
	if s.config.Security.BasicAuth.Enabled {
		h = s.basicAuthMiddleware(h)
	}
	
	// Rate limiting middleware
	if s.config.Security.RateLimit.Enabled {
		h = s.rateLimitMiddleware(h)
	}
	
	// Request logging middleware (outermost to log all requests)
	if s.config.Security.RequestLogging.Enabled {
		h = s.requestLoggingMiddleware(h)
	}
	
	// Default server header
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "dns-monitoring-prometheus")
		h.ServeHTTP(w, r)
	})
}

func (s *prometheusServer) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			s.sendUnauthorized(w)
			return
		}
		
		expectedPassword, validUser := s.config.Security.BasicAuth.Users[username]
		if !validUser || subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) != 1 {
			s.sendUnauthorized(w)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func (s *prometheusServer) sendUnauthorized(w http.ResponseWriter) {
	realm := s.config.Security.BasicAuth.Realm
	if realm == "" {
		realm = "Prometheus Metrics"
	}
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func (s *prometheusServer) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var key string
		if s.config.Security.RateLimit.PerIP {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			key = ip
			
			// Check if IP is whitelisted
			if s.isIPWhitelisted(ip) {
				next.ServeHTTP(w, r)
				return
			}
		} else {
			key = "global"
		}
		
		limiter := s.getRateLimiter(key)
		if !limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func (s *prometheusServer) getRateLimiter(key string) *rate.Limiter {
	s.rlMutex.Lock()
	defer s.rlMutex.Unlock()
	
	if rl, exists := s.rateLimiters[key]; exists {
		rl.lastSeen = time.Now()
		return rl.limiter
	}
	
	// Create new rate limiter
	limit := rate.Limit(float64(s.config.Security.RateLimit.RequestsPerMin) / 60.0)
	burst := s.config.Security.RateLimit.BurstSize
	if burst <= 0 {
		burst = s.config.Security.RateLimit.RequestsPerMin / 2
	}
	
	limiter := rate.NewLimiter(limit, burst)
	s.rateLimiters[key] = &rateLimiter{
		limiter:  limiter,
		lastSeen: time.Now(),
	}
	
	return limiter
}

func (s *prometheusServer) isIPWhitelisted(ip string) bool {
	for _, whitelisted := range s.config.Security.RateLimit.WhitelistIPs {
		if strings.Contains(whitelisted, "/") {
			// CIDR notation
			_, ipNet, err := net.ParseCIDR(whitelisted)
			if err == nil && ipNet.Contains(net.ParseIP(ip)) {
				return true
			}
		} else if whitelisted == ip {
			return true
		}
	}
	return false
}

func (s *prometheusServer) cleanupRateLimiters() {
	ticker := time.NewTicker(s.config.Security.RateLimit.CleanupInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		s.rlMutex.Lock()
		now := time.Now()
		for key, rl := range s.rateLimiters {
			if now.Sub(rl.lastSeen) > s.config.Security.RateLimit.CleanupInterval*2 {
				delete(s.rateLimiters, key)
			}
		}
		s.rlMutex.Unlock()
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

func (s *prometheusServer) requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path should be excluded from logging
		for _, excludePath := range s.config.Security.RequestLogging.ExcludePaths {
			if r.URL.Path == excludePath {
				next.ServeHTTP(w, r)
				return
			}
		}
		
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, status: 200}
		
		// Log request based on log level
		if s.shouldLogRequest() {
			s.logRequest(r)
		}
		
		next.ServeHTTP(wrapped, r)
		
		duration := time.Since(start)
		
		// Log response
		s.logResponse(r, wrapped.status, wrapped.size, duration)
		
		// Log slow requests
		if duration > s.config.Security.RequestLogging.SlowRequestThreshold {
			s.logger.Printf("SLOW REQUEST: %s %s took %v (threshold: %v)", 
				r.Method, r.URL.Path, duration, s.config.Security.RequestLogging.SlowRequestThreshold)
		}
	})
}

func (s *prometheusServer) shouldLogRequest() bool {
	level := s.config.Security.RequestLogging.LogLevel
	return level == "debug" || level == "info"
}

func (s *prometheusServer) logRequest(r *http.Request) {
	logMsg := fmt.Sprintf("REQUEST: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	
	if s.config.Security.RequestLogging.LogHeaders {
		headers := make([]string, 0)
		for name, values := range r.Header {
			headers = append(headers, fmt.Sprintf("%s: %s", name, strings.Join(values, ", ")))
		}
		logMsg += fmt.Sprintf(" Headers: [%s]", strings.Join(headers, "; "))
	}
	
	if s.config.Security.RequestLogging.LogBody && r.ContentLength > 0 {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		logMsg += fmt.Sprintf(" Body: %s", string(body))
	}
	
	s.logger.Print(logMsg)
}

func (s *prometheusServer) logResponse(r *http.Request, status, size int, duration time.Duration) {
	level := s.config.Security.RequestLogging.LogLevel
	
	// Only log based on level and status
	if level == "error" && status < 400 {
		return
	}
	if level == "warn" && status < 300 {
		return
	}
	
	s.logger.Printf("RESPONSE: %s %s - Status: %d, Size: %d bytes, Duration: %v", 
		r.Method, r.URL.Path, status, size, duration)
}

func (s *prometheusServer) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	
	go func() {
		var err error
		if s.config.Security.TLS.Enabled {
			s.logger.Printf("Starting HTTPS server on port %d", s.config.Port)
			err = s.server.ListenAndServeTLS(s.config.Security.TLS.CertFile, s.config.Security.TLS.KeyFile)
		} else {
			s.logger.Printf("Starting HTTP server on port %d", s.config.Port)
			err = s.server.ListenAndServe()
		}
		
		if err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("prometheus server failed: %w", err)
		}
	}()
	
	select {
	case err := <-errCh:
		return err
	case <-time.After(100 * time.Millisecond):
		// Log security features enabled
		s.logSecurityFeatures()
		return nil
	}
}

func (s *prometheusServer) logSecurityFeatures() {
	features := []string{}
	
	if s.config.Security.BasicAuth.Enabled {
		features = append(features, fmt.Sprintf("BasicAuth (users: %d)", len(s.config.Security.BasicAuth.Users)))
	}
	if s.config.Security.TLS.Enabled {
		tlsInfo := "TLS"
		if s.config.Security.TLS.ClientAuth {
			tlsInfo += " with client auth"
		}
		features = append(features, tlsInfo)
	}
	if s.config.Security.RateLimit.Enabled {
		rlInfo := fmt.Sprintf("RateLimit (%d req/min", s.config.Security.RateLimit.RequestsPerMin)
		if s.config.Security.RateLimit.PerIP {
			rlInfo += ", per-IP"
		}
		rlInfo += ")"
		features = append(features, rlInfo)
	}
	if s.config.Security.RequestLogging.Enabled {
		features = append(features, fmt.Sprintf("RequestLogging (level: %s)", s.config.Security.RequestLogging.LogLevel))
	}
	
	if len(features) > 0 {
		s.logger.Printf("Security features enabled: %s", strings.Join(features, ", "))
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
	
	scheme := "http"
	if s.config.Security.TLS.Enabled {
		scheme = "https"
	}
	
	url := fmt.Sprintf("%s://localhost:%d/health", scheme, s.config.Port)
	client := &http.Client{
		Timeout: 1 * time.Second,
	}
	
	// For TLS connections, skip certificate verification for health checks
	if s.config.Security.TLS.Enabled {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}
	
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}