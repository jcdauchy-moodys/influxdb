package exportparquet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/apache/arrow/go/v16/arrow"
	"github.com/apache/arrow/go/v16/arrow/array"
	"github.com/apache/arrow/go/v16/parquet"
	"github.com/apache/arrow/go/v16/parquet/compress"
	"github.com/apache/arrow/go/v16/parquet/pqarrow"
	"go.uber.org/zap"

	"github.com/influxdata/flux/memory"
	"github.com/influxdata/influxdb/models"
	internal_errors "github.com/influxdata/influxdb/pkg/errors"
	"github.com/influxdata/influxdb/services/meta"
	"github.com/influxdata/influxdb/tsdb"
	"github.com/influxdata/influxql"
)

type config struct {
	Database        string
	RP              string
	Measurements    string
	StartTime       time.Time
	EndTime         time.Time
	TypeResolutions string
	NameResolutions string
	Output          string
	FilenamePattern string
	Compression     string
}

type exporter struct {
	client MetaClient
	store  *tsdb.Store

	// Input selection
	db, rp       string
	measurements []string

	// Time range filtering
	filterStartTime time.Time
	filterEndTime   time.Time

	// Output file settings
	path            string
	filenamePattern string
	filenames       map[string]int
	compression     compress.Compression

	// Source data and corresponding information
	groups    []meta.ShardGroupInfo
	startDate time.Time
	endDate   time.Time
	shards    []*tsdb.Shard

	// TSM and WAL file paths per shard
	tsmFiles map[uint64][]string
	walFiles map[uint64][]string

	// Schema information and resolutions
	schemata        map[string]*schemaCreator
	typeResolutions map[string]map[string]influxql.DataType
	nameResolutions map[string]map[string]string

	// Parquet metadata information
	exportStart time.Time

	// Export statistics
	stats struct {
		sync.Mutex
		measurementRows  map[string]int64 // rows per measurement
		measurementFiles map[string]int   // files per measurement
		totalRows        int64
		totalFiles       int
	}

	logger *zap.SugaredLogger
}

