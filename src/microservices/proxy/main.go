package main

import (
	"bytes"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Configuration
var (
	monolithURL           *url.URL
	moviesServiceURL      *url.URL
	eventsServiceURL      *url.URL
	gradualMigration      bool
	moviesMigrationPercent int
	eventsMigrationPercent int // Assuming similar for events, but not in yaml yet
)

func init() {
	// Seed random generator
	rand.Seed(time.Now().UnixNano())

	// Parse URLs
	var err error
	monolithURL, err = url.Parse(os.Getenv("MONOLITH_URL"))
	if err != nil {
		log.Fatal("Invalid MONOLITH_URL")
	}
	if monolithURL.Host == "" {
		monolithURL, _ = url.Parse("http://localhost:8080") // Fallback
	}

	moviesServiceURL, err = url.Parse(os.Getenv("MOVIES_SERVICE_URL"))
	if err != nil {
		log.Fatal("Invalid MOVIES_SERVICE_URL")
	}
	if moviesServiceURL.Host == "" {
		moviesServiceURL, _ = url.Parse("http://localhost:8081") // Fallback
	}

	eventsServiceURL, err = url.Parse(os.Getenv("EVENTS_SERVICE_URL"))
	if err != nil {
		log.Fatal("Invalid EVENTS_SERVICE_URL")
	}
	if eventsServiceURL.Host == "" {
		eventsServiceURL, _ = url.Parse("http://localhost:8082") // Fallback
	}

	// Parse migration settings
	gradualMigration = os.Getenv("GRADUAL_MIGRATION") == "true"
	if gradualMigration {
		log.Println("Gradual migration enabled")
	}

	if percentStr := os.Getenv("MOVIES_MIGRATION_PERCENT"); percentStr != "" {
		if percent, err := strconv.Atoi(percentStr); err == nil && percent >= 0 && percent <= 100 {
			moviesMigrationPercent = percent
		} else {
			log.Printf("Invalid MOVIES_MIGRATION_PERCENT, using 0: %s", percentStr)
		}
	}
	log.Printf("Movies migration percent: %d", moviesMigrationPercent)

	// For events, assume similar if needed, but not in yaml yet
	eventsMigrationPercent = 0 // Default to 0
}

// loggingMiddleware логирует входящие HTTP-запросы
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Логируем базовую информацию о запросе
		log.Printf("Incoming request: %s %s from %s", r.Method, r.URL.String(), r.RemoteAddr)

		// Логируем ключевые заголовки (избегайте чувствительных, как Authorization)
		log.Printf("Headers: User-Agent=%s, Content-Type=%s, Content-Length=%s",
			r.Header.Get("User-Agent"), r.Header.Get("Content-Type"), r.Header.Get("Content-Length"))

		// Опционально логируем тело запроса (только для отладки, с осторожностью - не логируйте чувствительные данные!)
		// Для production отключите это, чтобы избежать утечек.
		if r.Body != nil && r.Header.Get("Content-Type") != "" {
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				log.Printf("Request body: %s", string(bodyBytes))
				// Восстанавливаем тело для дальнейшей обработки
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			} else {
				log.Printf("Error reading request body: %v", err)
			}
		}

		// Оборачиваем ResponseWriter для захвата статуса и размера ответа (опционально)
		wrappedWriter := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Вызываем следующий handler
		next.ServeHTTP(wrappedWriter, r)

		// Логируем время выполнения и статус ответа
		duration := time.Since(start)
		log.Printf("Request completed: status=%d, duration=%v", wrappedWriter.statusCode, duration)
	})
}

// responseWriter оборачивает http.ResponseWriter для захвата статуса
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func main() {
	// Create reverse proxies
	monolithProxy := httputil.NewSingleHostReverseProxy(monolithURL)
	moviesProxy := httputil.NewSingleHostReverseProxy(moviesServiceURL)
	eventsProxy := httputil.NewSingleHostReverseProxy(eventsServiceURL)

	// Handler to route requests
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/movies"):
			routeMovies(w, r, monolithProxy, moviesProxy)
		case strings.HasPrefix(r.URL.Path, "/api/events"):
			routeEvents(w, r, monolithProxy, eventsProxy)
		default:
			log.Printf("Routing to monolith: %s %s", r.Method, r.URL.Path)
			r.URL.Host = monolithURL.Host
			r.URL.Scheme = monolithURL.Scheme
			r.Host = monolithURL.Host
			monolithProxy.ServeHTTP(w, r)
		}
	})

	// Применяем middleware ко всем маршрутам
	loggedMux := loggingMiddleware(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	log.Printf("API Gateway starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, loggedMux))
}

func routeMovies(w http.ResponseWriter, r *http.Request, monolithProxy, moviesProxy *httputil.ReverseProxy) {
	if gradualMigration {
		// Gradual migration: route based on percentage
		if rand.Intn(100) < moviesMigrationPercent {
			log.Printf("Routing movies to new service: %s %s", r.Method, r.URL.Path)
			r.URL.Host = moviesServiceURL.Host
			r.URL.Scheme = moviesServiceURL.Scheme
			r.Host = moviesServiceURL.Host
			moviesProxy.ServeHTTP(w, r)
		} else {
			log.Printf("Routing movies to monolith: %s %s", r.Method, r.URL.Path)
			r.URL.Host = monolithURL.Host
			r.URL.Scheme = monolithURL.Scheme
			r.Host = monolithURL.Host
			monolithProxy.ServeHTTP(w, r)
		}
	} else {
		// If gradual migration is off, route to monolith
		log.Printf("Routing movies to monolith (gradual off): %s %s", r.Method, r.URL.Path)
		r.URL.Host = monolithURL.Host
		r.URL.Scheme = monolithURL.Scheme
		r.Host = monolithURL.Host
		monolithProxy.ServeHTTP(w, r)
	}
}

func routeEvents(w http.ResponseWriter, r *http.Request, monolithProxy, eventsProxy *httputil.ReverseProxy) {
	// Similar logic for events, but using eventsMigrationPercent (default 0)
	if gradualMigration && rand.Intn(100) < eventsMigrationPercent {
		log.Printf("Routing events to new service: %s %s", r.Method, r.URL.Path)
		r.URL.Host = eventsServiceURL.Host
		r.URL.Scheme = eventsServiceURL.Scheme
		r.Host = eventsServiceURL.Host
		eventsProxy.ServeHTTP(w, r)
	} else {
		log.Printf("Routing events to monolith: %s %s", r.Method, r.URL.Path)
		r.URL.Host = monolithURL.Host
		r.URL.Scheme = monolithURL.Scheme
		r.Host = monolithURL.Host
		monolithProxy.ServeHTTP(w, r)
	}
}