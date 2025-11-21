package exportparquet

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/apache/arrow/go/v16/arrow"

	"github.com/influxdata/influxdb/models"
	"github.com/influxdata/influxdb/pkg/errors"
	"github.com/influxdata/influxdb/tsdb"
	"github.com/influxdata/influxql"
)

type seriesEntry struct {
	key    string
	tags   models.Tags
	fields map[string]influxql.DataType
}

type schemaCreator struct {
	measurement string
	shards      []*tsdb.Shard

	series map[uint64][]seriesEntry

	tags      []string
	fields    map[string]influxql.DataType
	fieldKeys []string
	conflicts map[string][]influxql.DataType

	typeResolutions map[string]influxql.DataType
	nameResolutions map[string]string
	autoResolved    []string // Track auto-resolved name conflicts
}

func (s *schemaCreator) extractSchema(ctx context.Context) (err error) {
	// Iterate over the shards and extract all series
	for _, shard := range s.shards {
		// Extract all fields of the measurement
		fields := shard.MeasurementFields([]byte(s.measurement)).FieldSet()
		if len(fields) == 0 {
			continue
		}

		// Collect all available series in the shard and measurement and store
		// them for later use and series accumulation
		seriesCursor, err := shard.CreateSeriesCursor(
			ctx,
			tsdb.SeriesCursorRequest{},
			influxql.MustParseExpr("_name = '"+s.measurement+"'"),
		)
		if err != nil {
			return fmt.Errorf("getting series cursor failed: %w", err)
		}
		defer errors.Capture(&err, seriesCursor.Close)()

		for {
			cur, err := seriesCursor.Next()
			if err != nil {
				return fmt.Errorf("advancing series cursor failed: %w", err)
			}
			if cur == nil {
				break
			}
			mname := string(cur.Name)
			if mname != s.measurement {
				continue
			}

			s.series[shard.ID()] = append(s.series[shard.ID()], seriesEntry{
				key:    s.measurement + "." + string(cur.Tags.HashKey(true)),
				tags:   cur.Tags.Clone(),
				fields: fields,
			})
		}
	}

	// Collect all tags and fields for creating the overall schema
	tags := make(map[string]bool)
	conflicts := make(map[string]map[influxql.DataType]bool)
	s.fields = make(map[string]influxql.DataType)
	for _, series := range s.series {
		for _, serie := range series {
			// Dedup tag keys
			for _, tag := range serie.tags {
				tags[string(tag.Key)] = true
			}
			// Detect field type conflicts and collect the fields
			for name, current := range serie.fields {
				if existing, found := s.fields[name]; found && current != existing {
					if _, exists := conflicts[name]; !exists {
						conflicts[name] = make(map[influxql.DataType]bool)
					}
					conflicts[name][current] = true
					conflicts[name][existing] = true
				}
				s.fields[name] = current
			}
		}
	}

	// Apply the type resolutions
	for name, ftype := range s.typeResolutions {
		if _, found := s.fields[name]; !found {
			continue
		}
		s.fields[name] = ftype
	}

	// Boil down all tags, fields and conflicts for later reuse
	s.tags = make([]string, 0, len(tags))
	for name := range tags {
		s.tags = append(s.tags, name)
	}
	sort.Strings(s.tags)

	s.fieldKeys = make([]string, 0, len(s.fields))
	for name, ftype := range s.fields {
		// Detect unconvertible fields
		switch ftype {
		case influxql.Float, influxql.Integer, influxql.String, influxql.Boolean, influxql.Unsigned:
			// Accepted type
			s.fieldKeys = append(s.fieldKeys, name)
		default:
			// Unconvertible type
			return fmt.Errorf("unconvertible field %q with type %v", name, ftype)
		}
	}
	sort.Strings(s.fieldKeys)

	s.conflicts = make(map[string][]influxql.DataType, len(conflicts))
	for name, conflict := range conflicts {
		s.conflicts[name] = make([]influxql.DataType, 0, len(conflict))
		for ftype := range conflict {
			s.conflicts[name] = append(s.conflicts[name], ftype)
		}
	}

	return nil
}

