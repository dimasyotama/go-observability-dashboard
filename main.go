package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Prometheus metrics
var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "handler", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "handler"},
	)

	itemOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "item_operations_total",
			Help: "Total number of item operations",
		},
		[]string{"operation", "status"},
	)

	searchRequestsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "search_requests_total",
			Help: "Total number of search requests",
		},
	)

	searchResultsCount = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "search_results_count",
			Help:    "Histogram of the number of results returned by search",
			Buckets: []float64{0, 1, 5, 10, 25, 50},
		},
	)
)

// Item represents a product item
type Item struct {
	Name    string  `json:"name" binding:"required"`
	Price   float64 `json:"price" binding:"required"`
	IsOffer *bool   `json:"is_offer,omitempty"`
}

// Fake database
var (
	fakeItemsDB = map[int]map[string]interface{}{
		1: {"name": "laptop", "price": 1200.0},
		2: {"name": "mouse", "price": 25.0},
		3: {"name": "keyboard", "price": 75.0},
	}

	allItems = []map[string]interface{}{
		{"name": "laptop", "price": 1200.0},
		{"name": "mouse", "price": 25.0},
		{"name": "keyboard", "price": 75.0},
		{"name": "monitor", "price": 300.0},
		{"name": "webcam", "price": 50.0},
	}
)

func init() {
	// Register Prometheus metrics
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(itemOperationsTotal)
	prometheus.MustRegister(searchRequestsTotal)
	prometheus.MustRegister(searchResultsCount)
}

// initTracer initializes OpenTelemetry tracer
func initTracer() (*sdktrace.TracerProvider, error) {
	ctx := context.Background()

	conn, err := grpc.DialContext(
		ctx,
		"otel-collector:4317",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("the-app"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}

// newSlogLogger creates a new structured logger that writes to both stdout and file
func newSlogLogger() (*slog.Logger, error) {
	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("/app/logs", 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile("/app/logs/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Create multi-writer (write to both stdout and file)
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	// Create JSON handler
	handler := slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	return slog.New(handler), nil
}

// structuredLogMiddleware adds a structured logger (slog) to the context
func structuredLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestLogger := logger

		span := trace.SpanFromContext(c.Request.Context())
		if span.SpanContext().IsValid() {
			requestLogger = logger.With(
				"trace_id", span.SpanContext().TraceID().String(),
				"span_id", span.SpanContext().SpanID().String(),
			)
		}

		c.Set("logger", requestLogger)
		c.Next()
	}
}

// prometheusMiddleware records metrics for each request
func prometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		path := c.FullPath()
		if path == "" {
			path = "none"
		}

		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
	}
}

func main() {
	// Initialize structured logger with file output
	logger, err := newSlogLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Initialize tracer
	tp, err := initTracer()
	if err != nil {
		logger.Error("Failed to initialize tracer", "error", err)
	} else {
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				logger.Error("Error shutting down tracer provider", "error", err)
			}
		}()
		logger.Info("Tracer initialized successfully")
	}

	// Create Gin router
	router := gin.Default()

	// Add OpenTelemetry middleware
	router.Use(otelgin.Middleware("the-app"))

	// Add structured logging middleware
	router.Use(structuredLogMiddleware(logger))

	// Add Prometheus middleware
	router.Use(prometheusMiddleware())

	// Prometheus metrics endpoint
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API endpoints
	router.GET("/", readRoot)
	router.GET("/items/:item_id", readItem)
	router.GET("/search/", searchItems)
	router.POST("/items/", createItem)
	router.GET("/status", getStatus)
	router.GET("/error-500", getError500)
	router.GET("/error-400", getError400)

	logger.Info("Starting server on port 5060...")
	if err := router.Run(":5060"); err != nil {
		logger.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}

// Helper to get logger from context
func getLogger(c *gin.Context) *slog.Logger {
	logger, exists := c.Get("logger")
	if !exists {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return logger.(*slog.Logger)
}

// Handler functions

func readRoot(c *gin.Context) {
	logger := getLogger(c)
	logger.Info("Accessed root endpoint")

	c.JSON(http.StatusOK, gin.H{
		"message": "Welcome to the Go application!",
	})
}

func readItem(c *gin.Context) {
	logger := getLogger(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.Atoi(itemIDStr)
	if err != nil {
		logger.Warn("Invalid item ID", "item_id", itemIDStr, "error", err)
		itemOperationsTotal.WithLabelValues("read", "bad_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid item ID"})
		return
	}

	item, exists := fakeItemsDB[itemID]
	if !exists {
		logger.Info("Item not found", "item_id", itemID)
		itemOperationsTotal.WithLabelValues("read", "not_found").Inc()
		c.JSON(http.StatusNotFound, gin.H{"detail": "Item not found"})
		return
	}

	response := map[string]interface{}{
		"item_id": itemID,
	}
	for k, v := range item {
		response[k] = v
	}

	logger.Info("Successfully retrieved item", "item_id", itemID)
	itemOperationsTotal.WithLabelValues("read", "success").Inc()
	c.JSON(http.StatusOK, response)
}

func searchItems(c *gin.Context) {
	logger := getLogger(c)
	searchRequestsTotal.Inc()

	name := c.Query("name")
	minPriceStr := c.DefaultQuery("min_price", "0")
	minPrice, err := strconv.ParseFloat(minPriceStr, 64)
	if err != nil {
		logger.Warn("Invalid min_price query param", "min_price", minPriceStr, "error", err)
		minPrice = 0
	}

	var results []map[string]interface{}
	for _, item := range allItems {
		itemName := item["name"].(string)
		itemPrice := item["price"].(float64)

		nameMatch := name == "" || strings.Contains(strings.ToLower(itemName), strings.ToLower(name))
		priceMatch := itemPrice >= minPrice

		if nameMatch && priceMatch {
			results = append(results, item)
		}
	}

	searchResultsCount.Observe(float64(len(results)))
	logger.Info("Search performed", "query_name", name, "min_price", minPrice, "results_found", len(results))

	c.JSON(http.StatusOK, gin.H{
		"search_results": results,
	})
}

func createItem(c *gin.Context) {
	logger := getLogger(c)
	var item Item
	if err := c.ShouldBindJSON(&item); err != nil {
		logger.Warn("Failed to bind JSON for create item", "error", err.Error())
		itemOperationsTotal.WithLabelValues("create", "bad_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	logger.Info("Item created successfully", "item_name", item.Name, "item_price", item.Price)
	itemOperationsTotal.WithLabelValues("create", "success").Inc()
	c.JSON(http.StatusOK, gin.H{
		"message": "Item created successfully",
		"item":    item,
	})
}

func getStatus(c *gin.Context) {
	getLogger(c).Info("Health check performed")
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"version": "1.0",
	})
}

func getError500(c *gin.Context) {
	getLogger(c).Error("Simulating 500 Internal Server Error")
	c.JSON(http.StatusInternalServerError, gin.H{
		"detail": "Internal Server Error",
	})
}

func getError400(c *gin.Context) {
	getLogger(c).Warn("Simulating 400 Bad Request")
	c.JSON(http.StatusBadRequest, gin.H{
		"detail": "Bad Request",
	})
}