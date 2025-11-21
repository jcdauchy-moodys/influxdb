# Export to Parquet

The `export-parquet` command exports InfluxDB data to Apache Parquet format files.

## Usage

```bash
influx_inspect export-parquet [flags]
```

## Compression Algorithms

Parquet supports multiple compression algorithms. Choose based on your priorities:

| Algorithm | Speed | Compression Ratio | Use Case |
|-----------|-------|-------------------|----------|
| **uncompressed** | ⚡⚡⚡ Fastest export | ❌ No compression | Testing, already compressed storage |
| **snappy** (default) | ⚡⚡ Fast | ✓ Good (2-4x) | **Best balance** for most cases |
| **lz4** | ⚡⚡ Fast | ✓ Good (2-4x) | Similar to Snappy, slightly faster |
| **gzip** | ⚡ Slow | ✓✓ Better (3-6x) | Slower export, better compression |
| **zstd** | ⚡ Slow | ✓✓✓ Best (4-8x) | **Best compression**, good speed |
| **brotli** | 🐌 Very slow | ✓✓✓ Best (4-8x) | Maximum compression, slowest |
| **lzo** | ⚡⚡ Fast | ✓ Moderate (2-3x) | Legacy, use LZ4 instead |

**Recommendation:**
- **Default (Snappy)**: Good for most users - fast export, decent compression
- **Fast export**: Use `--compression lz4` or `--compression uncompressed`
- **Best compression**: Use `--compression zstd` or `--compression brotli`

Example:
```bash
# Fast export with ZSTD compression
influx_inspect export-parquet \
  --database mydb \
  --compression zstd \
  --out ./parquet-export
```

## Performance Note: Using Metadata

For **significantly faster exports** when filtering by time range, provide the `--metadir` flag:

- **With metadata:** Only processes shards that overlap with your time range (smart filtering)
- **Without metadata:** Must scan all shards in the database/retention policy (brute force)

**Example speedup:** If you have 100 shards but only 5 overlap with your time range, using `--metadir` makes the export ~20x faster!

## Flags

### Required Flags

- `--database` (string): Database to export (required)

### Optional Flags

- `--config` (string): Config file of the InfluxDB v1 instance
- `--datadir` (string): Data storage path (default: `$HOME/.influxdb/data`)
- `--waldir` (string): WAL storage path (default: `$HOME/.influxdb/wal`)
- `--metadir` (string): Metadata storage path (default: `$HOME/.influxdb/meta` - optional, enables faster shard filtering by time range)
- `--retention` (string): Retention policy in the database to export (default: default RP of the DB)
- `--measurements` (string): Comma-separated list of measurements to export (default: `*` - all measurements)
- `--start` (string): Start time to export in RFC3339 format (e.g., `2024-01-01T00:00:00Z`)
- `--end` (string): End time to export in RFC3339 format (e.g., `2024-12-31T23:59:59Z`)
- `--resolve-types` (string): Comma-separated list of field type resolutions in the form `<measurement>.<field>=<type>`
- `--resolve-names` (string): Comma-separated list of field renamings in the form `<measurement>.<field>=<new name>`
- `--out` / `--output` (string): Output directory for exported parquet files (default: current working directory)
- `--filename-pattern` (string): Filename pattern for exported files (default: `{db}#{measurement}#{rp}#{start}#{end}.parquet`)
- `--compression` (string): Parquet compression algorithm (default: `snappy`)
  - Options: `uncompressed`, `snappy`, `gzip`, `lzo`, `brotli`, `lz4`, `zstd`
- `--dry-run` (bool): Print plan and exit without exporting (default: false)

## Type Resolution

When a field has conflicting types across different series (e.g., sometimes integer, sometimes float), you must resolve the type conflict using `--resolve-types`:

```bash
influx_inspect export-parquet \
  --database mydb \
  --resolve-types "cpu.usage=float,memory.free=int"
```

Valid types: `float`, `int`, `uint`, `bool`, `string`

## Name Resolution

### Automatic Resolution (Default)

When a field name conflicts with a tag name, the exporter **automatically** appends `_field` to the field name and logs a warning:

```bash
influx_inspect export-parquet --database mydb

# Output:
# WARNING: Auto-resolved tag/field name conflicts:
#   host -> host_field
#   region -> region_field
```