func newExporter(server *inspectServer, cfg *config, logger *zap.Logger) (*exporter, error) {
	client := server.MetaClient()

	// If meta client is available, validate database and retention policy
	if client != nil {
		db := client.Database(cfg.Database)
		if db == nil {
			logger.Sugar().Warnf("Database %q not found in metadata, proceeding anyway", cfg.Database)
		} else {
			if cfg.RP == "" {
				cfg.RP = db.DefaultRetentionPolicy
			}

			rp, err := client.RetentionPolicy(cfg.Database, cfg.RP)
			if rp == nil || err != nil {
				logger.Sugar().Warnf("Retention policy %q not found in metadata, proceeding anyway", cfg.RP)
			}
		}
	}

	// If no RP specified and no meta client, use "autogen" as default
	if cfg.RP == "" {
		cfg.RP = "autogen"
		logger.Sugar().Infof("No retention policy specified, using default: %q", cfg.RP)
	}

	store := tsdb.NewStore(server.TSDBConfig().Dir)
	if server.Logger() != nil {
		store.WithLogger(server.Logger())
	}
	store.EngineOptions.MonitorDisabled = true
	store.EngineOptions.CompactionDisabled = true
	store.EngineOptions.Config = server.TSDBConfig()
	store.EngineOptions.EngineVersion = server.TSDBConfig().Engine
	store.EngineOptions.IndexVersion = server.TSDBConfig().Index
	store.EngineOptions.DatabaseFilter = func(database string) bool {
		return database == cfg.Database
	}
	store.EngineOptions.RetentionPolicyFilter = func(_, rpolicy string) bool {
		return rpolicy == cfg.RP
	}

	// Set default filename pattern if not specified
	filenamePattern := cfg.FilenamePattern
	if filenamePattern == "" {
		filenamePattern = "{db}#{measurement}#{rp}#{start}#{end}.parquet"
	}

	// Parse compression type
	compression := compress.Codecs.Snappy // Default
	if cfg.Compression != "" {
		switch strings.ToLower(cfg.Compression) {
		case "uncompressed", "none":
			compression = compress.Codecs.Uncompressed
		case "snappy":
			compression = compress.Codecs.Snappy
		case "gzip":
			compression = compress.Codecs.Gzip
		case "lzo":
			compression = compress.Codecs.Lzo
		case "brotli":
			compression = compress.Codecs.Brotli
		case "lz4":
			compression = compress.Codecs.Lz4
		case "zstd":
			compression = compress.Codecs.Zstd
		default:
			return nil, fmt.Errorf("invalid compression type %q, supported: uncompressed, snappy, gzip, lzo, brotli, lz4, zstd", cfg.Compression)
		}
	}
	logger.Sugar().Infof("Using compression: %s", compression)

	// Create the exporter
	e := &exporter{
		client:          client,
		store:           store,
		db:              cfg.Database,
		rp:              cfg.RP,
		filterStartTime: cfg.StartTime,
		filterEndTime:   cfg.EndTime,
		path:            cfg.Output,
		filenamePattern: filenamePattern,
		compression:     compression,
		typeResolutions: make(map[string]map[string]influxql.DataType),
		nameResolutions: make(map[string]map[string]string),
		filenames:       make(map[string]int),
		tsmFiles:        make(map[uint64][]string),
		walFiles:        make(map[uint64][]string),
		logger:          logger.Sugar().Named("exporter"),
	}

	// Initialize statistics
	e.stats.measurementRows = make(map[string]int64)
	e.stats.measurementFiles = make(map[string]int)

	// Split the given measurements
	if cfg.Measurements != "" && cfg.Measurements != "*" {
		e.measurements = strings.Split(cfg.Measurements, ",")
	}

	// Prepare type resolutions
	if cfg.TypeResolutions != "" {
		for _, r := range strings.Split(cfg.TypeResolutions, ",") {
			field, ftype, found := strings.Cut(r, "=")
			if !found {
				return nil, fmt.Errorf("invalid format in type conflict resolution %q", r)
			}
			measurement, field, found := strings.Cut(field, ".")
			if !found {
				return nil, fmt.Errorf("invalid measurement in type conflict resolution %q", r)
			}
			if _, exists := e.typeResolutions[measurement]; !exists {
				e.typeResolutions[measurement] = make(map[string]influxql.DataType)
			}

			switch strings.ToLower(ftype) {
			case "float":
				e.typeResolutions[measurement][field] = influxql.Float
			case "int":
				e.typeResolutions[measurement][field] = influxql.Integer
			case "uint":
				e.typeResolutions[measurement][field] = influxql.Unsigned
			case "bool":
				e.typeResolutions[measurement][field] = influxql.Boolean
			case "string":
				e.typeResolutions[measurement][field] = influxql.String
			default:
				return nil, fmt.Errorf("invalid type in conflict resolution %q", r)
			}
		}
	}

	// Prepare name resolutions
	if cfg.NameResolutions != "" {
		for _, r := range strings.Split(cfg.NameResolutions, ",") {
			field, name, found := strings.Cut(r, "=")
			if !found {
				return nil, fmt.Errorf("invalid format in name conflict resolution %q", r)
			}
			measurement, field, found := strings.Cut(field, ".")
			if !found {
				return nil, fmt.Errorf("invalid measurement in name conflict resolution %q", r)
			}
			if _, exists := e.nameResolutions[measurement]; !exists {
				e.nameResolutions[measurement] = make(map[string]string)
			}
			e.nameResolutions[measurement][field] = name
		}
	}
	return e, nil
}