func (s *schemaCreator) validate() (hasConflicts bool, errs []error) {

	// Check for unresolved conflicting field types
	var typeConflicts []string
	for field := range s.conflicts {
		hasConflicts = true
		if _, resolved := s.typeResolutions[field]; !resolved {
			typeConflicts = append(typeConflicts, field)
		}
	}

	if len(typeConflicts) > 0 {
		errs = append(errs, fmt.Errorf("unresolved type conflicts for %q", strings.Join(typeConflicts, ",")))
	}

	// Initialize nameResolutions map if nil
	if s.nameResolutions == nil {
		s.nameResolutions = make(map[string]string)
	}

	// Automatically resolve name clashes between tags and fields
	s.autoResolved = []string{}
	for _, field := range s.fieldKeys {
		for _, tag := range s.tags {
			if tag == field {
				hasConflicts = true
				// If not manually resolved, auto-resolve by appending "_field"
				if _, resolved := s.nameResolutions[field]; !resolved {
					newName := field + "_field"
					// Ensure the new name doesn't conflict either
					conflictResolved := false
					suffix := 1
					for !conflictResolved {
						conflict := false
						// Check against tags
						for _, t := range s.tags {
							if t == newName {
								conflict = true
								break
							}
						}
						// Check against other fields
						if !conflict {
							for _, f := range s.fieldKeys {
								if f != field && f == newName {
									conflict = true
									break
								}
							}
						}
						if conflict {
							suffix++
							newName = field + "_field" + fmt.Sprintf("%d", suffix)
						} else {
							conflictResolved = true
						}
					}
					s.nameResolutions[field] = newName
					s.autoResolved = append(s.autoResolved, field+" -> "+newName)
				}
			}
		}
	}

	if len(errs) > 0 {
		// we have unresolved named or type conflicts; we cannot continue.
		return hasConflicts, errs
	}

	// Check for name clashes after resolving field names
	resolvedFieldKeys := make([]string, 0, len(s.fieldKeys))
	for _, field := range s.fieldKeys {
		if n, found := s.nameResolutions[field]; found {
			resolvedFieldKeys = append(resolvedFieldKeys, n)
		} else {
			resolvedFieldKeys = append(resolvedFieldKeys, field)
		}
	}
	var resolvedConflicts []string
	for i, field := range resolvedFieldKeys {
		origField := s.fieldKeys[i]
		for _, tag := range s.tags {
			if tag == field {
				hasConflicts = true
				resolvedConflicts = append(resolvedConflicts, "resolved '"+origField+"' with tag '"+tag+"'")
			}
		}
		for j, f := range resolvedFieldKeys {
			if i > j && field == f {
				hasConflicts = true
				resolvedConflicts = append(resolvedConflicts, "resolved '"+origField+"' with field '"+f+"'")
			}
		}
	}
	if len(resolvedConflicts) > 0 {
		return hasConflicts, []error{fmt.Errorf("conflicts after field name resolution for %s", strings.Join(resolvedConflicts, ", "))}
	}

	return hasConflicts, nil
}

func (s *schemaCreator) schema(info map[string]string) (*arrow.Schema, error) {
	columns := make([]arrow.Field, 0, 1+len(s.tags)+len(s.fields))

	// Add the timestamp column first
	columns = append(columns, arrow.Field{
		Name:     "time",
		Type:     &arrow.TimestampType{Unit: arrow.Nanosecond},
		Metadata: arrow.MetadataFrom(map[string]string{"kind": "timestamp"}),
	})

	// Add tags in alphabetical order
	for _, tag := range s.tags {
		columns = append(columns, arrow.Field{
			Name:     tag,
			Type:     &arrow.StringType{},
			Nullable: true,
			Metadata: arrow.MetadataFrom(map[string]string{"kind": "tag"}),
		})
	}

	// Add fields in alphabetical order with the corresponding arrow type
	for _, name := range s.fieldKeys {
		ftype := s.fields[name]
		if t, found := s.typeResolutions[name]; found {
			ftype = t
		}
		fname := name
		if n, found := s.nameResolutions[name]; found {
			fname = n
		}

		var dtype arrow.DataType
		switch ftype {
		case influxql.Float:
			dtype = &arrow.Float64Type{}
		case influxql.Integer:
			dtype = &arrow.Int64Type{}
		case influxql.String:
			dtype = &arrow.StringType{}
		case influxql.Boolean:
			dtype = &arrow.BooleanType{}
		case influxql.Unsigned:
			dtype = &arrow.Uint64Type{}
		default:
			return nil, fmt.Errorf("unconvertible field %q", name)
		}
		columns = append(columns, arrow.Field{
			Name:     fname,
			Type:     dtype,
			Nullable: true,
			Metadata: arrow.MetadataFrom(map[string]string{"kind": "field"}),
		})
	}

	// Add the metadata given as argument and add info about column kinds
	if info == nil {
		info = make(map[string]string)
	}
	info["tags"] = strings.Join(s.tags, ",")
	info["fields"] = strings.Join(s.fieldKeys, ",")
	metadata := arrow.MetadataFrom(info)

	return arrow.NewSchema(columns, &metadata), nil
}

