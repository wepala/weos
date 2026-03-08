// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/pkg/identity"

	"go.uber.org/fx"
)

type ThemeFile struct {
	Path string
	Size int64
}

type UploadThemeResult struct {
	Theme         *entities.Theme
	Templates     []*entities.Template
	Files         []ThemeFile
	TemplateFiles map[string]string // template ID → file path from manifest
}

type ThemeService interface {
	Create(ctx context.Context, cmd CreateThemeCommand) (*entities.Theme, error)
	GetByID(ctx context.Context, id string) (*entities.Theme, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Theme], error)
	Update(ctx context.Context, cmd UpdateThemeCommand) (*entities.Theme, error)
	Delete(ctx context.Context, cmd DeleteThemeCommand) error
	Upload(ctx context.Context, cmd UploadThemeCommand) (*UploadThemeResult, error)
}

type themeService struct {
	repo         repositories.ThemeRepository
	templateRepo repositories.TemplateRepository
	logger       entities.Logger
}

func ProvideThemeService(params struct {
	fx.In
	Repo         repositories.ThemeRepository
	TemplateRepo repositories.TemplateRepository
	Logger       entities.Logger
}) ThemeService {
	return &themeService{
		repo:         params.Repo,
		templateRepo: params.TemplateRepo,
		logger:       params.Logger,
	}
}

func (s *themeService) Create(
	ctx context.Context, cmd CreateThemeCommand,
) (*entities.Theme, error) {
	entity, err := new(entities.Theme).With(cmd.Name, cmd.Slug)
	if err != nil {
		return nil, fmt.Errorf("failed to create theme: %w", err)
	}
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "theme created", "id", entity.GetID())
	return entity, nil
}