func (e *exporter) open(ctx context.Context) error {
	if err := e.store.Open(); err != nil {
		return err
	}

	// Determine time range for shard filtering
	min := models.MinNanoTime
	max := models.MaxNanoTime
	if !e.filterStartTime.IsZero() {
		min = e.filterStartTime.UnixNano()
		e.logger.Infof("Filtering data from: %s", e.filterStartTime.Format(time.RFC3339))
	}
	if !e.filterEndTime.IsZero() {
		max = e.filterEndTime.UnixNano()
		e.logger.Infof("Filtering data to: %s", e.filterEndTime.Format(time.RFC3339))
	}

	// Try to use meta client if available, otherwise discover shards from filesystem
	if e.client != nil {
		// First, list all shard groups to help diagnose time range issues
		allGroups, err := e.client.NodeShardGroupsByTimeRange(e.db, e.rp, time.Unix(0, 0).UnixNano(), time.Now().Add(365*24*time.Hour).UnixNano())
		if err == nil && len(allGroups) > 0 {
			e.logger.Debugf("All shard groups in %s/%s:", e.db, e.rp)
			for _, sg := range allGroups {
				e.logger.Debugf("  Shard Group ID=%d, StartTime=%s, EndTime=%s, Shards=%v",
					sg.ID, sg.StartTime.Format(time.RFC3339), sg.EndTime.Format(time.RFC3339), len(sg.Shards))
			}
		}

		// Determine all shard groups in the database within the time range
		groups, err := e.client.NodeShardGroupsByTimeRange(e.db, e.rp, min, max)
		if err != nil {
			e.logger.Warnf("Could not get shard groups from metadata: %v, falling back to filesystem discovery", err)
			if err := e.discoverShardsFromFilesystem(); err != nil {
				return err
			}
		} else {
			if len(groups) == 0 {
				e.logger.Warnf("No shard groups found in metadata for time range %s to %s",
					time.Unix(0, min).Format(time.RFC3339), time.Unix(0, max).Format(time.RFC3339))
				e.logger.Warnf("Falling back to filesystem discovery to scan all shards")
				if err := e.discoverShardsFromFilesystem(); err != nil {
					return err
				}
			} else {
				sort.Sort(meta.ShardGroupInfos(groups))
				e.startDate = groups[0].StartTime
				e.endDate = groups[len(groups)-1].EndTime
				e.groups = groups

				// Log the actual data range being exported
				e.logger.Infof("Found %d shard group(s) covering %s to %s", len(groups), e.startDate.Format(time.RFC3339), e.endDate.Format(time.RFC3339))

				// Collect all shards
				for _, grp := range groups {
					ids := make([]uint64, 0, len(grp.Shards))
					for _, s := range grp.Shards {
						ids = append(ids, s.ID)
					}
					e.shards = append(e.shards, e.store.Shards(ids)...)
				}
			}
		}
	} else {
		// No meta client, discover shards from filesystem
		e.logger.Infof("No metadata available, discovering shards from filesystem")
		if err := e.discoverShardsFromFilesystem(); err != nil {
			return err
		}
	}

	if len(e.shards) == 0 {
		e.logger.Infof("No shards found")
		return nil
	}

	// Discover TSM and WAL files for each shard
	if err := e.discoverTSMFiles(); err != nil {
		return fmt.Errorf("discovering TSM files failed: %w", err)
	}
	if err := e.discoverWALFiles(); err != nil {
		return fmt.Errorf("discovering WAL files failed: %w", err)
	}

	// Determine all measurements in all shards
	if len(e.measurements) == 0 {
		measurements := make(map[string]bool)
		for _, shard := range e.shards {
			if err := shard.ForEachMeasurementName(func(name []byte) error {
				measurements[string(name)] = true
				return nil
			}); err != nil {
				return fmt.Errorf("getting measurement names failed: %w", err)
			}
		}
		for m := range measurements {
			e.measurements = append(e.measurements, m)
		}
	}
	sort.Strings(e.measurements)

	// Collect the schemata for all measurments
	e.schemata = make(map[string]*schemaCreator, len(e.measurements))
	for _, m := range e.measurements {
		creator := &schemaCreator{
			measurement:     m,
			shards:          e.shards,
			series:          make(map[uint64][]seriesEntry, len(e.shards)),
			typeResolutions: e.typeResolutions[m],
			nameResolutions: e.nameResolutions[m],
		}
		if err := creator.extractSchema(ctx); err != nil {
			return fmt.Errorf("extracting schema for measurement %q failed: %w", m, err)
		}
		e.schemata[m] = creator
	}

	return nil
}

