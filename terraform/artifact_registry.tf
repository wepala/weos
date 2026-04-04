resource "google_artifact_registry_repository" "app" {
  location      = var.region
  repository_id = var.service_name
  format        = "DOCKER"
  project       = var.project_id

  depends_on = [google_project_service.apis]
}