func (s *themeService) GetByID(
	ctx context.Context, id string,
) (*entities.Theme, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *themeService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Theme], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *themeService) Update(
	ctx context.Context, cmd UpdateThemeCommand,
) (*entities.Theme, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Restore(
		entity.GetID(), cmd.Name, cmd.Slug, cmd.Description,
		cmd.Version, cmd.ThumbnailURL, cmd.Status, entity.CreatedAt(), entity.GetSequenceNo(),
	); err != nil {
		return nil, fmt.Errorf("failed to update theme: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "theme updated", "id", entity.GetID())
	return entity, nil
}

func (s *themeService) Delete(
	ctx context.Context, cmd DeleteThemeCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "theme deleted", "id", cmd.ID)
	return nil
}

func (s *themeService) Upload(
	ctx context.Context, cmd UploadThemeCommand,
) (*UploadThemeResult, error) {
	zipData, err := io.ReadAll(cmd.ZipReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read zip data: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	if len(reader.File) == 0 {
		return nil, fmt.Errorf("zip file is empty")
	}

	manifest, err := resolveManifest(reader, cmd)
	if err != nil {
		return nil, err
	}

	if len(manifest.Templates) == 0 {
		manifest.Templates = []entities.TemplateManifest{
			{Name: "Homepage", Slug: "index", File: findIndexHTML(reader)},
		}
	}

	theme, err := new(entities.Theme).With(manifest.Name, manifest.Slug)
	if err != nil {
		return nil, fmt.Errorf("invalid theme manifest: %w", err)
	}

	if err := s.repo.Save(ctx, theme); err != nil {
		return nil, fmt.Errorf("failed to save theme: %w", err)
	}
	s.logger.Info(ctx, "theme created from upload",
		"id", theme.GetID(), "source", manifest.Source)

	storagePath := cmd.StoragePath
	if storagePath == "" {
		storagePath = "themes"
	}
	themeDir := filepath.Join(storagePath, manifest.Slug)
	if err := extractZip(reader, themeDir); err != nil {
		return nil, fmt.Errorf("failed to extract theme files: %w", err)
	}

	templates := make([]*entities.Template, 0, len(manifest.Templates))
	templateFiles := make(map[string]string, len(manifest.Templates))
	for _, tmplManifest := range manifest.Templates {
		themeSlug := identity.ExtractThemeSlug(theme.GetID())
		tmpl, err := new(entities.Template).With(
			tmplManifest.Name, tmplManifest.Slug, themeSlug)
		if err != nil {
			s.logger.Info(ctx, "skipping invalid template",
				"slug", tmplManifest.Slug, "error", err.Error())
			continue
		}
		if err := s.templateRepo.Save(ctx, tmpl, theme.GetID()); err != nil {
			return nil, fmt.Errorf("failed to save template %q: %w",
				tmplManifest.Slug, err)
		}
		s.logger.Info(ctx, "template created from upload",
			"id", tmpl.GetID(), "themeID", theme.GetID())
		templates = append(templates, tmpl)
		templateFiles[tmpl.GetID()] = tmplManifest.File
	}

	var files []ThemeFile
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		files = append(files, ThemeFile{
			Path: filepath.ToSlash(f.Name),
			Size: int64(f.UncompressedSize64),
		})
	}

	return &UploadThemeResult{
		Theme:         theme,
		Templates:     templates,
		Files:         files,
		TemplateFiles: templateFiles,
	}, nil
}

const maxReadmeBytes = 8 * 1024

var (
	copyrightRe     = regexp.MustCompile(`(?i)copyright\s+(?:©|\(c\))?\s*\d{4}(?:[–-]\d{4})?\s+(.+)`)
	versionSuffixRe = regexp.MustCompile(`[-_](?:v?\d+(?:\.\d+)*|master|main|develop|dev|latest|stable)$`)
	skipDirRe       = regexp.MustCompile(`(?i)^(?:\.|node_modules|vendor|bower_components)`)
)

// resolveManifest applies the metadata resolution cascade:
// 1. theme.json (if present and valid)
// 2. User-provided Name override
// 3. README heading + description
// 4. Zip root directory name
// 5. Zip filename
func resolveManifest(
	reader *zip.Reader, cmd UploadThemeCommand,
) (*entities.ThemeManifest, error) {
	// Priority 1: theme.json
	manifest, err := readThemeJSON(reader)
	if err != nil {
		return nil, err
	}
	if manifest != nil {
		manifest.Source = "theme.json"
		// Auto-discover templates if theme.json defines none
		if len(manifest.Templates) == 0 {
			manifest.Templates = discoverTemplates(reader)
		}
		return manifest, nil
	}

	// No theme.json — build manifest from other sources
	manifest = &entities.ThemeManifest{}

	readmeName, readmeDesc := readReadme(reader)
	licenseAuthor := readLicenseAuthor(reader)

	// Priority 2: User-provided name override
	if cmd.Name != "" {
		manifest.Name = cmd.Name
		manifest.Source = "user-provided"
	}

	// Priority 3: README heading
	if manifest.Name == "" && readmeName != "" {
		manifest.Name = readmeName
		manifest.Source = "readme"
	}

	// Priority 4: Root directory name
	if manifest.Name == "" {
		if rootDir := detectRootDirectory(reader); rootDir != "" {
			manifest.Name = cleanDirectoryName(rootDir)
			manifest.Source = "root-directory"
		}
	}

	// Priority 5: Zip filename
	if manifest.Name == "" && cmd.FileName != "" {
		name := strings.TrimSuffix(cmd.FileName, filepath.Ext(cmd.FileName))
		manifest.Name = cleanDirectoryName(name)
		manifest.Source = "filename"
	}

	if manifest.Name == "" {
		return nil, fmt.Errorf(
			"could not determine theme name: provide a name, " +
				"add a theme.json, or include a README with a heading")
	}

	manifest.Slug = identity.Slugify(manifest.Name)
	manifest.Description = readmeDesc
	manifest.Author = licenseAuthor
	manifest.Templates = discoverTemplates(reader)

	return manifest, nil
}

// readThemeJSON looks for theme.json at root or one level deep.
// Returns nil, nil if not found (not an error).
// Returns error only if the file exists but is malformed.
func readThemeJSON(reader *zip.Reader) (*entities.ThemeManifest, error) {
	for _, f := range reader.File {
		name := filepath.ToSlash(f.Name)
		depth := strings.Count(strings.TrimSuffix(name, "/"), "/")
		if filepath.Base(name) == "theme.json" && depth <= 1 {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open theme.json: %w", err)
			}
			defer rc.Close()

			var manifest entities.ThemeManifest
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				return nil, fmt.Errorf("failed to parse theme.json: %w", err)
			}
			if manifest.Name == "" {
				return nil, fmt.Errorf("theme.json must include a name")
			}
			if manifest.Slug == "" {
				manifest.Slug = identity.Slugify(manifest.Name)
			}
			return &manifest, nil
		}
	}
	return nil, nil
}

