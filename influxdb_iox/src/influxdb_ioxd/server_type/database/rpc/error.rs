use generated_types::google::{
    AlreadyExists, FieldViolation, InternalError, NotFound, PreconditionViolation, QuotaFailure,
    ResourceType,
};
use observability_deps::tracing::error;

/// map common [`server::Error`] errors  to the appropriate tonic Status
pub fn default_server_error_handler(error: server::Error) -> tonic::Status {
    use server::{DatabaseNameFromRulesError, Error};

    match error {
        Error::IdNotSet => PreconditionViolation {
            category: "Writer ID".to_string(),
            subject: "influxdata.com/iox".to_string(),
            description: "Writer ID must be set".to_string(),
        }
        .into(),
        Error::DatabaseNotInitialized { db_name } => {
            tonic::Status::unavailable(format!("Database ({}) is not yet initialized", db_name))
        }
        Error::DatabaseAlreadyExists { db_name } => {
            AlreadyExists::new(ResourceType::Database, db_name).into()
        }
        Error::ServerNotInitialized { server_id } => tonic::Status::unavailable(format!(
            "Server ID is set ({}) but server is not yet initialized (e.g. DBs and remotes \
                     are not loaded). Server is not yet ready to read/write data.",
            server_id
        )),
        Error::DatabaseNotFound { db_name } => {
            NotFound::new(ResourceType::Database, db_name).into()
        }
        Error::DatabaseUuidNotFound { uuid } => {
            NotFound::new(ResourceType::DatabaseUuid, uuid.to_string()).into()
        }
        Error::InvalidDatabaseName { source } => FieldViolation {
            field: "db_name".into(),
            description: source.to_string(),
        }
        .into(),
        Error::WipePreservedCatalog { source } => default_database_error_handler(source),
        Error::DatabaseInit { source } => {
            tonic::Status::invalid_argument(format!("Cannot initialize database: {}", source))
        }
        Error::DatabaseAlreadyOwnedByThisServer { uuid } => {
            AlreadyExists::new(ResourceType::DatabaseUuid, uuid.to_string()).into()
        }
        Error::UuidMismatch { .. } | Error::CannotClaimDatabase { .. } => {
            tonic::Status::invalid_argument(error.to_string())
        }
        Error::CouldNotGetDatabaseNameFromRules {
            source: DatabaseNameFromRulesError::DatabaseRulesNotFound { uuid, .. },
        } => NotFound::new(ResourceType::DatabaseUuid, uuid.to_string()).into(),
        error => {
            error!(?error, "Unexpected error");
            InternalError {}.into()
        }
    }
}

/// map common [`catalog::Error`](server::db::catalog::Error) errors to the appropriate tonic Status
pub fn default_catalog_error_handler(error: server::db::catalog::Error) -> tonic::Status {
    use server::db::catalog::Error;
    match error {
        Error::TableNotFound { table } => NotFound::new(ResourceType::Table, table).into(),
        Error::PartitionNotFound { partition, table } => {
            NotFound::new(ResourceType::Partition, format!("{}:{}", table, partition)).into()
        }
        Error::ChunkNotFound {
            chunk_id,
            partition,
            table,
        } => NotFound::new(
            ResourceType::Chunk,
            format!("{}:{}:{}", table, partition, chunk_id),
        )
        .into(),
    }
}

/// map common [`database::Error`](server::database::Error) errors  to the appropriate tonic Status
pub fn default_database_error_handler(error: server::database::Error) -> tonic::Status {
    use server::database::Error;
    match error {
        Error::InvalidState { .. } => tonic::Status::failed_precondition(error.to_string()),
        Error::WipePreservedCatalog { source, .. } => {
            error!(%source, "Unexpected error while wiping catalog");
            InternalError {}.into()
        }
        Error::RebuildPreservedCatalog { source, .. } => {
            error!(%source, "Unexpected error while rebuilding catalog");
            InternalError {}.into()
        }
        Error::RulesNotUpdateable { .. } => tonic::Status::failed_precondition(error.to_string()),
        Error::CannotPersistUpdatedRules { .. } => {
            tonic::Status::failed_precondition(error.to_string())
        }
        Error::SkipReplay { source, .. } => {
            error!(%source, "Unexpected error skipping replay");
            InternalError {}.into()
        }
        Error::CannotReleaseUnowned { .. } => tonic::Status::failed_precondition(error.to_string()),
        Error::CannotRelease { source, .. } => {
            error!(%source, "Unexpected error releasing database");
            InternalError {}.into()
        }
    }
}

/// map common [`db::Error`](server::db::Error) errors  to the appropriate tonic Status
pub fn default_db_error_handler(error: server::db::Error) -> tonic::Status {
    use server::db::Error;
    match error {
        Error::LifecycleError { source } => PreconditionViolation {
            category: "chunk".to_string(),
            subject: "influxdata.com/iox".to_string(),
            description: format!(
                "Cannot perform operation due to wrong chunk lifecycle: {}",
                source
            ),
        }
        .into(),
        Error::CannotFlushPartition {
            table_name,
            partition_key,
        } => PreconditionViolation {
            category: "database".to_string(),
            subject: "influxdata.com/iox".to_string(),
            description: format!(
                "Cannot persist partition because it cannot be flushed at the moment: {}:{}",
                table_name, partition_key
            ),
        }
        .into(),
        Error::CatalogError { source } => default_catalog_error_handler(source),
        error => {
            error!(?error, "Unexpected error");
            InternalError {}.into()
        }
    }
}

/// map common [`server::db::DmlError`](server::db::DmlError) errors  to the appropriate tonic Status
pub fn default_dml_error_handler(error: server::db::DmlError) -> tonic::Status {
    use server::db::DmlError;

    match error {
        DmlError::HardLimitReached {} => QuotaFailure {
            subject: "influxdata.com/iox/buffer".to_string(),
            description: "hard buffer limit reached".to_string(),
        }
        .into(),
        e => tonic::Status::invalid_argument(e.to_string()),
    }
}
