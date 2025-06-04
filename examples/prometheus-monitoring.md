# Prometheus Metrics for InfluxDB

This InfluxDB distribution includes comprehensive Prometheus metrics collection for monitoring your InfluxDB server performance and health.

## Overview

The Prometheus metrics endpoint is available at `/metrics` and provides detailed insights into:

- HTTP request metrics (request count, duration, active requests)
- Database operations (writes, queries, points written)
- Storage engine metrics (series count, memory usage, disk usage)
- TSM engine metrics (compactions)
- Cache metrics (hits, misses, size)
- System metrics (uptime, memory usage)
- Error tracking and authentication failures

## Accessing Metrics

The metrics endpoint is available at:
```
http://localhost:8086/metrics
```

If authentication is enabled, you'll need to provide credentials:
```bash
curl -u username:password http://localhost:8086/metrics
```

## Available Metrics

### HTTP Metrics

- `influxdb_http_requests_total` - Total number of HTTP requests by method, endpoint, and status code
- `influxdb_http_request_duration_seconds` - Duration of HTTP requests in seconds
- `influxdb_http_active_requests` - Number of currently active HTTP requests

### Database Write Metrics

- `influxdb_points_written_total` - Total number of points written by database, retention policy, and status (ok/fail)
- `influxdb_write_requests_total` - Total number of write requests by database and retention policy
- `influxdb_write_request_duration_seconds` - Duration of write requests in seconds

### Database Query Metrics

- `influxdb_query_requests_total` - Total number of query requests by database and query type
- `influxdb_query_request_duration_seconds` - Duration of query requests in seconds

### Storage Engine Metrics

- `influxdb_series_created_total` - Total number of series created by database
- `influxdb_series_count` - Current number of series in each database
- `influxdb_memory_usage_bytes` - Memory usage in bytes by component
- `influxdb_disk_usage_bytes` - Disk usage in bytes by database and path

### TSM Engine Metrics

- `influxdb_tsm_compactions_total` - Total number of TSM compactions by database and level
- `influxdb_tsm_compaction_duration_seconds` - Duration of TSM compactions in seconds

### Cache Metrics

- `influxdb_cache_size_bytes` - Current cache size in bytes by database and cache type
- `influxdb_cache_hits_total` - Total number of cache hits by database and cache type
- `influxdb_cache_misses_total` - Total number of cache misses by database and cache type

### WAL Metrics

- `influxdb_wal_writes_total` - Total number of WAL writes by database
- `influxdb_wal_bytes_written_total` - Total bytes written to WAL by database

### System and Error Metrics

- `influxdb_uptime_seconds` - Number of seconds since InfluxDB started
- `influxdb_info` - Information about the InfluxDB instance (version, commit, branch, build time)
- `influxdb_errors_total` - Total number of errors by type and component
- `influxdb_auth_failures_total` - Total number of authentication failures by method

### Shard and Continuous Query Metrics

- `influxdb_shard_count` - Current number of shards by database and retention policy
- `influxdb_continuous_queries_executed_total` - Total number of continuous queries executed

## Prometheus Configuration

Here's an example Prometheus configuration to scrape InfluxDB metrics:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'influxdb'
    static_configs:
      - targets: ['localhost:8086']
    metrics_path: '/metrics'
    scrape_interval: 30s
    # If authentication is enabled:
    # basic_auth:
    #   username: 'your-username'
    #   password: 'your-password'
```

## Grafana Dashboard

You can create comprehensive Grafana dashboards using these metrics. Here are some useful queries:

### HTTP Request Rate
```promql
rate(influxdb_http_requests_total[5m])
```

### Write Throughput (points/second)
```promql
rate(influxdb_points_written_total{status="ok"}[5m])
```

### Query Duration 95th Percentile
```promql
histogram_quantile(0.95, rate(influxdb_query_request_duration_seconds_bucket[5m]))
```

### Memory Usage
```promql
influxdb_memory_usage_bytes{component="heap_in_use"}
```

### Error Rate
```promql
rate(influxdb_errors_total[5m])
```

## Alerting Rules

Example Prometheus alerting rules:

```yaml
groups:
  - name: influxdb
    rules:
      - alert: InfluxDBHighErrorRate
        expr: rate(influxdb_errors_total[5m]) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "InfluxDB has high error rate"
          description: "InfluxDB error rate is {{ $value }} errors/second"

      - alert: InfluxDBHighMemoryUsage
        expr: influxdb_memory_usage_bytes{component="heap_in_use"} > 1e9
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "InfluxDB high memory usage"
          description: "InfluxDB heap usage is {{ $value | humanize }}B"

      - alert: InfluxDBSlowQueries
        expr: histogram_quantile(0.95, rate(influxdb_query_request_duration_seconds_bucket[5m])) > 30
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "InfluxDB slow queries detected"
          description: "95th percentile query duration is {{ $value }}s"
```

## Configuration

The metrics endpoint respects the InfluxDB authentication settings. If authentication is enabled in your InfluxDB configuration, you'll need to authenticate to access the metrics endpoint.

Example InfluxDB configuration:
```toml
[http]
  enabled = true
  bind-address = ":8086"
  auth-enabled = true
  # Enables authentication on the /ping, /metrics, and deprecated /status
  ping-auth-enabled = true
```

## Monitoring Best Practices

1. **Monitor Write Performance**: Track `influxdb_write_request_duration_seconds` and `influxdb_points_written_total` to ensure writes are performing well.

2. **Watch Memory Usage**: Monitor `influxdb_memory_usage_bytes` to prevent out-of-memory issues.

3. **Track Query Performance**: Use `influxdb_query_request_duration_seconds` to identify slow queries.

4. **Monitor Errors**: Set up alerts on `influxdb_errors_total` and `influxdb_auth_failures_total`.

5. **Storage Health**: Track `influxdb_series_count` and `influxdb_tsm_compactions_total` to monitor storage engine health.

6. **System Resources**: Monitor `influxdb_uptime_seconds` and overall system health.

The metrics are updated in real-time for HTTP operations and every 30 seconds for system metrics, providing comprehensive monitoring capabilities for your InfluxDB deployment. 