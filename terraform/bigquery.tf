resource "google_bigquery_dataset" "events" {
  dataset_id    = "${replace(var.service_name, "-", "_")}_events"
  friendly_name = "${var.service_name} Event Store"
  description   = "Event sourcing event store"
  location      = var.bq_location
  project       = var.project_id

  delete_contents_on_destroy = var.bq_delete_contents_on_destroy

  labels = {
    application = replace(var.service_name, "-", "_")
    managed_by  = "terraform"
  }

  depends_on = [google_project_service.apis]
}

resource "google_bigquery_table" "events" {
  dataset_id          = google_bigquery_dataset.events.dataset_id
  table_id            = "events"
  project             = var.project_id
  deletion_protection = true

  time_partitioning {
    type  = "DAY"
    field = "created_at"
  }

  clustering = ["aggregate_id", "event_type"]

  schema = jsonencode([
    {
      name        = "id"
      type        = "STRING"
      mode        = "REQUIRED"
      description = "Unique event ID (KSUID)"
    },
    {
      name        = "aggregate_id"
      type        = "STRING"
      mode        = "REQUIRED"
      description = "Aggregate entity ID (URN format)"
    },
    {
      name        = "event_type"
      type        = "STRING"
      mode        = "REQUIRED"
      description = "Event type identifier (e.g. Person.Created)"
    },
    {
      name        = "sequence_no"
      type        = "INTEGER"
      mode        = "REQUIRED"
      description = "Sequence number within the aggregate event stream"
    },
    {
      name        = "transaction_id"
      type        = "STRING"
      mode        = "NULLABLE"
      description = "Correlates events committed in the same UnitOfWork"
    },
    {
      name        = "payload"
      type        = "STRING"
      mode        = "NULLABLE"
      description = "Event payload (JSON-encoded string; use PARSE_JSON(payload) or JSON_VALUE for analytics)"
    },
    {
      name        = "metadata"
      type        = "STRING"
      mode        = "NULLABLE"
      description = "Event metadata (JSON-encoded string; use PARSE_JSON(metadata) or JSON_VALUE for analytics)"
    },
    {
      name        = "created_at"
      type        = "TIMESTAMP"
      mode        = "REQUIRED"
      description = "Event creation timestamp"
    },
  ])
}
