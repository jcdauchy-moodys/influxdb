package exportparquet

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"

	"go.uber.org/zap"

	"github.com/influxdata/influxdb/models"
	"github.com/influxdata/influxdb/pkg/escape"
	"github.com/influxdata/influxdb/tsdb"
	"github.com/influxdata/influxdb/tsdb/engine/tsm1"
	"github.com/influxdata/influxql"
)

type row struct {
	timestamp int64
	tags      map[string]string
	fields    map[string]interface{}
}

type batcher struct {
	measurement []byte
	shard       *tsdb.Shard
	tsmFiles    []string
	walFiles    []string

	typeResolutions map[string]influxql.DataType
	converter       map[string]func(interface{}) (interface{}, error)
	nameResolutions map[string]string

	series    []seriesEntry
	start     int64
	end       int64
	startTime int64
	endTime   int64

	logger *zap.SugaredLogger
}

func (b *batcher) init() error {
	// Setup the type converters for the conflicting fields
	b.converter = make(map[string]func(interface{}) (interface{}, error), len(b.typeResolutions))
	for field, ftype := range b.typeResolutions {
		switch ftype {
		case influxql.Float:
			b.converter[field] = toFloat
		case influxql.Unsigned:
			b.converter[field] = toUint
		case influxql.Integer:
			b.converter[field] = toInt
		case influxql.Boolean:
			b.converter[field] = toBool
		case influxql.String:
			b.converter[field] = toString
		default:
			return fmt.Errorf("unknown converter %v for field %q", ftype, field)
		}
	}

	b.start = models.MinNanoTime
	if b.end == 0 {
		b.end = models.MaxNanoTime
	}

	// Store original time range for filtering
	b.startTime = b.start
	b.endTime = b.end

	return nil
}

