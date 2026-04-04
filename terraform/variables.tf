# -----------------------------------------------------------------------------
# Project
# -----------------------------------------------------------------------------

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for all resources"
  type        = string
  default     = "us-central1"
}

variable "service_name" {
  description = "Name used for Cloud Run service, database, and related resources"
  type        = string
  default     = "weos"
}

# -----------------------------------------------------------------------------
# Container Image
# -----------------------------------------------------------------------------

variable "image_tag" {
  description = "Docker image tag to deploy"
  type        = string
  default     = "latest"
}

# -----------------------------------------------------------------------------
# Cloud SQL
# -----------------------------------------------------------------------------

variable "cloud_sql_tier" {
  description = "Cloud SQL machine type"
  type        = string
  default     = "db-f1-micro"
}

variable "cloud_sql_disk_size" {
  description = "Cloud SQL disk size in GB"
  type        = number
  default     = 10
}

# -----------------------------------------------------------------------------
# Cloud Run
# -----------------------------------------------------------------------------

variable "cloud_run_cpu" {
  description = "CPU allocation for Cloud Run containers"
  type        = string
  default     = "1"
}

variable "cloud_run_memory" {
  description = "Memory allocation for Cloud Run containers"
  type        = string
  default     = "512Mi"
}

variable "cloud_run_min_instances" {
  description = "Minimum number of Cloud Run instances"
  type        = number
  default     = 0
}

variable "cloud_run_max_instances" {
  description = "Maximum number of Cloud Run instances"
  type        = number
  default     = 3
}

# -----------------------------------------------------------------------------
# Application Secrets
# -----------------------------------------------------------------------------

variable "session_secret" {
  description = "Secret key for session cookies"
  type        = string
  sensitive   = true
}

variable "gemini_api_key" {
  description = "Google Gemini API key"
  type        = string
  sensitive   = true
  default     = ""
}

variable "google_oauth_client_id" {
  description = "Google OAuth client ID"
  type        = string
  default     = ""
}

variable "google_oauth_client_secret" {
  description = "Google OAuth client secret"
  type        = string
  sensitive   = true
  default     = ""
}

# -----------------------------------------------------------------------------
# Application Config
# -----------------------------------------------------------------------------

variable "frontend_url" {
  description = "Frontend URL for OAuth redirects (e.g. https://weos-xxxxx.run.app)"
  type        = string
  default     = ""
}

variable "log_level" {
  description = "Application log level (debug, info, warn, error)"
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.log_level)
    error_message = "log_level must be one of: debug, info, warn, error"
  }
}

# -----------------------------------------------------------------------------
# BigQuery
# -----------------------------------------------------------------------------

variable "bq_location" {
  description = "BigQuery dataset location"
  type        = string
  default     = "US"
}

variable "bq_delete_contents_on_destroy" {
  description = "Allow BigQuery dataset contents to be deleted on terraform destroy"
  type        = bool
  default     = false
}
