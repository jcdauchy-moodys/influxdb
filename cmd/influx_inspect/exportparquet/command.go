package exportparquet

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/influxdata/influxdb/cmd/influxd/run"
	internal_errors "github.com/influxdata/influxdb/pkg/errors"
	"github.com/influxdata/influxdb/services/meta"
	"github.com/influxdata/influxdb/tsdb"
)

// Command represents the program execution for "influx_inspect export-parquet".
type Command struct {
	// Standard input/output, overridden for testing.
	Stderr io.Writer
	Logger *zap.Logger
}

// NewCommand returns a new instance of the export Command.
func NewCommand() *Command {
	return &Command{
		Stderr: os.Stderr,
	}
}

// Run executes the export command using the specified args.
func (cmd *Command) Run(args ...string) (err error) {
	var (
		configPath      string
		dataDir         string
		walDir          string
		metaDir         string
		database        string
		rp              string
		measurements    string
		typeResolutions string
		nameResolutions string
		output          string
		filenamePattern string
		compression     string
		start           string
		end             string
		dryRun          bool
	)

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current working directory failed: %w", err)
	}

	flags := flag.NewFlagSet("export-parquet", flag.ContinueOnError)
	flags.StringVar(&configPath, "config", "", "Config file of the InfluxDB v1 instance")
	flags.StringVar(&dataDir, "datadir", os.Getenv("HOME")+"/.influxdb/data", "Data storage path")
	flags.StringVar(&walDir, "waldir", os.Getenv("HOME")+"/.influxdb/wal", "WAL storage path")
	flags.StringVar(&metaDir, "metadir", "", "Metadata storage path (optional, enables faster shard filtering by time range)")
	flags.StringVar(&database, "database", "", "Database to export")
	flags.StringVar(&rp, "retention", "", "Retention policy in the database to export (default: default RP of the DB)")
	flags.StringVar(&measurements, "measurements", "*", "Comma-separated list of measurements to export")
	flags.StringVar(&start, "start", "", "Optional: the start time to export (RFC3339 format)")
	flags.StringVar(&end, "end", "", "Optional: the end time to export (RFC3339 format)")
	flags.StringVar(&typeResolutions, "resolve-types", "", "Comma-separated list of field type resolutions in the form <measurements>.<field>=<type>")
	flags.StringVar(&nameResolutions, "resolve-names", "", "Comma-separated list of field renamings in the form <measurements>.<field>=<new name> (default: auto-appends '_field' to conflicting fields)")
	flags.StringVar(&output, "out", cwd, "Output directory for exported parquet files")
	flags.StringVar(&output, "output", cwd, "Output directory for exported parquet files (alias for -out)")
	flags.StringVar(&filenamePattern, "filename-pattern", "", "Filename pattern (default: {db}#{measurement}#{rp}#{start}#{end}.parquet). Supports: {db}, {measurement}, {rp}, {start}, {end}, {shard}, {seq}. Can include directories: {db}/{rp}/{measurement}.parquet")
	flags.StringVar(&compression, "compression", "snappy", "Parquet compression algorithm: uncompressed, snappy, gzip, lzo, brotli, lz4, zstd (default: snappy)")
	flags.BoolVar(&dryRun, "dry-run", false, "Print plan and exit")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing flags failed: %w", err)
	}

	if database == "" {
		return errors.New("database is required")
	}

	// Parse time range
	var startTime, endTime time.Time
	if start != "" {
		startTime, err = time.Parse(time.RFC3339, start)
		if err != nil {
			return fmt.Errorf("parsing start time failed: %w", err)
		}
	}
	if end != "" {
		endTime, err = time.Parse(time.RFC3339, end)
		if err != nil {
			return fmt.Errorf("parsing end time failed: %w", err)
		}
	}
	if !startTime.IsZero() && !endTime.IsZero() && endTime.Before(startTime) {
		return errors.New("end time must be after start time")
	}

	loggerCfg := zap.NewDevelopmentConfig()
	loggerCfg.DisableStacktrace = true
	loggerCfg.DisableCaller = true
	cmd.Logger, err = loggerCfg.Build()
	if err != nil {
		return fmt.Errorf("creating logger failed: %w", err)
	}

	// Create a simple server adapter for influx_inspect
	server := &inspectServer{
		dataDir: dataDir,
		walDir:  walDir,
		metaDir: metaDir,
		logger:  cmd.Logger,
	}

	if configPath != "" {
		if err := server.loadConfig(configPath); err != nil {
			return fmt.Errorf("loading config failed: %w", err)
		}
	} else {
		// Use direct paths
		server.config = run.NewConfig()
		server.config.Data.Dir = dataDir
		server.config.Data.WALDir = walDir
		// Set metadir if provided
		if metaDir != "" {
			server.config.Meta.Dir = metaDir
		}
	}

	if err := server.Open(); err != nil {
		return fmt.Errorf("opening server failed: %w", err)
	}
	defer server.Close()

	cfg := &config{
		Database:        database,
		RP:              rp,
		Measurements:    measurements,
		StartTime:       startTime,
		EndTime:         endTime,
		TypeResolutions: typeResolutions,
		NameResolutions: nameResolutions,
		Output:          output,
		FilenamePattern: filenamePattern,
		Compression:     compression,
	}
	exp, err := newExporter(server, cfg, cmd.Logger)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if err := exp.open(ctx); err != nil {
		return fmt.Errorf("opening exporter failed: %w", err)
	}
	defer internal_errors.Capture(&err, exp.close)()

	exp.printPlan(cmd.Stderr)

	if dryRun {
		return nil
	}

	return exp.export(ctx)
}