func (e *exporter) close() error {
	return e.store.Close()
}

func (e *exporter) discoverShardsFromFilesystem() error {
	dataDir := e.store.EngineOptions.Config.Dir
	dbRPPath := filepath.Join(dataDir, e.db, e.rp)

	// Check if the path exists
	if _, err := os.Stat(dbRPPath); os.IsNotExist(err) {
		return fmt.Errorf("database/retention policy path does not exist: %s", dbRPPath)
	}

	// Walk the directory to find shard directories
	shardIDs := make(map[uint64]bool)
	err := filepath.Walk(dbRPPath, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for shard directories (they are numeric IDs)
		if f.IsDir() && path != dbRPPath {
			shardIDStr := filepath.Base(path)
			shardID, err := strconv.ParseUint(shardIDStr, 10, 64)
			if err != nil {
				// Not a valid shard directory, skip
				return filepath.SkipDir
			}
			shardIDs[shardID] = true
			return filepath.SkipDir
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walking data directory failed: %w", err)
	}

	if len(shardIDs) == 0 {
		return fmt.Errorf("no shards found in %s", dbRPPath)
	}

	// Convert to slice and get shards from store
	ids := make([]uint64, 0, len(shardIDs))
	for id := range shardIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	e.shards = e.store.Shards(ids)
	e.logger.Infof("Discovered %d shard(s) from filesystem", len(e.shards))

	// Set default date range if not specified
	if e.filterStartTime.IsZero() {
		e.startDate = time.Unix(0, models.MinNanoTime)
	} else {
		e.startDate = e.filterStartTime
	}
	if e.filterEndTime.IsZero() {
		e.endDate = time.Unix(0, models.MaxNanoTime)
	} else {
		e.endDate = e.filterEndTime
	}

	return nil
}

func (e *exporter) discoverTSMFiles() error {
	dataDir := e.store.EngineOptions.Config.Dir
	dbRPPath := filepath.Join(dataDir, e.db, e.rp)

	return filepath.Walk(dbRPPath, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if this is a TSM file
		if filepath.Ext(path) != ".tsm" {
			return nil
		}

		// Extract shard ID from path
		shardDir := filepath.Dir(path)
		shardIDStr := filepath.Base(shardDir)
		shardID, err := strconv.ParseUint(shardIDStr, 10, 64)
		if err != nil {
			// Not a valid shard directory, skip
			return nil
		}

		// Check if this shard is in our list
		found := false
		for _, shard := range e.shards {
			if shard.ID() == shardID {
				found = true
				break
			}
		}
		if !found {
			return nil
		}

		e.tsmFiles[shardID] = append(e.tsmFiles[shardID], path)
		return nil
	})
}

func (e *exporter) discoverWALFiles() error {
	walDir := e.store.EngineOptions.Config.WALDir
	dbRPPath := filepath.Join(walDir, e.db, e.rp)

	return filepath.Walk(dbRPPath, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if this is a WAL file
		fileName := filepath.Base(path)
		if filepath.Ext(path) != ".wal" || !strings.HasPrefix(fileName, "_") {
			return nil
		}

		// Extract shard ID from path
		shardDir := filepath.Dir(path)
		shardIDStr := filepath.Base(shardDir)
		shardID, err := strconv.ParseUint(shardIDStr, 10, 64)
		if err != nil {
			// Not a valid shard directory, skip
			return nil
		}

		// Check if this shard is in our list
		found := false
		for _, shard := range e.shards {
			if shard.ID() == shardID {
				found = true
				break
			}
		}
		if !found {
			return nil
		}

		e.walFiles[shardID] = append(e.walFiles[shardID], path)
		return nil
	})
}