// readReadme scans for README.md or README, returning the first heading and description.
func readReadme(reader *zip.Reader) (name, description string) {
	var best *zip.File
	for _, f := range reader.File {
		base := strings.ToLower(filepath.Base(f.Name))
		if base == "readme.md" {
			best = f
			break
		}
		if base == "readme" && best == nil {
			best = f
		}
	}
	if best == nil {
		return "", ""
	}

	rc, err := best.Open()
	if err != nil {
		return "", ""
	}
	defer rc.Close()

	limited := io.LimitReader(rc, maxReadmeBytes)
	scanner := bufio.NewScanner(limited)

	var descLines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if name == "" && strings.HasPrefix(line, "# ") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			continue
		}
		if name != "" && line != "" && !strings.HasPrefix(line, "#") {
			descLines = append(descLines, line)
			if len(descLines) >= 3 {
				break
			}
		}
	}
	return name, strings.Join(descLines, " ")
}

// readLicenseAuthor extracts author from a Copyright line in LICENSE/LICENSE.md.
func readLicenseAuthor(reader *zip.Reader) string {
	for _, f := range reader.File {
		base := strings.ToLower(filepath.Base(f.Name))
		if base == "license" || base == "license.md" || base == "licence" || base == "licence.md" {
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			defer rc.Close()

			limited := io.LimitReader(rc, maxReadmeBytes)
			scanner := bufio.NewScanner(limited)
			for scanner.Scan() {
				if m := copyrightRe.FindStringSubmatch(scanner.Text()); len(m) > 1 {
					return strings.TrimRight(strings.TrimSpace(m[1]), ".")
				}
			}
			return ""
		}
	}
	return ""
}

// detectRootDirectory checks if all entries share a common root directory prefix.
func detectRootDirectory(reader *zip.Reader) string {
	var root string
	for _, f := range reader.File {
		name := filepath.ToSlash(f.Name)
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 {
			return ""
		}
		dir := parts[0]
		if root == "" {
			root = dir
		} else if dir != root {
			return ""
		}
	}
	return root
}

// cleanDirectoryName strips version/branch suffixes from a directory or filename.
func cleanDirectoryName(name string) string {
	name = strings.TrimSpace(name)
	name = versionSuffixRe.ReplaceAllString(name, "")
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	return strings.TrimSpace(name)
}

// findIndexHTML scans the zip for the shallowest index.html or index.htm,
// respecting the same skip rules as discoverTemplates.
func findIndexHTML(reader *zip.Reader) string {
	bestPath := ""
	bestDepth := -1
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		base := strings.ToLower(filepath.Base(name))
		if base != "index.html" && base != "index.htm" {
			continue
		}
		dir := filepath.Dir(name)
		skip := false
		for _, part := range strings.Split(dir, "/") {
			if skipDirRe.MatchString(part) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		depth := strings.Count(name, "/")
		if bestDepth == -1 || depth < bestDepth {
			bestPath = name
			bestDepth = depth
		}
	}
	return bestPath
}

// discoverTemplates scans the zip for HTML files and builds template manifests.
func discoverTemplates(reader *zip.Reader) []entities.TemplateManifest {
	seen := make(map[string]int) // slug → depth
	var templates []entities.TemplateManifest

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".html" && ext != ".htm" {
			continue
		}

		base := filepath.Base(name)
		if strings.HasPrefix(base, "_") {
			continue
		}

		// Check for skipped directories
		dir := filepath.Dir(name)
		skip := false
		for _, part := range strings.Split(dir, "/") {
			if skipDirRe.MatchString(part) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		slug := identity.Slugify(strings.TrimSuffix(base, filepath.Ext(base)))
		if slug == "" {
			continue
		}
		depth := strings.Count(name, "/")

		if prevDepth, exists := seen[slug]; exists {
			if depth >= prevDepth {
				continue
			}
			// Replace with shallower file
			for i, t := range templates {
				if t.Slug == slug {
					templates[i].File = name
					templates[i].Name = titleCase(slug)
					break
				}
			}
			seen[slug] = depth
			continue
		}

		seen[slug] = depth
		templates = append(templates, entities.TemplateManifest{
			Name: titleCase(slug),
			Slug: slug,
			File: name,
		})
	}
	return templates
}

// titleCase converts a slug like "about-us" to "About Us".
func titleCase(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func extractZip(reader *zip.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create theme directory: %w", err)
	}

	for _, f := range reader.File {
		target := filepath.Join(destDir, f.Name)
		// Prevent zip slip
		if !isInsideDir(target, destDir) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func isInsideDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return !filepath.IsAbs(rel) && rel != ".." &&
		len(rel) < 3 || rel[:3] != "../"
}
