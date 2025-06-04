package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP Metrics
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_http_requests_total",
			Help: "Total number of HTTP requests processed by InfluxDB server",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "influxdb_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	httpActiveRequests = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_http_active_requests",
			Help: "Number of currently active HTTP requests",
		},
		[]string{"endpoint"},
	)

	// Database Write Metrics
	pointsWrittenTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_points_written_total",
			Help: "Total number of points written to InfluxDB",
		},
		[]string{"database", "retention_policy", "status"},
	)

	writeRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_write_requests_total",
			Help: "Total number of write requests to InfluxDB",
		},
		[]string{"database", "retention_policy"},
	)

	writeRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "influxdb_write_request_duration_seconds",
			Help:    "Duration of write requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"database", "retention_policy"},
	)

	// Database Query Metrics
	queryRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_query_requests_total",
			Help: "Total number of query requests to InfluxDB",
		},
		[]string{"database", "query_type"},
	)

	queryRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "influxdb_query_request_duration_seconds",
			Help:    "Duration of query requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"database", "query_type"},
	)

	// Storage Engine Metrics
	seriesCreated = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_series_created_total",
			Help: "Total number of series created",
		},
		[]string{"database"},
	)

	seriesCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_series_count",
			Help: "Current number of series in the database",
		},
		[]string{"database"},
	)

	memoryUsageBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_memory_usage_bytes",
			Help: "Memory usage in bytes by component",
		},
		[]string{"component"},
	)

	diskUsageBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_disk_usage_bytes",
			Help: "Disk usage in bytes by database",
		},
		[]string{"database", "path"},
	)

	// WAL Metrics
	walWrites = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_wal_writes_total",
			Help: "Total number of WAL writes",
		},
		[]string{"database"},
	)

	walBytesWritten = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_wal_bytes_written_total",
			Help: "Total bytes written to WAL",
		},
		[]string{"database"},
	)

	// TSM Engine Metrics
	tsmCompactions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_tsm_compactions_total",
			Help: "Total number of TSM compactions",
		},
		[]string{"database", "level"},
	)

	tsmCompactionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "influxdb_tsm_compaction_duration_seconds",
			Help:    "Duration of TSM compactions in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		},
		[]string{"database", "level"},
	)

	// Cache Metrics
	cacheSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_cache_size_bytes",
			Help: "Current cache size in bytes",
		},
		[]string{"database", "cache_type"},
	)

	cacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_cache_hits_total",
			Help: "Total number of cache hits",
		},
		[]string{"database", "cache_type"},
	)

	cacheMisses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_cache_misses_total",
			Help: "Total number of cache misses",
		},
		[]string{"database", "cache_type"},
	)

	// Continuous Query Metrics
	continuousQueriesExecuted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_continuous_queries_executed_total",
			Help: "Total number of continuous queries executed",
		},
		[]string{"database", "status"},
	)

	// Shard Metrics
	shardCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_shard_count",
			Help: "Current number of shards",
		},
		[]string{"database", "retention_policy"},
	)

	// Authentication Metrics
	authFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_auth_failures_total",
			Help: "Total number of authentication failures",
		},
		[]string{"method"},
	)

	// Error Metrics
	errorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "influxdb_errors_total",
			Help: "Total number of errors by type",
		},
		[]string{"error_type", "component"},
	)

	// System Info
	info = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "influxdb_info",
			Help: "Information about the InfluxDB instance",
		},
		[]string{"version", "commit", "branch", "build_time"},
	)

	uptimeSeconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "influxdb_uptime_seconds",
			Help: "Number of seconds since InfluxDB started",
		},
	)
)

var (
	startTime time.Time
	once      sync.Once
)

func init() {
	once.Do(func() {
		startTime = time.Now()
	})
}