func (e *exporter) printPlan(w io.Writer) {
	tw := tabwriter.NewWriter(w, 10, 8, 1, '\t', 0)

	if !e.filterStartTime.IsZero() || !e.filterEndTime.IsZero() {
		fmt.Fprintf(w, "Time filter: ")
		if !e.filterStartTime.IsZero() {
			fmt.Fprintf(w, "from %s ", e.filterStartTime.Format(time.RFC3339))
		}
		if !e.filterEndTime.IsZero() {
			fmt.Fprintf(w, "to %s ", e.filterEndTime.Format(time.RFC3339))
		}
		fmt.Fprintln(w)
	}

	if len(e.groups) > 0 {
		fmt.Fprintf(w, "Exporting source data from %s to %s in %d shard group(s):\n", e.startDate, e.endDate, len(e.groups))
		fmt.Fprintln(tw, "  Group\tStart\tEnd\t#Shards")
		fmt.Fprintln(tw, "  -----\t-----\t---\t-------")
		for _, g := range e.groups {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%d\n", g.ID, g.StartTime, g.EndTime, len(g.Shards))
		}
		fmt.Fprintln(tw)
	} else {
		fmt.Fprintf(w, "Exporting %d shard(s) from %s to %s\n", len(e.shards), e.startDate.Format(time.RFC3339), e.endDate.Format(time.RFC3339))
		fmt.Fprintln(tw, "  Shard")
		fmt.Fprintln(tw, "  -----")
		for _, s := range e.shards {
			fmt.Fprintf(tw, "  %d\n", s.ID())
		}
		fmt.Fprintln(tw)
	}

	fmt.Fprintf(w, "Creating the following schemata for %d measurement(s):\n", len(e.measurements))
	for _, measurement := range e.measurements {
		creator := e.schemata[measurement]
		hasConflicts, errs := creator.validate()
		if len(errs) > 0 {
			fmt.Fprintf(
				w,
				"! Measurement %q with conflict(s) in %d tag(s), %d field(s):\n",
				measurement,
				len(creator.tags),
				len(creator.fieldKeys),
			)
		} else if hasConflicts {
			fmt.Fprintf(
				w,
				"* Measurement %q with resolved conflicts in %d tag(s), %d field(s):\n",
				measurement,
				len(creator.tags),
				len(creator.fieldKeys),
			)
		} else {
			fmt.Fprintf(
				w,
				"  Measurement %q with %d tag(s) and  %d field(s):\n",
				measurement,
				len(creator.tags),
				len(creator.fieldKeys),
			)

		}
		fmt.Fprintln(tw, "    Column\tKind\tDatatype")
		fmt.Fprintln(tw, "    ------\t----\t--------")
		fmt.Fprintln(tw, "    time\ttimestamp\ttimestamp (nanosecond)")
		for _, name := range creator.tags {
			fmt.Fprintf(tw, "    %s\ttag\tstring\n", name)
		}
		for _, name := range creator.fieldKeys {
			ftype := creator.fields[name].String()
			if types, found := creator.conflicts[name]; found {
				parts := make([]string, 0, len(types))
				for _, t := range types {
					parts = append(parts, t.String())
				}
				ftype = strings.Join(parts, "|")
				if rftype, found := creator.typeResolutions[name]; found {
					ftype += " -> " + rftype.String()
				}
			}
			fname := name
			if n, found := creator.nameResolutions[name]; found {
				fname += " -> " + n
			}
			fmt.Fprintf(tw, "    %s\tfield\t%s\n", fname, ftype)
		}
		for _, err := range errs {
			fmt.Fprintln(tw, " ", err)
		}

		// Show warnings for auto-resolved name conflicts
		if len(creator.autoResolved) > 0 {
			fmt.Fprintln(tw, "    WARNING: Auto-resolved tag/field name conflicts:")
			for _, resolution := range creator.autoResolved {
				fmt.Fprintf(tw, "      %s\n", resolution)
			}
		}

		fmt.Fprintln(tw)
	}

	tw.Flush()
}

