package metrics

import (
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
)

// Global singleton collector
var (
	globalCollector *InfluxDBCollector
	collectorMutex  sync.Mutex
)

// InfluxDBCollector implements prometheus.Collector to expose InfluxDB statistics
type InfluxDBCollector struct {
	stats StatsProvider
	mutex sync.RWMutex

	// Metric descriptors
	httpRequests        *prometheus.Desc
	httpRequestDuration *prometheus.Desc
	activeRequests      *prometheus.Desc
	writeRequests       *prometheus.Desc
	queryRequests       *prometheus.Desc
	pointsWritten       *prometheus.Desc
	clientErrors        *prometheus.Desc
	serverErrors        *prometheus.Desc
}

// StatsProvider interface allows the collector to access InfluxDB statistics
type StatsProvider interface {
	GetRequests() int64
	GetActiveRequests() int64
	GetWriteRequests() int64
	GetQueryRequests() int64
	GetPointsWrittenOK() int64
	GetClientErrors() int64
	GetServerErrors() int64
	GetRequestDuration() int64
}

// UpdateStats updates the stats provider for the collector
func (c *InfluxDBCollector) UpdateStats(stats StatsProvider) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.stats = stats
}

// NewInfluxDBCollector creates a new InfluxDB metrics collector
func NewInfluxDBCollector(stats StatsProvider) *InfluxDBCollector {
	return &InfluxDBCollector{
		stats: stats,
		httpRequests: prometheus.NewDesc(
			"influxdb_httpd_requests_total",
			"Total number of HTTP requests processed by InfluxDB HTTPD service",
			[]string{"type"}, nil,
		),
		httpRequestDuration: prometheus.NewDesc(
			"influxdb_httpd_request_duration_nanoseconds_total",
			"Total duration of HTTP requests in nanoseconds from HTTPD service",
			nil, nil,
		),
		activeRequests: prometheus.NewDesc(
			"influxdb_httpd_active_requests",
			"Number of currently active HTTP requests in HTTPD service",
			nil, nil,
		),
		writeRequests: prometheus.NewDesc(
			"influxdb_httpd_write_requests_total",
			"Total number of write requests to HTTPD service",
			nil, nil,
		),
		queryRequests: prometheus.NewDesc(
			"influxdb_httpd_query_requests_total",
			"Total number of query requests to HTTPD service",
			nil, nil,
		),
		pointsWritten: prometheus.NewDesc(
			"influxdb_httpd_points_written_total",
			"Total number of points successfully written via HTTPD service",
			nil, nil,
		),
		clientErrors: prometheus.NewDesc(
			"influxdb_httpd_client_errors_total",
			"Total number of client errors (4xx) from HTTPD service",
			nil, nil,
		),
		serverErrors: prometheus.NewDesc(
			"influxdb_httpd_server_errors_total",
			"Total number of server errors (5xx) from HTTPD service",
			nil, nil,
		),
	}
}

// Describe implements prometheus.Collector
func (c *InfluxDBCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.httpRequests
	ch <- c.httpRequestDuration
	ch <- c.activeRequests
	ch <- c.writeRequests
	ch <- c.queryRequests
	ch <- c.pointsWritten
	ch <- c.clientErrors
	ch <- c.serverErrors
}

// Collect implements prometheus.Collector
func (c *InfluxDBCollector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.RLock()
	stats := c.stats
	c.mutex.RUnlock()

	if stats == nil {
		return
	}

	// Collect HTTP request metrics
	ch <- prometheus.MustNewConstMetric(
		c.httpRequests, prometheus.CounterValue, float64(stats.GetRequests()), "total",
	)
	ch <- prometheus.MustNewConstMetric(
		c.httpRequestDuration, prometheus.CounterValue, float64(stats.GetRequestDuration()),
	)
	ch <- prometheus.MustNewConstMetric(
		c.activeRequests, prometheus.GaugeValue, float64(stats.GetActiveRequests()),
	)
	ch <- prometheus.MustNewConstMetric(
		c.writeRequests, prometheus.CounterValue, float64(stats.GetWriteRequests()),
	)
	ch <- prometheus.MustNewConstMetric(
		c.queryRequests, prometheus.CounterValue, float64(stats.GetQueryRequests()),
	)
	ch <- prometheus.MustNewConstMetric(
		c.pointsWritten, prometheus.CounterValue, float64(stats.GetPointsWrittenOK()),
	)
	ch <- prometheus.MustNewConstMetric(
		c.clientErrors, prometheus.CounterValue, float64(stats.GetClientErrors()),
	)
	ch <- prometheus.MustNewConstMetric(
		c.serverErrors, prometheus.CounterValue, float64(stats.GetServerErrors()),
	)
}

// HTTPDStatsAdapter adapts any statistics struct to the StatsProvider interface
type HTTPDStatsAdapter struct {
	statsValue reflect.Value
}

func (a *HTTPDStatsAdapter) GetRequests() int64        { return a.getField("Requests") }
func (a *HTTPDStatsAdapter) GetActiveRequests() int64  { return a.getField("ActiveRequests") }
func (a *HTTPDStatsAdapter) GetWriteRequests() int64   { return a.getField("WriteRequests") }
func (a *HTTPDStatsAdapter) GetQueryRequests() int64   { return a.getField("QueryRequests") }
func (a *HTTPDStatsAdapter) GetPointsWrittenOK() int64 { return a.getField("PointsWrittenOK") }
func (a *HTTPDStatsAdapter) GetClientErrors() int64    { return a.getField("ClientErrors") }
func (a *HTTPDStatsAdapter) GetServerErrors() int64    { return a.getField("ServerErrors") }
func (a *HTTPDStatsAdapter) GetRequestDuration() int64 { return a.getField("RequestDuration") }

func (a *HTTPDStatsAdapter) getField(fieldName string) int64 {
	field := a.statsValue.Elem().FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return atomic.LoadInt64((*int64)(unsafe.Pointer(field.Addr().Pointer())))
}

// RegisterInfluxDBCollector registers the InfluxDB metrics collector with Prometheus
// This function is safe to call multiple times - it will only register once
func RegisterInfluxDBCollector(stats interface{}) {
	collectorMutex.Lock()
	defer collectorMutex.Unlock()

	adapter := &HTTPDStatsAdapter{
		statsValue: reflect.ValueOf(stats),
	}

	// If we don't have a global collector yet, create and register it
	if globalCollector == nil {
		globalCollector = NewInfluxDBCollector(adapter)
		prometheus.MustRegister(globalCollector)
	} else {
		// Update the existing collector with new stats
		globalCollector.UpdateStats(adapter)
	}
}