// Collectors returns all Prometheus collectors for InfluxDB metrics
func Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		httpRequestsTotal,
		httpRequestDuration,
		httpActiveRequests,
		pointsWrittenTotal,
		writeRequestsTotal,
		writeRequestDuration,
		queryRequestsTotal,
		queryRequestDuration,
		seriesCreated,
		seriesCount,
		memoryUsageBytes,
		diskUsageBytes,
		walWrites,
		walBytesWritten,
		tsmCompactions,
		tsmCompactionDuration,
		cacheSize,
		cacheHits,
		cacheMisses,
		continuousQueriesExecuted,
		shardCount,
		authFailures,
		errorsTotal,
		info,
		uptimeSeconds,
	}
}

// HTTP metric helpers
func RecordHTTPRequest(method, endpoint, statusCode string, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, endpoint, statusCode).Inc()
	httpRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

func IncActiveHTTPRequests(endpoint string) {
	httpActiveRequests.WithLabelValues(endpoint).Inc()
}

func DecActiveHTTPRequests(endpoint string) {
	httpActiveRequests.WithLabelValues(endpoint).Dec()
}

// Write metric helpers
func RecordPointsWritten(database, retentionPolicy, status string, count int64) {
	pointsWrittenTotal.WithLabelValues(database, retentionPolicy, status).Add(float64(count))
}

func RecordWriteRequest(database, retentionPolicy string, duration time.Duration) {
	writeRequestsTotal.WithLabelValues(database, retentionPolicy).Inc()
	writeRequestDuration.WithLabelValues(database, retentionPolicy).Observe(duration.Seconds())
}

// Query metric helpers
func RecordQueryRequest(database, queryType string, duration time.Duration) {
	queryRequestsTotal.WithLabelValues(database, queryType).Inc()
	queryRequestDuration.WithLabelValues(database, queryType).Observe(duration.Seconds())
}

// Storage metric helpers
func SetSeriesCount(database string, count int64) {
	seriesCount.WithLabelValues(database).Set(float64(count))
}

func IncSeriesCreated(database string, count int64) {
	seriesCreated.WithLabelValues(database).Add(float64(count))
}

func SetMemoryUsage(component string, bytes int64) {
	memoryUsageBytes.WithLabelValues(component).Set(float64(bytes))
}

func SetDiskUsage(database, path string, bytes int64) {
	diskUsageBytes.WithLabelValues(database, path).Set(float64(bytes))
}

// WAL metric helpers
func IncWALWrites(database string, count int64) {
	walWrites.WithLabelValues(database).Add(float64(count))
}

func IncWALBytesWritten(database string, bytes int64) {
	walBytesWritten.WithLabelValues(database).Add(float64(bytes))
}

// TSM metric helpers
func RecordTSMCompaction(database, level string, duration time.Duration) {
	tsmCompactions.WithLabelValues(database, level).Inc()
	tsmCompactionDuration.WithLabelValues(database, level).Observe(duration.Seconds())
}

// Cache metric helpers
func SetCacheSize(database, cacheType string, bytes int64) {
	cacheSize.WithLabelValues(database, cacheType).Set(float64(bytes))
}

func IncCacheHits(database, cacheType string) {
	cacheHits.WithLabelValues(database, cacheType).Inc()
}

func IncCacheMisses(database, cacheType string) {
	cacheMisses.WithLabelValues(database, cacheType).Inc()
}

// CQ metric helpers
func IncContinuousQueriesExecuted(database, status string) {
	continuousQueriesExecuted.WithLabelValues(database, status).Inc()
}

// Shard metric helpers
func SetShardCount(database, retentionPolicy string, count int64) {
	shardCount.WithLabelValues(database, retentionPolicy).Set(float64(count))
}

// Auth metric helpers
func IncAuthFailures(method string) {
	authFailures.WithLabelValues(method).Inc()
}

// Error metric helpers
func IncErrors(errorType, component string) {
	errorsTotal.WithLabelValues(errorType, component).Inc()
}

// System metric helpers
func SetInfo(version, commit, branch, buildTime string) {
	info.WithLabelValues(version, commit, branch, buildTime).Set(1)
}

func UpdateUptime() {
	uptimeSeconds.Set(time.Since(startTime).Seconds())
}
