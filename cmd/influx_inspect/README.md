# `influx_inspect`

## Ways to run

### `influx_inspect`
Will print usage for the tool.

### `influx_inspect report`
Displays series meta-data for all shards.  Default location [$HOME/.influxdb]

### `influx_inspect dumptsm`
Dumps low-level details about tsm1 files

#### Flags

##### `-index` bool
Dump raw index data.

`default` = false

#### `-blocks` bool
Dump raw block data.

`default` = false

#### `-all`
Dump all data. Caution: This may print a lot of information.

`default` = false

#### `-filter-key`
Only display index and block data match this key substring.

`default` = ""


### `influx_inspect export`
Exports all tsm files to line protocol.  This output file can be imported via the [influx](https://github.com/influxdata/influxdb/tree/master/importer#running-the-import-command) command.


#### `-datadir` string
Data storage path.

`default` = "$HOME/.influxdb/data"

#### `-waldir` string
WAL storage path.

`default` = "$HOME/.influxdb/wal"

#### `-out` string
Destination file to export to

`default` = "$HOME/.influxdb/export"

#### `-database` string (optional)
Database to export.

`default` = ""

#### `-retention` string (optional)
Retention policy to export.

`default` = ""

#### `-start` string (optional)
Optional. The time range to start at.

#### `-end` string (optional)
Optional. The time range to end at.

#### `-compress` bool (optional)
Compress the output.

`default` = false

#### Sample Commands

Export entire database and compress output:
```
influx_inspect export --compress
```

Export specific retention policy:
```
influx_inspect export --database mydb --retention autogen
```

##### Sample Data
This is a sample of what the output will look like.

```
# DDL
CREATE DATABASE MY_DB_NAME
CREATE RETENTION POLICY autogen ON MY_DB_NAME DURATION inf REPLICATION 1

# DML
# CONTEXT-DATABASE:MY_DB_NAME
# CONTEXT-RETENTION-POLICY:autogen
randset value=97.9296104805 1439856000000000000
randset value=25.3849066842 1439856100000000000
```

### `influx_inspect export-parquet`
Exports TSM files to Apache Parquet format for use with analytical tools (Spark, Pandas, DuckDB, etc.). See [exportparquet/README.md](exportparquet/README.md) for detailed documentation.

#### Quick Start

Export entire database to Parquet format:
```
influx_inspect export-parquet --database mydb --output ./parquet-export
```

Dry run to preview schema:
```
influx_inspect export-parquet --database mydb --dry-run
```

Export with type resolution for conflicting field types:
```
influx_inspect export-parquet --database mydb --resolve-types "cpu.usage=float" --output ./parquet-export
```

#### Key Features

- **Columnar Format**: Efficient storage and query performance
- **Strong Typing**: Preserves InfluxDB field types (float, int, uint, bool, string)
- **Schema Resolution**: Handles type conflicts and name clashes
- **Metadata**: Includes database, retention policy, measurement info in file metadata
- **Analytics Ready**: Direct compatibility with Apache Spark, Pandas, DuckDB, and other tools

#### Use Cases

Use `export-parquet` instead of `export` when:
- Analyzing data with analytical tools (Spark, Pandas, DuckDB, etc.)
- Need efficient columnar storage for large datasets
- Require strong typing for downstream processing
- Want to leverage Parquet's built-in compression

Use `export` (line protocol) when:
- Re-importing data back to InfluxDB
- Need human-readable output
- Want to preserve DDL statements

# Caveats

The system does not have access to the meta store when exporting TSM shards.  As such, it always creates the retention policy with infinite duration and replication factor of 1.
End users may want to change this prior to re-importing if they are importing to a cluster or want a different duration for retention.