If `_field` itself conflicts, it appends a number (`_field2`, `_field3`, etc.).

### Manual Override

You can override the automatic resolution using `--resolve-names`:

```bash
influx_inspect export-parquet \
  --database mydb \
  --resolve-names "cpu.host=hostname,cpu.region=region_name"
```

This gives you full control over the renamed field names.

## Filename Pattern

Customize the output filename pattern using `--filename-pattern`. The default pattern is:
```
{db}#{measurement}#{rp}#{start}#{end}.parquet
```

This generates filenames like:
```
mydb#cpu#autogen#2024-10-30T00:00:00Z#2024-10-31T00:00:00Z.parquet
```

### Supported Placeholders

- `{db}` or `{database}`: Database name
- `{measurement}`: Measurement name
- `{rp}` or `{retention}`: Retention policy name
- `{start}`: Start time (filesystem-safe format: `2006-01-02T15-04-05Z`)
- `{end}`: End time (filesystem-safe format: `2006-01-02T15-04-05Z`)
- `{shard}`: Shard ID
- `{seq}`: Sequence number (5-digit with leading zeros)

**Note:** All placeholder values are automatically sanitized for filesystem safety (special characters like `:`, `/`, `*`, etc. are replaced with `-`).

### Examples

Simple pattern:
```bash
--filename-pattern "{measurement}.parquet"
```

With shard ID and sequence:
```bash
--filename-pattern "{db}_{measurement}_shard{shard}_{seq}.parquet"
```

**Directory structures** (automatically created):
```bash
# Organize by database and retention policy
--filename-pattern "{db}/{rp}/{measurement}.parquet"
# Creates: mydb/autogen/cpu.parquet
#          mydb/autogen/memory.parquet

# Organize by measurement
--filename-pattern "{measurement}/{start}.parquet"
# Creates: cpu/2024-11-21T10-00-00Z.parquet
#          memory/2024-11-21T10-00-00Z.parquet

# Complex hierarchy
--filename-pattern "{db}/{rp}/{measurement}/shard-{shard}.parquet"
# Creates: mydb/autogen/cpu/shard-51494.parquet
#          mydb/autogen/cpu/shard-51495.parquet
```

## Examples

### Export entire database

```bash
influx_inspect export-parquet --database mydb --output ./parquet-export
```

### Export specific retention policy

```bash
influx_inspect export-parquet \
  --database mydb \
  --retention autogen \
  --output ./parquet-export
```

### Export specific measurements

```bash
influx_inspect export-parquet \
  --database mydb \
  --measurements "cpu,mem,disk" \
  --output ./parquet-export
```

### Export with time range

```bash
influx_inspect export-parquet \
  --datadir /moodys/influxdb/data \
  --waldir /moodys/influxdb/wal \
  --database spark_db \
  --retention autogen \
  --start "2025-11-10T00:00:00Z" \
  --end "2025-11-12T00:00:00Z" \
  --out ./parquet-export
```

### Export with metadata for faster shard filtering

```bash
influx_inspect export-parquet \
  --datadir /moodys/influxdb/data \
  --waldir /moodys/influxdb/wal \
  --metadir /moodys/influxdb/meta \
  --database spark_db \
  --retention autogen \
  --start "2025-11-10T00:00:00Z" \
  --end "2025-11-12T00:00:00Z" \
  --out ./parquet-export
```

**With `--metadir`:** Only processes shards that overlap with the time range (faster)  
**Without `--metadir`:** Must check all shards in the database/retention policy (slower for time-range queries)

### Export with custom filename pattern

```bash
influx_inspect export-parquet \
  --database mydb \
  --retention autogen \
  --filename-pattern "{db}#{measurement}#{rp}#{start}#{end}.parquet" \
  --out ./parquet-export
```

This creates files like:
```
mydb#cpu#autogen#2024-10-30T15-04-05Z#2024-10-31T15-04-05Z.parquet
mydb#memory#autogen#2024-10-30T15-04-05Z#2024-10-31T15-04-05Z.parquet
```

### Export with directory structure

```bash
influx_inspect export-parquet \
  --database mydb \
  --retention autogen \
  --filename-pattern "{db}/{rp}/{measurement}.parquet" \
  --out ./parquet-export
```

This creates a directory hierarchy:
```
parquet-export/
├── mydb/
│   └── autogen/
│       ├── cpu.parquet
│       ├── memory.parquet
│       └── disk.parquet
```

