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

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
	"go.uber.org/fx"
)

// TestAccountUploadFolders drives the scenarios that give an upload an owner:
// where the file is stored, and who may read it back by its URL.
//
// It boots on accountScopedWorld, so every request passes through
// authhttp.RequireAuth and acts in the account a real sign-in resolved — the
// same identity the upload handler reads the account from. The instance runs
// against local storage only; the GCS and S3 key shapes are pinned by the unit
// tests beside those backends.
func TestAccountUploadFolders(t *testing.T) {
	runFeatureWith(t, "account upload folders", "features/account_upload_folders.feature",
		initAccountUploadFoldersScenario)
}

const (
	uploadsPath     = "/api/uploads"
	uploadFilesPath = "/api/uploads/files/"
)

// storedPhoto is a file the instance holds, by the URL path it is read back at
// and the bytes a read must return.
type storedPhoto struct {
	path string
	body []byte
}

type uploadFoldersWorld struct {
	*accountScopedWorld

	fileService       application.FileService
	permissionService application.ResourcePermissionService
	uploadDir         string

	photos  map[string]*storedPhoto // account name -> the photo it stored last
	legacy  map[string]*storedPhoto // file name -> a photo stored before account folders
	recipes map[string]string       // recipe name -> resource id

	// answers holds every answer in the order the scenario asked, so the
	// comparison steps speak about the same requests the scenario made.
	answers    []*capturedAnswer
	servedWant []byte
	// target is the stored photo the last read aimed at, whether or not the
	// caller may have it, so a refusal can be checked for leaking its bytes.
	target       []byte
	recipeAnswer *capturedAnswer
	photoAnswer  *capturedAnswer
}

func initAccountUploadFoldersScenario(sc *godog.ScenarioContext) {
	w := &uploadFoldersWorld{
		accountScopedWorld: &accountScopedWorld{
			accounts: map[string]string{},
			people:   map[string]*person{},
		},
		photos:  map[string]*storedPhoto{},
		legacy:  map[string]*storedPhoto{},
		recipes: map[string]string{},
	}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		if w.tmpDir != "" {
			// A temp dir left behind costs disk space, never a scenario's answer.
			_ = os.RemoveAll(w.tmpDir)
		}
		return ctx, nil
	})

	// --- instance and people ---
	sc.Step(`^a WeOS instance where requests are authenticated by their session$`, w.bootWithUploads)
	sc.Step(`^"([^"]*)" is signed in$`, func(name string) error { _, err := w.signIn(name); return err })
	sc.Step(`^the meal-planning preset is installed$`, w.mealPlanningInstalled)

	// --- staging ---
	sc.Step(`^"([^"]*)" has a stored photo named "([^"]*)"$`, w.hasStoredPhoto)
	sc.Step(`^"([^"]*)" has a recipe "([^"]*)" carrying a stored photo named "([^"]*)"$`, w.hasRecipeWithPhoto)
	sc.Step(`^"([^"]*)" has granted "([^"]*)" read access to the recipe "([^"]*)"$`, w.grantedReadOnRecipe)
	sc.Step(`^a photo "([^"]*)" was stored at its flat URL before files were kept by account$`, w.legacyFlatPhoto)

	// --- uploading ---
	sc.Step(`^"([^"]*)" sends a photo named "([^"]*)"$`, w.sendsPhoto)
	sc.Step(`^someone carrying no session sends a photo named "([^"]*)"$`, w.anonymousSendsPhoto)

	// --- reading ---
	sc.Step(`^"([^"]*)" requests that photo by its URL$`, w.requestsOwnPhoto)
	sc.Step(`^"([^"]*)" requests "([^"]*)"'s photo by its URL$`, w.requestsOthersPhoto)
	sc.Step(`^"([^"]*)" requests a photo at a URL in its own folder that names no stored file$`,
		w.requestsMissingInOwnFolder)
	sc.Step(`^"([^"]*)" reads the recipe "([^"]*)"$`, w.readsRecipe)
	sc.Step(`^"([^"]*)" requests the recipe's photo by its URL$`, w.requestsRecipePhoto)
	sc.Step(`^"([^"]*)" requests a file below its own folder whose path escapes it as "([^"]*)"$`,
		w.requestsEscapingPath)
	sc.Step(`^"([^"]*)" requests "([^"]*)" at its flat URL$`, w.requestsLegacyPhoto)

	// --- what came back ---
	sc.Step(`^the upload succeeds$`, w.uploadSucceeded)
	sc.Step(`^the stored file's location is under the folder for "([^"]*)"$`, w.storedUnderFolder)
	sc.Step(`^the file's URL is under the folder for "([^"]*)"$`, w.urlUnderFolder)
	sc.Step(`^the request is refused as not authenticated$`, func() error { return w.lastStatusIs(http.StatusUnauthorized) })
	sc.Step(`^nothing is stored$`, w.nothingStored)
	sc.Step(`^the photo is served$`, w.photoServed)
	sc.Step(`^the request is refused as not found$`, w.refusedAsNotFound)
	sc.Step(`^both requests are answered with the same status and the same body$`, w.lastTwoIdentical)
	sc.Step(`^the recipe "([^"]*)" is read$`, w.recipeWasRead)
	sc.Step(`^the request for its photo is refused as not found$`, w.recipePhotoRefusedNotFound)
}