// inspectServer implements a minimal server interface for influx_inspect
type inspectServer struct {
	dataDir string
	walDir  string
	metaDir string
	logger  *zap.Logger
	config  *run.Config
	client  *meta.Client
}

func (s *inspectServer) loadConfig(path string) error {
	config := run.NewConfig()
	if err := config.FromTomlFile(path); err != nil {
		return err
	}
	s.config = config
	return nil
}

func (s *inspectServer) Open() error {
	if s.config == nil {
		return errors.New("config not loaded")
	}

	// Only validate and open meta client if Meta.Dir is specified
	if s.config.Meta.Dir != "" {
		// Check if meta directory exists
		if _, err := os.Stat(s.config.Meta.Dir); err != nil {
			if os.IsNotExist(err) {
				s.logger.Warn("Metadata directory does not exist, will use filesystem discovery")
				return nil
			}
			return fmt.Errorf("checking meta directory: %w", err)
		}

		// Validate the configuration.
		if err := s.config.Validate(); err != nil {
			return fmt.Errorf("validate config: %w", err)
		}

		s.client = meta.NewClient(s.config.Meta)
		if err := s.client.Open(); err != nil {
			s.logger.Sugar().Warnf("Could not open meta client: %v, will use filesystem discovery", err)
			// Don't fail completely - we can work without meta
			s.client = nil
		} else {
			s.logger.Info("Successfully opened metadata client for optimized shard filtering")
		}
	}

	return nil
}

func (s *inspectServer) Close() {
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
}

func (s *inspectServer) MetaClient() MetaClient {
	if s.client == nil {
		return nil
	}
	return &metaClientAdapter{s.client}
}

func (s *inspectServer) TSDBConfig() tsdb.Config {
	return s.config.Data
}

func (s *inspectServer) Logger() *zap.Logger {
	return s.logger
}

// MetaClient is a minimal interface for metadata operations
type MetaClient interface {
	Database(name string) *meta.DatabaseInfo
	RetentionPolicy(database, name string) (*meta.RetentionPolicyInfo, error)
	NodeShardGroupsByTimeRange(database, policy string, min, max int64) ([]meta.ShardGroupInfo, error)
}

type metaClientAdapter struct {
	*meta.Client
}

func (m *metaClientAdapter) NodeShardGroupsByTimeRange(database, policy string, min, max int64) ([]meta.ShardGroupInfo, error) {
	if m.Client == nil {
		return nil, errors.New("meta client not available")
	}
	return m.Client.ShardGroupsByTimeRange(database, policy, time.Unix(0, min), time.Unix(0, max))
}