### Dry run to check schema

```bash
influx_inspect export-parquet \
  --database mydb \
  --dry-run
```

This will print the schema plan without actually exporting data.

### Export with automatic name conflict resolution

```bash
influx_inspect export-parquet \
  --database mydb \
  --out ./parquet-export
```

If there are tag/field name conflicts, they are automatically resolved:
```
Creating the following schemata for 1 measurement(s):
  Measurement "cpu" with 2 tag(s) and  3 field(s):
    Column        Kind       Datatype
    ------        ----       --------
    time          timestamp  timestamp (nanosecond)
    host          tag        string
    region        tag        string
    host -> host_field    field      string
    usage         field      float
    count         field      int
    WARNING: Auto-resolved tag/field name conflicts:
      host -> host_field
```

### Export with type resolution

```bash
influx_inspect export-parquet \
  --database mydb \
  --resolve-types "temperature.value=float,count.value=int" \
  --output ./parquet-export
```

### Export using config file

```bash
influx_inspect export-parquet \
  --config /etc/influxdb/influxdb.conf \
  --database mydb \
  --output ./parquet-export
```

### Export with different compression

```bash
# No compression (fastest export, largest files)
influx_inspect export-parquet \
  --database mydb \
  --compression uncompressed \
  --out ./parquet-export

# ZSTD compression (best compression ratio)
influx_inspect export-parquet \
  --database mydb \
  --compression zstd \
  --out ./parquet-export

# LZ4 compression (fast export, good compression)
influx_inspect export-parquet \
  --database mydb \
  --compression lz4 \
  --out ./parquet-export
```

## Output Format

The command creates Parquet files in the output directory with the following naming scheme:

```
<measurement>-<sequence>.parquet
```

For example:
- `cpu-00000.parquet`
- `cpu-00001.parquet`
- `memory-00000.parquet`

Each Parquet file contains:

1. **Metadata**: Database, retention policy, measurement, shard info, export timestamp
2. **Schema**: 
   - `time` column (timestamp with nanosecond precision)
   - Tag columns (string type, nullable)
   - Field columns (typed according to InfluxDB field types, nullable)

## Schema

The Parquet schema preserves the InfluxDB data model:

- **time**: Timestamp column (nanosecond precision)
- **tags**: String columns (nullable)
- **fields**: Typed columns based on InfluxDB field types:
  - Float → Float64
  - Integer → Int64
  - Unsigned → Uint64
  - Boolean → Boolean
  - String → String

## Troubleshooting

### Type Conflicts

If you encounter an error about unresolved type conflicts:

1. Run with `--dry-run` to see which fields have conflicts
2. Use `--resolve-types` to specify the desired type for each conflicting field

Example error:
```
! Measurement "cpu" with conflict(s) in 0 tag(s), 1 field(s):
  unresolved type conflicts for "usage"
```

Solution:
```bash
influx_inspect export-parquet \
  --database mydb \
  --resolve-types "cpu.usage=float" \
  --output ./parquet-export
```

### Name Conflicts

If a field name matches a tag name, you must rename one of them:

```bash
influx_inspect export-parquet \
  --database mydb \
  --resolve-names "cpu.host=hostname_field" \
  --output ./parquet-export
```

### Missing Meta Store

If running without access to the meta store, the export may fail. Ensure:
- The `--config` flag points to a valid InfluxDB configuration file, OR
- The `--datadir` and `--waldir` flags point to valid data directories

## Comparison with `export` Command

| Feature | `export` | `export-parquet` |
|---------|----------|------------------|
| Output Format | Line Protocol | Apache Parquet |
| Compression | Optional (gzip) | Built-in (columnar) |
| Type Preservation | Text | Strongly Typed |
| Schema Evolution | Manual | Automatic |
| Analytics Tools | Limited | Excellent (Spark, Pandas, etc.) |
| Re-import to InfluxDB | Yes (influx CLI) | No (requires conversion) |

Use `export-parquet` when:
- You need to analyze data with analytical tools (Spark, Pandas, DuckDB, etc.)
- You want efficient columnar storage
- You need strong typing for downstream processing

Use `export` when:
- You plan to re-import data back to InfluxDB
- You need human-readable output
- You want to preserve DDL statements