// --- instance ---------------------------------------------------------------

func (w *uploadFoldersWorld) bootWithUploads() error {
	dir, err := os.MkdirTemp("", "weos-upload-folders-e2e-")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "upload-folders.db")
	w.uploadDir = filepath.Join(dir, "uploads")

	// Local storage only: with no bucket configured the provider returns the
	// local backend alone, which is what the read route serves from.
	w.setEnv("STORAGE_LOCAL_PATH", ptr(w.uploadDir))
	w.setEnv("STORAGE_GCS_BUCKET", nil)
	w.setEnv("STORAGE_S3_BUCKET", nil)

	w.extraOptions = []fx.Option{fx.Populate(&w.fileService, &w.permissionService)}
	w.mountExtraRoutes = w.mountUploadRoutes
	return w.boot(false)
}

// mountUploadRoutes mounts the upload routes through the same calls serve.go
// makes, behind the same guards as every other protected route.
func (w *uploadFoldersWorld) mountUploadRoutes(api *echo.Group, guards []echo.MiddlewareFunc) {
	uploadHandler := handlers.NewUploadHandler(w.fileService, w.logger, 0)
	api.POST("/uploads", uploadHandler.Upload, guards...)
	api.GET("/uploads/files/*", handlers.ServeUploadedFiles(w.uploadDir, w.logger), guards...)
}

// --- people -----------------------------------------------------------------

func ownerEmailFor(accountName string) string {
	return "owner@" + slugForAccountName(accountName) + ".example"
}

// stageAccount creates the account and its owner the first time a scenario
// names it, so "Cedar Realty" is one account with one owner throughout.
func (w *uploadFoldersWorld) stageAccount(name string) (*person, error) {
	email := ownerEmailFor(name)
	if _, staged := w.accounts[name]; !staged {
		if err := w.accountWithOwner(name, email, aPassword); err != nil {
			return nil, err
		}
	}
	return w.people[email], nil
}

// signIn signs the account's owner in over HTTP, once, and proves the session
// acts in that account — otherwise every folder assertion below would be
// measuring some other account.
func (w *uploadFoldersWorld) signIn(name string) (*person, error) {
	p, err := w.stageAccount(name)
	if err != nil {
		return nil, err
	}
	if p.cookie != "" {
		return p, nil
	}
	if err := w.signsIn(p.email); err != nil {
		return nil, err
	}
	if p.lastAnswer == nil || p.lastAnswer.status != http.StatusOK || p.cookie == "" {
		return nil, fmt.Errorf("%q could not sign in: %s", name, describe(p.lastAnswer))
	}
	if p.accountID != w.accounts[name] {
		return nil, fmt.Errorf("%q signed in to account %q, not its own %q", name, p.accountID, w.accounts[name])
	}
	return p, nil
}