func (b *batcher) next(ctx context.Context) ([]row, error) {
	// Build lookup maps for series and pre-compute hash keys and tag maps
	seriesMap := make(map[string]seriesEntry)
	seriesKeyCache := make(map[string]string)       // TSM series key -> hash key
	tagsCache := make(map[string]map[string]string) // hash key -> tags map

	for _, s := range b.series {
		seriesMap[s.key] = s
		// Pre-build tags map for this series
		tagsMap := make(map[string]string, len(s.tags))
		for _, t := range s.tags {
			tagsMap[string(t.Key)] = string(t.Value)
		}
		tagsCache[s.key] = tagsMap
	}

	if len(seriesMap) == 0 {
		b.logger.Debugf("No series found for measurement %q", string(b.measurement))
		return nil, nil
	}

	b.logger.Debugf("Processing measurement %q with %d series, %d TSM files, %d WAL files",
		string(b.measurement), len(seriesMap), len(b.tsmFiles), len(b.walFiles))

	// Accumulate data from both TSM and WAL files
	data := make(map[string]map[int64]row, len(b.series))

	// Read TSM files
	for _, tsmPath := range b.tsmFiles {
		if err := b.readTSMFile(tsmPath, seriesMap, seriesKeyCache, tagsCache, data); err != nil {
			return nil, fmt.Errorf("reading TSM file %q failed: %w", tsmPath, err)
		}
	}

	// Read WAL files
	for _, walPath := range b.walFiles {
		if err := b.readWALFile(walPath, seriesMap, seriesKeyCache, tagsCache, data); err != nil {
			return nil, fmt.Errorf("reading WAL file %q failed: %w", walPath, err)
		}
	}

	if len(data) == 0 {
		return nil, nil
	}

	// Extract the rows - TSM data is already time-ordered, so we can just append
	var rowCount int
	for _, tmap := range data {
		rowCount += len(tmap)
	}
	rows := make([]row, 0, rowCount)

	// Collect all timestamps first to sort only if needed
	timestamps := make([]int64, 0, rowCount)
	for _, tmap := range data {
		for ts := range tmap {
			timestamps = append(timestamps, ts)
		}
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	// Build rows in sorted timestamp order
	for _, ts := range timestamps {
		for _, tmap := range data {
			if r, exists := tmap[ts]; exists {
				rows = append(rows, r)
				delete(tmap, ts) // Prevent duplicates
				break
			}
		}
	}

	return rows, nil
}

func (b *batcher) readTSMFile(tsmPath string, seriesMap map[string]seriesEntry, seriesKeyCache map[string]string, tagsCache map[string]map[string]string, data map[string]map[int64]row) error {
	f, err := os.Open(tsmPath)
	if err != nil {
		if os.IsNotExist(err) {
			b.logger.Warnf("skipped missing TSM file: %s", tsmPath)
			return nil
		}
		return err
	}
	defer f.Close()

	r, err := tsm1.NewTSMReader(f)
	if err != nil {
		b.logger.Warnf("unable to read %s, skipping: %v", tsmPath, err)
		return nil
	}
	defer r.Close()

	// File-level filtering: skip entire file if outside time range
	if sgStart, sgEnd := r.TimeRange(); sgStart > b.endTime || sgEnd < b.startTime {
		return nil
	}

	for i := 0; i < r.KeyCount(); i++ {
		key, _ := r.KeyAt(i)
		seriesKeyBytes, fieldBytes := tsm1.SeriesAndFieldFromCompositeKey(key)

		// Skip if not our measurement (measurement is part of the series key)
		if !bytes.HasPrefix(seriesKeyBytes, b.measurement) {
			continue
		}

		field := string(escape.Unescape(fieldBytes))
		tsmSeriesKey := string(seriesKeyBytes)

		// Use cached series key lookup
		seriesKey, found := seriesKeyCache[tsmSeriesKey]
		if !found {
			// First time seeing this series key, parse and cache it
			parsedTags := models.ParseTags(seriesKeyBytes)
			seriesKey = string(b.measurement) + "." + string(parsedTags.HashKey(true))
			seriesKeyCache[tsmSeriesKey] = seriesKey
		}

		series, found := seriesMap[seriesKey]
		if !found {
			continue
		}

		// Check if this field is in the series
		if _, fieldExists := series.fields[field]; !fieldExists {
			continue
		}

		values, err := r.ReadAll(key)
		if err != nil {
			b.logger.Warnf("unable to read key %q in %s, skipping: %v", string(key), tsmPath, err)
			continue
		}

		// Prepare mappings
		fname := field
		if n, found := b.nameResolutions[field]; found {
			fname = n
		}
		converter := identity
		if c, found := b.converter[field]; found {
			converter = c
		}

		// Initialize data structures
		if data[seriesKey] == nil {
			data[seriesKey] = make(map[int64]row, tsdb.DefaultMaxPointsPerBlock)
		}

		// Use cached tags map
		tagsMap := tagsCache[seriesKey]

		// Process values
		for _, value := range values {
			ts := value.UnixNano()

			// Value-level filtering
			if ts < b.startTime || ts > b.endTime {
				continue
			}

			// Convert value if needed
			v, err := converter(value.Value())
			if err != nil {
				b.logger.Errorf("converting %v of field %q failed: %v", value.Value(), field, err)
				continue
			}

			// Create row if doesn't exist
			if _, exists := data[seriesKey][ts]; !exists {
				data[seriesKey][ts] = row{
					timestamp: ts,
					tags:      tagsMap,
					fields:    make(map[string]interface{}),
				}
			}

			data[seriesKey][ts].fields[fname] = v
		}
	}

	return nil
}

func (b *batcher) readWALFile(walPath string, seriesMap map[string]seriesEntry, seriesKeyCache map[string]string, tagsCache map[string]map[string]string, data map[string]map[int64]row) error {
	f, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			b.logger.Warnf("skipped missing WAL file: %s", walPath)
			return nil
		}
		return err
	}
	defer f.Close()

	r := tsm1.NewWALSegmentReader(f)
	defer r.Close()

	for r.Next() {
		entry, err := r.Read()
		if err != nil {
			n := r.Count()
			b.logger.Warnf("WAL file %s corrupt at position %d: %v", walPath, n, err)
			break
		}

		switch t := entry.(type) {
		case *tsm1.DeleteWALEntry, *tsm1.DeleteRangeWALEntry:
			// Skip deletes
			continue
		case *tsm1.WriteWALEntry:
			for key, values := range t.Values {
				seriesKeyBytes, fieldBytes := tsm1.SeriesAndFieldFromCompositeKey([]byte(key))

				// Skip if not our measurement (measurement is part of the series key)
				if !bytes.HasPrefix(seriesKeyBytes, b.measurement) {
					continue
				}

				field := string(escape.Unescape(fieldBytes))
				tsmSeriesKey := string(seriesKeyBytes)

				// Use cached series key lookup
				seriesKey, found := seriesKeyCache[tsmSeriesKey]
				if !found {
					// First time seeing this series key, parse and cache it
					parsedTags := models.ParseTags(seriesKeyBytes)
					seriesKey = string(b.measurement) + "." + string(parsedTags.HashKey(true))
					seriesKeyCache[tsmSeriesKey] = seriesKey
				}

				series, found := seriesMap[seriesKey]
				if !found {
					continue
				}

				// Check if this field is in the series
				if _, fieldExists := series.fields[field]; !fieldExists {
					continue
				}

				// Prepare mappings
				fname := field
				if n, found := b.nameResolutions[field]; found {
					fname = n
				}
				converter := identity
				if c, found := b.converter[field]; found {
					converter = c
				}

				// Initialize data structures
				if data[seriesKey] == nil {
					data[seriesKey] = make(map[int64]row, tsdb.DefaultMaxPointsPerBlock)
				}

				// Use cached tags map
				tagsMap := tagsCache[seriesKey]

				// Process values
				for _, value := range values {
					ts := value.UnixNano()

					// Value-level filtering
					if ts < b.startTime || ts > b.endTime {
						continue
					}

					// Convert value if needed
					v, err := converter(value.Value())
					if err != nil {
						b.logger.Errorf("converting %v of field %q failed: %v", value.Value(), field, err)
						continue
					}

					// Create row if doesn't exist (WAL data overwrites TSM data)
					if _, exists := data[seriesKey][ts]; !exists {
						data[seriesKey][ts] = row{
							timestamp: ts,
							tags:      tagsMap,
							fields:    make(map[string]interface{}),
						}
					}

					// WAL data takes precedence over TSM data
					data[seriesKey][ts].fields[fname] = v
				}
			}
		}
	}

	return nil
}