func (e *exporter) export(ctx context.Context) error {
	// Check if the schema has unresolved conflicts and log auto-resolutions
	for m, s := range e.schemata {
		if _, errs := s.validate(); len(errs) > 0 {
			err := errors.Join(errs...)
			return fmt.Errorf("%w in schema of measurement %q", err, m)
		}

		// Log warnings for auto-resolved name conflicts
		if len(s.autoResolved) > 0 {
			e.logger.Warnf("Measurement %q: Auto-resolved tag/field name conflicts", m)
			for _, resolution := range s.autoResolved {
				e.logger.Warnf("  %s", resolution)
			}
		}
	}

	// Create the export directory if it doesn't exist
	if err := os.MkdirAll(e.path, 0700); err != nil {
		return fmt.Errorf("creating directory %q failed: %w", e.path, err)
	}

	// Export shards in parallel
	e.exportStart = time.Now()
	e.logger.Info("Starting export...")

	// Use number of CPUs for parallelism
	numWorkers := runtime.NumCPU()
	if numWorkers > len(e.shards) {
		numWorkers = len(e.shards)
	}
	e.logger.Infof("Using %d parallel workers for %d shard(s)", numWorkers, len(e.shards))

	// Create worker pool
	type shardJob struct {
		shard *tsdb.Shard
		index int
	}
	jobs := make(chan shardJob, len(e.shards))
	errChan := make(chan error, len(e.shards))

	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobs {
				start := time.Now()
				e.logger.Infof("[Worker %d] Starting export of shard %d (%d/%d)...",
					workerID, job.shard.ID(), job.index+1, len(e.shards))

				for _, m := range e.measurements {
					if err := e.exportMeasurement(ctx, job.shard, m); err != nil {
						path := "unknown"
						if f, serr := job.shard.SeriesFile(); serr != nil {
							e.logger.Errorf("determining series file failed: %v", serr)
						} else {
							path = f.Path()
						}
						errChan <- fmt.Errorf("exporting measurement %q in shard %d at %q failed: %w", m, job.shard.ID(), path, err)
						return
					}
				}
				e.logger.Infof("[Worker %d] Finished export of shard %d in %v",
					workerID, job.shard.ID(), time.Since(start))
			}
		}(w)
	}

	// Queue all shards
	for i, shard := range e.shards {
		jobs <- shardJob{shard: shard, index: i}
	}
	close(jobs)

	// Wait for all workers
	wg.Wait()
	close(errChan)

	// Check for errors
	if err := <-errChan; err != nil {
		return err
	}

	e.logger.Infof("Finished export in %v", time.Since(e.exportStart))

	// Print export summary
	e.printSummary()

	return nil
}

func (e *exporter) printSummary() {
	e.logger.Info("=" + strings.Repeat("=", 70))
	e.logger.Infof("Export Summary for %s/%s", e.db, e.rp)
	e.logger.Info("=" + strings.Repeat("=", 70))

	if e.stats.totalRows == 0 {
		e.logger.Warn("No data exported")
		return
	}

	e.logger.Infof("Total: %s rows in %d file(s)", formatNumber(e.stats.totalRows), e.stats.totalFiles)
	e.logger.Info("")

	// Sort measurements by name for consistent output
	measurements := make([]string, 0, len(e.stats.measurementRows))
	for m := range e.stats.measurementRows {
		measurements = append(measurements, m)
	}
	sort.Strings(measurements)

	e.logger.Info("Per Measurement:")
	for _, m := range measurements {
		rows := e.stats.measurementRows[m]
		files := e.stats.measurementFiles[m]
		percentage := float64(rows) / float64(e.stats.totalRows) * 100
		e.logger.Infof("  %-40s %12s rows in %3d file(s) (%5.1f%%)",
			m, formatNumber(rows), files, percentage)
	}
	e.logger.Info("=" + strings.Repeat("=", 70))
}

