package backup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

func (e *engine) probeDestination(ctx context.Context, c *Config) (map[string]any, error) {
	d := c.Destination
	result := map[string]any{"reachable": false, "path": d.Path, "writeTested": false, "authTested": false, "kind": d.kind()}
	if err := validateDestination(&d, true); err != nil {
		return result, err
	}
	var err error
	switch d.kind() {
	case "local":
		var root *os.Root
		root, err = os.OpenRoot(d.Path)
		if err == nil {
			err = root.Close()
		}
	case "remote":
		err = e.withRemote(ctx, d.Connection, func(ctx context.Context, f remote.FS) error {
			entry, err := f.Stat(ctx, d.Path)
			if err == nil && entry.Type != "dir" {
				return errors.New("The destination is not a real directory.")
			}
			return err
		})
		result["authTested"] = err == nil
	case "s3":
		client, clientErr := s3Client(ctx, d)
		if clientErr != nil {
			return result, clientErr
		}
		_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(d.Bucket)})
		result["authTested"] = err == nil
	default:
		client := backupHTTPClient()
		defer client.CloseIdleConnections()
		method, target := http.MethodHead, strings.ReplaceAll(d.URL, "{filename}", "ervisio-connection-check")
		if d.Kind == "drive" {
			method, target = http.MethodGet, serviceAPI(d.URL)+"/me"
		}
		if d.Kind == "repo" {
			method, target = http.MethodGet, serviceAPI(d.URL)+"/repos/"+d.Owner+"/"+d.Repository
		}
		req, requestErr := http.NewRequestWithContext(ctx, method, target, nil)
		if requestErr != nil {
			return result, errors.New("The service URL is invalid.")
		}
		applyUploadAuth(req, d)
		response, requestErr := client.Do(req)
		if requestErr != nil {
			return result, errors.New("The upload service could not be reached securely.")
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return result, errors.New("The upload service rejected the API key or permission.")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			if d.Kind != "http" || response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusBadRequest {
				return result, errors.New("The upload service did not accept the connection check.")
			}
		} else {
			result["authTested"] = d.Kind != "http"
		}
	}
	if err != nil {
		return result, errors.New("The destination could not be checked. Verify this account's access and service availability.")
	}
	result["reachable"] = true
	return result, nil
}