func (w *uploadFoldersWorld) identityOf(name string) (context.Context, *person, error) {
	p, err := w.stageAccount(name)
	if err != nil {
		return nil, nil, err
	}
	accountID := w.accounts[name]
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID:         p.agentID,
		AccountIDs:      []string{accountID},
		ActiveAccountID: accountID,
	})
	return ctx, p, nil
}

// --- requests ---------------------------------------------------------------

func photoBytes(accountName, filename string) []byte {
	// A JPEG signature, so the upload handler's content sniffing sees a photo.
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, fmt.Sprintf("%s's %s", accountName, filename)...)
}

func (w *uploadFoldersWorld) do(req *http.Request, cookie string) (*capturedAnswer, error) {
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", req.URL.RequestURI(), err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read the answer to %s: %w", req.URL.RequestURI(), err)
	}
	answer := &capturedAnswer{status: res.StatusCode, body: string(raw)}
	w.answers = append(w.answers, answer)
	return answer, nil
}

func (w *uploadFoldersWorld) get(cookie, target string) (*capturedAnswer, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("could not build a request for %s: %w", target, err)
	}
	return w.do(req, cookie)
}

func (w *uploadFoldersWorld) upload(cookie, filename string, body []byte) (*capturedAnswer, error) {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(body); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, w.server.URL+uploadsPath, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return w.do(req, cookie)
}

// uploadedPath reads the URL an accepted upload handed back.
func uploadedPath(answer *capturedAnswer) (string, error) {
	var body struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &body); err != nil {
		return "", fmt.Errorf("the upload answer is not the envelope: %s", describe(answer))
	}
	if !strings.HasPrefix(body.Data.URL, uploadFilesPath) {
		return "", fmt.Errorf("the upload handed back %q, not a URL under %s", body.Data.URL, uploadFilesPath)
	}
	return body.Data.URL, nil
}

// --- staging ----------------------------------------------------------------

func (w *uploadFoldersWorld) hasStoredPhoto(name, filename string) error {
	p, err := w.signIn(name)
	if err != nil {
		return err
	}
	body := photoBytes(name, filename)
	answer, err := w.upload(p.cookie, filename, body)
	if err != nil {
		return err
	}
	if answer.status != http.StatusCreated {
		return fmt.Errorf("%q could not store %q: %s", name, filename, describe(answer))
	}
	stored, err := uploadedPath(answer)
	if err != nil {
		return err
	}
	w.photos[name] = &storedPhoto{path: stored, body: body}
	return nil
}