func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	// Add thousands separators
	if len(s) <= 3 {
		return s
	}

	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}

func (e *exporter) exportMeasurement(ctx context.Context, shard *tsdb.Shard, measurement string) (err error) {
	startMeasurement := time.Now()

	// Get the cumulative scheme with all tags and fields for the measurement
	creator, found := e.schemata[measurement]
	if !found {
		return errors.New("no schema creator found")
	}

	if len(creator.fieldKeys) == 0 {
		e.logger.Debugf("  Skipping measurement %q without fields", measurement)
		return nil
	}

	// Determine time range for this batch
	startTime := models.MinNanoTime
	endTime := models.MaxNanoTime
	if !e.filterStartTime.IsZero() {
		startTime = e.filterStartTime.UnixNano()
	}
	if !e.filterEndTime.IsZero() {
		endTime = e.filterEndTime.UnixNano()
	}

	// Get TSM and WAL files for this shard
	tsmFiles := e.tsmFiles[shard.ID()]
	walFiles := e.walFiles[shard.ID()]

	// Sort files to ensure consistent ordering
	sort.Strings(tsmFiles)
	sort.Strings(walFiles)

	// Create a batch processor
	batcher := &batcher{
		measurement:     []byte(measurement),
		shard:           shard,
		tsmFiles:        tsmFiles,
		walFiles:        walFiles,
		series:          creator.series[shard.ID()],
		typeResolutions: creator.typeResolutions,
		nameResolutions: creator.nameResolutions,
		start:           startTime,
		end:             endTime,
		logger:          e.logger.Named("batcher"),
	}
	if err := batcher.init(); err != nil {
		return fmt.Errorf("creating batcher failed: %w", err)
	}

	e.logger.Infof("  Exporting measurement %q...", measurement)

	// Read all data from TSM and WAL files
	rows, err := batcher.next(ctx)
	if err != nil {
		return fmt.Errorf("reading data failed: %w", err)
	}
	if len(rows) == 0 {
		e.logger.Debugf("  Skipping measurement %q without data", measurement)
		return nil
	}

	// Track statistics (thread-safe)
	e.stats.Lock()
	e.stats.measurementRows[measurement] += int64(len(rows))
	e.stats.measurementFiles[measurement]++
	e.stats.totalRows += int64(len(rows))
	e.stats.totalFiles++
	e.stats.Unlock()

	// Create a parquet schema for writing the data
	metadata := map[string]string{
		"export":      e.exportStart.Format(time.RFC3339),
		"database":    e.db,
		"retention":   e.rp,
		"measurement": measurement,
		"shard":       strconv.FormatUint(shard.ID(), 10),
		"start_time":  e.startDate.Format(time.RFC3339Nano),
		"end_time":    e.endDate.Format(time.RFC3339Nano),
	}
	schema, err := creator.schema(metadata)
	if err != nil {
		return fmt.Errorf("creating arrow schema failed: %w", err)
	}

	// Create parquet file using the filename pattern
	filename := e.generateFilename(measurement, shard.ID())
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("creating file %q failed: %w", filename, err)
	}

	writer, err := pqarrow.NewFileWriter(
		schema,
		file,
		parquet.NewWriterProperties(
			parquet.WithCreatedBy("influx_inspect"),
			parquet.WithCompression(e.compression),
		),
		pqarrow.NewArrowWriterProperties(pqarrow.WithCoerceTimestamps(arrow.Nanosecond)),
	)
	if err != nil {
		if err := file.Close(); err != nil {
			e.logger.Errorf("closing file failed: %v", err)
		}
		return fmt.Errorf("creating parquet writer for file %q failed: %w", filename, err)
	}
	defer internal_errors.Capture(&err, writer.Close)()

	// Prepare the record builder
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()

	// Convert the data to an arrow representation
	record := e.convertData(rows, builder, creator.tags, creator.fieldKeys)

	// Write data
	if err := writer.Write(record); err != nil {
		return fmt.Errorf("writing parquet file %q failed: %w", filename, err)
	}

	e.logger.Infof(
		"  exported %d rows of measurement %q to %q in %v...",
		len(rows),
		measurement,
		filename,
		time.Since(startMeasurement),
	)

	return nil
}

