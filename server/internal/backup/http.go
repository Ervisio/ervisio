package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type uploadArtifact struct {
	Name, Local, SHA256 string
	Size                int64
	Manifest            []byte
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func publishHTTP(ctx context.Context, c *Config, local, hash string, info archiveInfo) (string, []string, error) {
	fi, err := os.Stat(local)
	if err != nil {
		return "", nil, err
	}
	name := backupArchiveName(c, info)
	m := manifest{archiveInfo: info, Archive: name, Bytes: fi.Size(), SHA256: hash}
	m.MAC = m.mac(c.ownerKey)
	metadata, _ := json.Marshal(m)
	location, err := uploadHTTPArtifact(ctx, c.Destination, uploadArtifact{Name: name, Local: local, SHA256: hash, Size: fi.Size(), Manifest: metadata})
	return location, []string{}, err
}

func serviceAPI(raw string) string {
	base := strings.TrimRight(raw, "/")
	if !strings.HasSuffix(base, "/api/v1") {
		base += "/api/v1"
	}
	return base
}

func artifactMultipart(file *os.File, artifact uploadArtifact, field, folder string, withManifest bool) (io.Reader, int64, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if folder != "" {
		if err := writer.WriteField("folder", folder); err != nil {
			return nil, 0, "", err
		}
	}
	if _, err := writer.CreateFormFile(field, artifact.Name); err != nil {
		return nil, 0, "", err
	}
	header := append([]byte{}, buffer.Bytes()...)
	buffer.Reset()
	if withManifest && len(artifact.Manifest) > 0 {
		part, err := writer.CreateFormFile(field, artifact.Name+".manifest.json")
		if err != nil {
			return nil, 0, "", err
		}
		if _, err := part.Write(artifact.Manifest); err != nil {
			return nil, 0, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, 0, "", err
	}
	tail := append([]byte{}, buffer.Bytes()...)
	return io.MultiReader(bytes.NewReader(header), file, bytes.NewReader(tail)), int64(len(header)) + artifact.Size + int64(len(tail)), writer.FormDataContentType(), nil
}

func applyUploadAuth(req *http.Request, d Destination) {
	if d.APIKey == nil || *d.APIKey == "" {
		return
	}
	header, value := "Authorization", "Bearer "+*d.APIKey
	if d.Kind == "http" {
		header = d.APIHeader
		value = *d.APIKey
		if d.AuthScheme == "bearer" {
			value = "Bearer " + value
		}
	}
	req.Header.Set(header, value)
}

func uploadDo(ctx context.Context, client *http.Client, d Destination, method, target, contentType string, body io.Reader, size int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, errors.New("The upload URL is invalid.")
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", contentType)
	applyUploadAuth(req, d)
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("The external upload could not complete. Check the service URL, TLS and access; no retention was performed.")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("The external upload returned HTTP %d. Check the API key, permission and service limits; no retention was performed.", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("The upload service returned an unreadable or oversized acknowledgement.")
	}
	return data, nil
}

// External services own their objects. These adapters deliberately expose no
// delete/list-retention operation and never configure server-side expiry.
func uploadHTTPArtifact(ctx context.Context, d Destination, artifact uploadArtifact) (string, error) {
	if err := validateDestination(&d, true); err != nil {
		return "", err
	}
	client := backupHTTPClient()
	defer client.CloseIdleConnections()
	if d.Kind == "repo" {
		return uploadRepoArtifact(ctx, client, d, artifact)
	}
	file, err := os.Open(artifact.Local)
	if err != nil {
		return "", err
	}
	defer file.Close()
	target, method := d.URL, d.Method
	var body io.Reader = file
	size, contentType := artifact.Size, "application/octet-stream"
	if d.Kind == "drive" {
		target, method = serviceAPI(d.URL)+"/files/upload", "POST"
	}
	if d.Kind == "drive" || d.UploadMode == "multipart" {
		field := d.Field
		if d.Kind == "drive" {
			field = "files"
		}
		body, size, contentType, err = artifactMultipart(file, artifact, field, d.Folder, d.Kind == "drive")
		if err != nil {
			return "", err
		}
	}
	target = strings.ReplaceAll(target, "{filename}", url.PathEscape(artifact.Name))
	response, err := uploadDo(ctx, client, d, method, target, contentType, body, size)
	if err != nil {
		return "", err
	}
	if d.Kind == "drive" {
		var result struct {
			Success bool `json:"success"`
			Data    []struct {
				ID   string `json:"_id"`
				Name string `json:"name"`
				Size int64  `json:"size"`
			} `json:"data"`
		}
		if json.Unmarshal(response, &result) != nil || !result.Success {
			return "", errors.New("ErvisioDrive did not acknowledge the uploaded files.")
		}
		archiveID := ""
		manifestOK := len(artifact.Manifest) == 0
		for _, file := range result.Data {
			if file.Name == artifact.Name && file.Size == artifact.Size && regexpFileID(file.ID) {
				archiveID = file.ID
			}
			if file.Name == artifact.Name+".manifest.json" && file.Size == int64(len(artifact.Manifest)) {
				manifestOK = true
			}
		}
		if archiveID == "" || !manifestOK {
			return "", errors.New("ErvisioDrive did not confirm the archive and its provenance manifest sizes.")
		}
		return serviceAPI(d.URL) + "/files/" + archiveID + "/download", nil
	}
	return target, nil
}

func regexpFileID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func uploadRepoArtifact(ctx context.Context, client *http.Client, d Destination, artifact uploadArtifact) (string, error) {
	base := serviceAPI(d.URL) + "/repos/" + url.PathEscape(d.Owner) + "/" + url.PathEscape(d.Repository) + "/releases"
	// Drafts do not create tags or change a repository's branch history.
	input, _ := json.Marshal(map[string]any{"tag": "ervisio-backup-" + newID(), "name": artifact.Name, "body": "Ervisio snapshot. SHA-256: " + artifact.SHA256, "draft": true, "prerelease": false, "target": d.Target})
	response, err := uploadDo(ctx, client, d, "POST", base, "application/json", bytes.NewReader(input), int64(len(input)))
	if err != nil {
		return "", err
	}
	var release struct {
		ID    string `json:"id"`
		Draft bool   `json:"draft"`
	}
	if json.Unmarshal(response, &release) != nil || !release.Draft || !uuidReference(release.ID) {
		return "", errors.New("ErvisioRepo did not acknowledge a private draft release.")
	}
	assetURL := base + "/" + release.ID + "/assets?name="
	file, err := os.Open(artifact.Local)
	if err != nil {
		return "", err
	}
	defer file.Close()
	response, err = uploadDo(ctx, client, d, "POST", assetURL+url.QueryEscape(artifact.Name), "application/octet-stream", file, artifact.Size)
	if err != nil {
		return "", err
	}
	if !validRepoAsset(response, artifact.Name, artifact.Size, artifact.SHA256) {
		return "", errors.New("ErvisioRepo did not confirm the uploaded archive size and SHA-256.")
	}
	if len(artifact.Manifest) > 0 {
		digest := sha256Hex(artifact.Manifest)
		name := artifact.Name + ".manifest.json"
		response, err = uploadDo(ctx, client, d, "POST", assetURL+url.QueryEscape(name), "application/json", bytes.NewReader(artifact.Manifest), int64(len(artifact.Manifest)))
		if err != nil {
			return "", err
		}
		if !validRepoAsset(response, name, int64(len(artifact.Manifest)), digest) {
			return "", errors.New("ErvisioRepo did not confirm the provenance manifest.")
		}
	}
	return base + "/" + release.ID, nil
}

func uuidReference(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
func validRepoAsset(data []byte, name string, size int64, hash string) bool {
	var asset struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	return json.Unmarshal(data, &asset) == nil && asset.Name == name && asset.Size == size && strings.EqualFold(asset.SHA256, hash)
}
