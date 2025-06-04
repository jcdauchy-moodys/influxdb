package main

import (
	"fmt"
	"net/http"

	"github.com/influxdata/influxdb/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MockStatistics simulates the httpd.Statistics struct
type MockStatistics struct {
	Requests        int64
	QueryRequests   int64
	WriteRequests   int64
	ActiveRequests  int64
	PointsWrittenOK int64
	ClientErrors    int64
	ServerErrors    int64
	RequestDuration int64
}

func main() {
	// Create mock statistics
	stats := &MockStatistics{
		Requests:        100,
		QueryRequests:   50,
		WriteRequests:   30,
		ActiveRequests:  5,
		PointsWrittenOK: 1000,
		ClientErrors:    2,
		ServerErrors:    1,
		RequestDuration: 5000000, // 5ms in nanoseconds
	}

	// Register the InfluxDB collector
	metrics.RegisterInfluxDBCollector(stats)

	// Set up HTTP server with metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	fmt.Println("Starting metrics test server on :8080")
	fmt.Println("Visit http://localhost:8080/metrics to see InfluxDB metrics")

	// Start server
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
	}
}