func (e *exporter) generateFilename(measurement string, shardID uint64) string {
	// Get sequence number for this measurement
	seq := e.filenames[measurement]
	e.filenames[measurement]++

	// Format timestamps for filename (filesystem-safe format)
	startStr := ""
	endStr := ""
	if !e.filterStartTime.IsZero() {
		startStr = e.filterStartTime.Format("2006-01-02T15-04-05Z")
	} else {
		startStr = e.startDate.Format("2006-01-02T15-04-05Z")
	}
	if !e.filterEndTime.IsZero() {
		endStr = e.filterEndTime.Format("2006-01-02T15-04-05Z")
	} else {
		endStr = e.endDate.Format("2006-01-02T15-04-05Z")
	}

	// Sanitize values for filesystem safety
	safeDB := sanitizeFilename(e.db)
	safeRP := sanitizeFilename(e.rp)
	safeMeasurement := sanitizeFilename(measurement)

	// Replace placeholders in pattern
	filename := e.filenamePattern
	filename = strings.ReplaceAll(filename, "{db}", safeDB)
	filename = strings.ReplaceAll(filename, "{database}", safeDB)
	filename = strings.ReplaceAll(filename, "{measurement}", safeMeasurement)
	filename = strings.ReplaceAll(filename, "{rp}", safeRP)
	filename = strings.ReplaceAll(filename, "{retention}", safeRP)
	filename = strings.ReplaceAll(filename, "{start}", startStr)
	filename = strings.ReplaceAll(filename, "{end}", endStr)
	filename = strings.ReplaceAll(filename, "{shard}", fmt.Sprintf("%d", shardID))
	filename = strings.ReplaceAll(filename, "{seq}", fmt.Sprintf("%05d", seq))

	// Convert to native path separators (handles both / and \ correctly)
	filename = filepath.FromSlash(filename)
	fullPath := filepath.Join(e.path, filename)

	// Ensure the directory exists if pattern includes subdirectories
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		e.logger.Errorf("Failed to create directory %q: %v", dir, err)
	}

	return fullPath
}

// sanitizeFilename removes or replaces characters that are invalid in filenames
func sanitizeFilename(name string) string {
	// Replace characters that are problematic in filenames
	replacer := strings.NewReplacer(
		":", "-",
		"/", "-",
		"\\", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
	)
	return replacer.Replace(name)
}

func (e *exporter) convertData(rows []row, builder *array.RecordBuilder, tags, fields []string) arrow.Record {
	for _, r := range rows {
		builder.Field(0).(*array.TimestampBuilder).Append(arrow.Timestamp(r.timestamp))
		base := 1
		for i, k := range tags {
			if v, found := r.tags[k]; found {
				builder.Field(base + i).(*array.StringBuilder).Append(v)
			} else {
				builder.Field(base + i).AppendNull()
			}
		}
		base = len(tags) + 1
		for i, k := range fields {
			v, found := r.fields[k]
			if !found {
				builder.Field(base + i).AppendNull()
				continue
			}
			switch b := builder.Field(base + i).(type) {
			case *array.Float64Builder:
				b.Append(v.(float64))
			case *array.Int64Builder:
				b.Append(v.(int64))
			case *array.Uint64Builder:
				b.Append(v.(uint64))
			case *array.StringBuilder:
				b.Append(v.(string))
			case *array.BooleanBuilder:
				b.Append(v.(bool))
			}
		}
	}

	return builder.NewRecord()
}
