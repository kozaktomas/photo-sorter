package metrics

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

// dbPoolCollector exports sql.DB.Stats() as Prometheus gauges. Implementing
// prometheus.Collector directly (instead of pre-registering Gauges and
// scraping them on a tick) ensures every /metrics scrape sees the exact
// pool state at scrape time without an extra goroutine.
type dbPoolCollector struct {
	db *sql.DB

	open        *prometheus.Desc
	inUse       *prometheus.Desc
	idle        *prometheus.Desc
	waitCount   *prometheus.Desc
	waitSeconds *prometheus.Desc
	maxOpen     *prometheus.Desc
}

// newDBPoolCollector wires up the descriptors for the five sql.DBStats
// fields that matter operationally: open / in_use / idle for capacity,
// wait_count + wait_duration_seconds_total for saturation alerts, and
// max_open as a static reference value for ratio expressions.
func newDBPoolCollector(db *sql.DB) *dbPoolCollector {
	const subsystem = "db_pool"
	return &dbPoolCollector{
		db: db,
		open: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "open"),
			"Number of established database connections (in use + idle).",
			nil, nil,
		),
		inUse: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "in_use"),
			"Number of database connections currently in use by the application.",
			nil, nil,
		),
		idle: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "idle"),
			"Number of idle database connections in the pool.",
			nil, nil,
		),
		waitCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "wait_count_total"),
			"Total number of connections waited for since the pool was opened.",
			nil, nil,
		),
		waitSeconds: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "wait_duration_seconds_total"),
			"Total time blocked waiting for a connection, in seconds, since the pool was opened.",
			nil, nil,
		),
		maxOpen: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "max_open"),
			"Maximum number of open connections allowed by the pool.",
			nil, nil,
		),
	}
}

// Describe implements prometheus.Collector. We declare every descriptor up
// front so the registry can detect duplicate registrations early.
func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.open
	ch <- c.inUse
	ch <- c.idle
	ch <- c.waitCount
	ch <- c.waitSeconds
	ch <- c.maxOpen
}

// Collect implements prometheus.Collector. sql.DB.Stats() is a cheap
// snapshot — it just reads atomically updated counters — so calling it on
// every scrape is fine.
func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.db.Stats()
	ch <- prometheus.MustNewConstMetric(c.open, prometheus.GaugeValue, float64(stats.OpenConnections))
	ch <- prometheus.MustNewConstMetric(c.inUse, prometheus.GaugeValue, float64(stats.InUse))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(stats.Idle))
	ch <- prometheus.MustNewConstMetric(c.waitCount, prometheus.CounterValue, float64(stats.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitSeconds, prometheus.CounterValue, stats.WaitDuration.Seconds())
	ch <- prometheus.MustNewConstMetric(c.maxOpen, prometheus.GaugeValue, float64(stats.MaxOpenConnections))
}