func (w *uploadFoldersWorld) hasRecipeWithPhoto(name, recipe, filename string) error {
	if err := w.hasStoredPhoto(name, filename); err != nil {
		return err
	}
	ctx, _, err := w.identityOf(name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(map[string]string{"name": recipe, "image": w.photos[name].path})
	if err != nil {
		return err
	}
	created, err := w.resourceService.Create(ctx, application.CreateResourceCommand{TypeSlug: "recipe", Data: data})
	if err != nil {
		return fmt.Errorf("could not stage the recipe %q for %q: %w", recipe, name, err)
	}
	w.recipes[recipe] = created.GetID()
	return nil
}

func (w *uploadFoldersWorld) grantedReadOnRecipe(owner, grantee, recipe string) error {
	id, ok := w.recipes[recipe]
	if !ok {
		return fmt.Errorf("no recipe named %q has been staged", recipe)
	}
	ctx, _, err := w.identityOf(owner)
	if err != nil {
		return err
	}
	granted, err := w.stageAccount(grantee)
	if err != nil {
		return err
	}
	err = w.permissionService.Grant(ctx, application.GrantPermissionCommand{
		ResourceID: id,
		AgentID:    granted.agentID,
		Actions:    json.RawMessage(`["read"]`),
	})
	if err != nil {
		return fmt.Errorf("%q could not grant %q read access to %q: %w", owner, grantee, recipe, err)
	}
	return nil
}

// legacyFlatPhoto writes the file where the local backend put every upload
// before account folders: flat, directly under the upload directory.
func (w *uploadFoldersWorld) legacyFlatPhoto(filename string) error {
	if err := os.MkdirAll(w.uploadDir, 0o750); err != nil {
		return fmt.Errorf("could not create the upload directory: %w", err)
	}
	stored := ksuid.New().String() + "-" + filename
	body := photoBytes("before account folders", filename)
	if err := os.WriteFile(filepath.Join(w.uploadDir, stored), body, 0o600); err != nil {
		return fmt.Errorf("could not stage the flat file %q: %w", stored, err)
	}
	w.legacy[filename] = &storedPhoto{path: uploadFilesPath + stored, body: body}
	return nil
}

// --- uploading --------------------------------------------------------------

func (w *uploadFoldersWorld) sendsPhoto(name, filename string) error {
	p, err := w.signIn(name)
	if err != nil {
		return err
	}
	body := photoBytes(name, filename)
	answer, err := w.upload(p.cookie, filename, body)
	if err != nil {
		return err
	}
	if answer.status == http.StatusCreated {
		stored, err := uploadedPath(answer)
		if err != nil {
			return err
		}
		w.photos[name] = &storedPhoto{path: stored, body: body}
	}
	return nil
}

func (w *uploadFoldersWorld) anonymousSendsPhoto(filename string) error {
	_, err := w.upload("", filename, photoBytes("nobody", filename))
	return err
}

// --- reading ----------------------------------------------------------------

func (w *uploadFoldersWorld) photoOf(name string) (*storedPhoto, error) {
	photo, ok := w.photos[name]
	if !ok {
		return nil, fmt.Errorf("%q has no stored photo", name)
	}
	return photo, nil
}

func (w *uploadFoldersWorld) requestsOwnPhoto(name string) error {
	return w.requestsOthersPhoto(name, name)
}

func (w *uploadFoldersWorld) requestsOthersPhoto(caller, owner string) error {
	p, err := w.signIn(caller)
	if err != nil {
		return err
	}
	photo, err := w.photoOf(owner)
	if err != nil {
		return err
	}
	w.servedWant = photo.body
	w.target = photo.body
	_, err = w.get(p.cookie, w.server.URL+photo.path)
	return err
}

func (w *uploadFoldersWorld) requestsMissingInOwnFolder(caller string) error {
	p, err := w.signIn(caller)
	if err != nil {
		return err
	}
	missing := uploadFilesPath + "accounts/" + w.accounts[caller] + "/uploads/" + ksuid.New().String() + "-lasagna.jpg"
	_, err = w.get(p.cookie, w.server.URL+missing)
	return err
}

func (w *uploadFoldersWorld) readsRecipe(caller, recipe string) error {
	p, err := w.signIn(caller)
	if err != nil {
		return err
	}
	id, ok := w.recipes[recipe]
	if !ok {
		return fmt.Errorf("no recipe named %q has been staged", recipe)
	}
	answer, err := w.get(p.cookie, w.server.URL+"/api/recipe/"+id)
	if err != nil {
		return err
	}
	w.recipeAnswer = answer
	return nil
}

// requestsRecipePhoto follows the photo URL the recipe read handed back, not
// the one staging remembers, so the request is the one a reader could make.
func (w *uploadFoldersWorld) requestsRecipePhoto(caller string) error {
	p, err := w.signIn(caller)
	if err != nil {
		return err
	}
	if w.recipeAnswer == nil || w.recipeAnswer.status != http.StatusOK {
		return fmt.Errorf("%q has no recipe read to take a photo URL from: %s", caller, describe(w.recipeAnswer))
	}
	image, _, err := recipeFields(w.recipeAnswer)
	if err != nil {
		return err
	}
	if image == "" {
		return fmt.Errorf("the recipe %q read carries no photo: %s", caller, w.recipeAnswer.body)
	}
	target := image
	if strings.HasPrefix(image, "/") {
		target = w.server.URL + image
	}
	answer, err := w.get(p.cookie, target)
	if err != nil {
		return err
	}
	w.photoAnswer = answer
	return nil
}

// requestsEscapingPath builds a URL that starts inside the caller's own folder
// and climbs, with the escape as written, into the other account's photo.
func (w *uploadFoldersWorld) requestsEscapingPath(caller, escape string) error {
	p, err := w.signIn(caller)
	if err != nil {
		return err
	}
	var target *storedPhoto
	for name, photo := range w.photos {
		if name == caller {
			continue
		}
		if target != nil {
			return fmt.Errorf("more than one other account has a stored photo; the escape has no single target")
		}
		target = photo
	}
	if target == nil {
		return fmt.Errorf("no other account has a stored photo to escape into")
	}
	inOtherAccount, ok := strings.CutPrefix(target.path, uploadFilesPath+"accounts/")
	if !ok {
		return fmt.Errorf("the other account's photo is not in an account folder: %s", target.path)
	}
	probe := uploadFilesPath + "accounts/" + w.accounts[caller] + "/uploads/" + escape + escape + inOtherAccount

	// A probe that does not resolve onto the other account's file proves
	// nothing by being refused, so check the arithmetic before sending it.
	decoded, err := url.PathUnescape(probe)
	if err != nil {
		return fmt.Errorf("the probe %q does not decode: %w", probe, err)
	}
	if path.Clean(decoded) != target.path {
		return fmt.Errorf("the probe %q resolves to %q, not to the other account's photo %q",
			probe, path.Clean(decoded), target.path)
	}

	req, err := http.NewRequest(http.MethodGet, w.server.URL+probe, nil)
	if err != nil {
		return fmt.Errorf("could not build the probe %q: %w", probe, err)
	}
	if !strings.Contains(req.URL.RequestURI(), escape) {
		return fmt.Errorf("the client would send %q, which no longer carries the escape %q",
			req.URL.RequestURI(), escape)
	}
	w.target = target.body
	_, err = w.do(req, p.cookie)
	return err
}

func (w *uploadFoldersWorld) requestsLegacyPhoto(caller, filename string) error {
	p, err := w.signIn(caller)
	if err != nil {
		return err
	}
	photo, ok := w.legacy[filename]
	if !ok {
		return fmt.Errorf("no flat file named %q has been staged", filename)
	}
	w.servedWant = photo.body
	_, err = w.get(p.cookie, w.server.URL+photo.path)
	return err
}

// --- what came back ---------------------------------------------------------

func (w *uploadFoldersWorld) lastAnswer() (*capturedAnswer, error) {
	if len(w.answers) == 0 {
		return nil, fmt.Errorf("no request has been made")
	}
	return w.answers[len(w.answers)-1], nil
}

func (w *uploadFoldersWorld) lastStatusIs(status int) error {
	answer, err := w.lastAnswer()
	if err != nil {
		return err
	}
	if answer.status != status {
		return fmt.Errorf("expected %d, got %s", status, describe(answer))
	}
	return nil
}

// refusedAsNotFound checks that the refusal came from the file route and holds
// nothing of the photo the request aimed at. A 404 from another handler, or a
// 404 carrying the photo's bytes, is not the refusal the scenario means.
func (w *uploadFoldersWorld) refusedAsNotFound() error {
	if err := w.lastStatusIs(http.StatusNotFound); err != nil {
		return err
	}
	answer, _ := w.lastAnswer()
	if len(w.target) == 0 {
		return fmt.Errorf("no request named a stored photo, so the refusal cannot be checked for its bytes")
	}
	if strings.Contains(answer.body, string(w.target)) {
		return fmt.Errorf("the refusal carries the photo it refused: %s", describe(answer))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(answer.body), &body); err != nil {
		return fmt.Errorf("the refusal is not a JSON error envelope: %s", describe(answer))
	}
	if len(body) != 1 || body["error"] != "file not found" {
		return fmt.Errorf(`expected the file route's refusal {"error":"file not found"}, got %s`, describe(answer))
	}
	return nil
}

func (w *uploadFoldersWorld) uploadSucceeded() error {
	if err := w.lastStatusIs(http.StatusCreated); err != nil {
		return err
	}
	answer, _ := w.lastAnswer()
	_, err := uploadedPath(answer)
	return err
}

// storedFiles lists every regular file under the upload directory, relative to
// it and slash-separated.
func (w *uploadFoldersWorld) storedFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(w.uploadDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, relErr := filepath.Rel(w.uploadDir, p)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

// storedUnderFolder finds the photo on disk by its bytes rather than by the URL
// the upload handed back, so the location is checked independently of the URL.
func (w *uploadFoldersWorld) storedUnderFolder(name string) error {
	photo, err := w.photoOf(name)
	if err != nil {
		return err
	}
	files, err := w.storedFiles()
	if err != nil {
		return fmt.Errorf("could not list the stored files: %w", err)
	}
	var found []string
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(w.uploadDir, filepath.FromSlash(f)))
		if err != nil {
			return fmt.Errorf("could not read the stored file %q: %w", f, err)
		}
		if bytes.Equal(data, photo.body) {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		return fmt.Errorf("expected the photo stored exactly once, found it at %v among %v", found, files)
	}
	folder := "accounts/" + w.accounts[name] + "/uploads"
	if path.Dir(found[0]) != folder {
		return fmt.Errorf("the photo is stored at %q, not in the folder %q", found[0], folder)
	}
	return nil
}

func (w *uploadFoldersWorld) urlUnderFolder(name string) error {
	photo, err := w.photoOf(name)
	if err != nil {
		return err
	}
	folder := uploadFilesPath + "accounts/" + w.accounts[name] + "/uploads/"
	rest, ok := strings.CutPrefix(photo.path, folder)
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return fmt.Errorf("the URL %q is not a file in the folder %q", photo.path, folder)
	}
	return nil
}

