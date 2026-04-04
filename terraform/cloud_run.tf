resource "google_cloud_run_v2_service" "app" {
  name                = var.service_name
  location            = var.region
  project             = var.project_id
  deletion_protection = false

  template {
    service_account = google_service_account.cloud_run.email

    scaling {
      min_instance_count = var.cloud_run_min_instances
      max_instance_count = var.cloud_run_max_instances
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.main.connection_name]
      }
    }

    containers {
      image = local.image_url

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = var.cloud_run_cpu
          memory = var.cloud_run_memory
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      # Plain environment variables
      env {
        name  = "LOG_LEVEL"
        value = var.log_level
      }

      env {
        name  = "FRONTEND_URL"
        value = var.frontend_url
      }

      # Secret environment variables
      env {
        name = "DATABASE_DSN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.database_dsn.secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "SESSION_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.session_secret.secret_id
            version = "latest"
          }
        }
      }

      dynamic "env" {
        for_each = nonsensitive(var.gemini_api_key != "") ? ["GEMINI_API_KEY"] : []
        content {
          name = "GEMINI_API_KEY"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.gemini_api_key[0].secret_id
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = var.google_oauth_client_id != "" ? ["GOOGLE_CLIENT_ID"] : []
        content {
          name = "GOOGLE_CLIENT_ID"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.oauth_client_id[0].secret_id
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = nonsensitive(var.google_oauth_client_secret != "") ? ["GOOGLE_CLIENT_SECRET"] : []
        content {
          name = "GOOGLE_CLIENT_SECRET"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.oauth_client_secret[0].secret_id
              version = "latest"
            }
          }
        }
      }

      startup_probe {
        http_get {
          path = "/api/health"
          port = 8080
        }
        initial_delay_seconds = 5
        period_seconds        = 5
        failure_threshold     = 10
        timeout_seconds       = 3
      }

      liveness_probe {
        http_get {
          path = "/api/health"
          port = 8080
        }
        period_seconds  = 15
        timeout_seconds = 3
      }
    }
  }

  depends_on = [
    google_project_service.apis,
    google_secret_manager_secret_version.database_dsn,
    google_secret_manager_secret_version.session_secret,
  ]
}

# Allow unauthenticated access (app handles its own auth)
resource "google_cloud_run_v2_service_iam_member" "public" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.app.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
