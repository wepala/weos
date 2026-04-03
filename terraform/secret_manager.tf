# -----------------------------------------------------------------------------
# Database DSN (assembled from Cloud SQL outputs)
# -----------------------------------------------------------------------------

resource "google_secret_manager_secret" "database_dsn" {
  secret_id = "${var.service_name}-database-dsn"
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "database_dsn" {
  secret      = google_secret_manager_secret.database_dsn.id
  secret_data = "host=/cloudsql/${google_sql_database_instance.main.connection_name} user=${google_sql_user.app.name} password=${random_password.db_password.result} dbname=${google_sql_database.app.name} sslmode=disable"
}

resource "google_secret_manager_secret_iam_member" "database_dsn" {
  secret_id = google_secret_manager_secret.database_dsn.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}

# -----------------------------------------------------------------------------
# Session Secret
# -----------------------------------------------------------------------------

resource "google_secret_manager_secret" "session_secret" {
  secret_id = "${var.service_name}-session-secret"
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "session_secret" {
  secret      = google_secret_manager_secret.session_secret.id
  secret_data = var.session_secret
}

resource "google_secret_manager_secret_iam_member" "session_secret" {
  secret_id = google_secret_manager_secret.session_secret.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}

# -----------------------------------------------------------------------------
# Gemini API Key (conditional)
# -----------------------------------------------------------------------------

resource "google_secret_manager_secret" "gemini_api_key" {
  count     = var.gemini_api_key != "" ? 1 : 0
  secret_id = "${var.service_name}-gemini-api-key"
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "gemini_api_key" {
  count       = var.gemini_api_key != "" ? 1 : 0
  secret      = google_secret_manager_secret.gemini_api_key[0].id
  secret_data = var.gemini_api_key
}

resource "google_secret_manager_secret_iam_member" "gemini_api_key" {
  count     = var.gemini_api_key != "" ? 1 : 0
  secret_id = google_secret_manager_secret.gemini_api_key[0].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}

# -----------------------------------------------------------------------------
# Google OAuth Client ID (conditional)
# -----------------------------------------------------------------------------

resource "google_secret_manager_secret" "oauth_client_id" {
  count     = var.google_oauth_client_id != "" ? 1 : 0
  secret_id = "${var.service_name}-oauth-client-id"
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "oauth_client_id" {
  count       = var.google_oauth_client_id != "" ? 1 : 0
  secret      = google_secret_manager_secret.oauth_client_id[0].id
  secret_data = var.google_oauth_client_id
}

resource "google_secret_manager_secret_iam_member" "oauth_client_id" {
  count     = var.google_oauth_client_id != "" ? 1 : 0
  secret_id = google_secret_manager_secret.oauth_client_id[0].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}

# -----------------------------------------------------------------------------
# Google OAuth Client Secret (conditional)
# -----------------------------------------------------------------------------

resource "google_secret_manager_secret" "oauth_client_secret" {
  count     = var.google_oauth_client_secret != "" ? 1 : 0
  secret_id = "${var.service_name}-oauth-client-secret"
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "oauth_client_secret" {
  count       = var.google_oauth_client_secret != "" ? 1 : 0
  secret      = google_secret_manager_secret.oauth_client_secret[0].id
  secret_data = var.google_oauth_client_secret
}

resource "google_secret_manager_secret_iam_member" "oauth_client_secret" {
  count     = var.google_oauth_client_secret != "" ? 1 : 0
  secret_id = google_secret_manager_secret.oauth_client_secret[0].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}