func (w *uploadFoldersWorld) nothingStored() error {
	files, err := w.storedFiles()
	if err != nil {
		return fmt.Errorf("could not list the stored files: %w", err)
	}
	if len(files) != 0 {
		return fmt.Errorf("expected nothing stored, found %v", files)
	}
	return nil
}

func (w *uploadFoldersWorld) photoServed() error {
	if err := w.lastStatusIs(http.StatusOK); err != nil {
		return err
	}
	answer, _ := w.lastAnswer()
	if answer.body != string(w.servedWant) {
		return fmt.Errorf("the file served is %q, not the photo %q", answer.body, w.servedWant)
	}
	return nil
}

func (w *uploadFoldersWorld) lastTwoIdentical() error {
	if len(w.answers) < 2 {
		return fmt.Errorf("expected two requests, saw %d", len(w.answers))
	}
	a, b := w.answers[len(w.answers)-2], w.answers[len(w.answers)-1]
	if a.status >= 200 && a.status < 300 {
		return fmt.Errorf("the other account's photo was served, so there is no refusal to compare: %s", describe(a))
	}
	if a.status != b.status {
		return fmt.Errorf("answers differ in status: %d vs %d", a.status, b.status)
	}
	if a.body != b.body {
		return fmt.Errorf("answers differ in body: %q vs %q", a.body, b.body)
	}
	return nil
}

func recipeFields(answer *capturedAnswer) (image, name string, err error) {
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(answer.body), &body); err != nil {
		return "", "", fmt.Errorf("the recipe answer is not the envelope: %s", describe(answer))
	}
	image, _ = body.Data["image"].(string)
	name, _ = body.Data["name"].(string)
	return image, name, nil
}

func (w *uploadFoldersWorld) recipeWasRead(recipe string) error {
	if w.recipeAnswer == nil || w.recipeAnswer.status != http.StatusOK {
		return fmt.Errorf("the recipe %q was not read: %s", recipe, describe(w.recipeAnswer))
	}
	_, name, err := recipeFields(w.recipeAnswer)
	if err != nil {
		return err
	}
	if name != recipe {
		return fmt.Errorf("the recipe read is named %q, not %q", name, recipe)
	}
	return nil
}

func (w *uploadFoldersWorld) recipePhotoRefusedNotFound() error {
	if w.photoAnswer == nil || w.photoAnswer.status != http.StatusNotFound {
		return fmt.Errorf("expected the photo refused as not found, got %s", describe(w.photoAnswer))
	}
	return nil
}
