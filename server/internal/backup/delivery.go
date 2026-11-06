package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ervisio/ervisio/server/internal/modules/remote"
)

type deliveryRequest struct {
	Destination Destination `json:"destination"`
	Name        string      `json:"name"`
	Parts       int         `json:"parts"`
	Bytes       int64       `json:"bytes"`
}

// Export payloads are confined to this account's fixed database staging folder.
// Requests cannot supply an arbitrary source path or execute an elevated bridge.
func (e *engine) deliver(ctx context.Context, token string) (any, error) {
	var request deliveryRequest
	if err := e.store.inbox(token, &request); err != nil {
		return nil, err
	}
	if request.Parts < 1 || request.Parts > 64 || request.Bytes < 1 || request.Bytes > 64<<20 || request.Name == "" || len(request.Name) > 180 || !text(request.Name, 180) || strings.ContainsAny(request.Name, "/\\") || request.Name == "." || request.Name == ".." {
		return nil, errors.New("Invalid export filename or size.")
	}
	if err := validateDestination(&request.Destination, true); err != nil {
		return nil, err
	}
	unlock, err := lockFile(filepath.Join(e.store.dir, "delivery.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	source := filepath.Join(filepath.Dir(e.store.dir), "database", "exports")
	fi, err := os.Lstat(source)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || !privateOwner(fi) {
		return nil, errors.New("The private database export folder is unavailable.")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	staged, err := os.CreateTemp(e.store.dir, "delivery-*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	hash := sha256.New()
	var size int64
	for index := 0; index < request.Parts; index++ {
		name := fmt.Sprintf("%s-%04d.part", token, index)
		file, openErr := openArchiveRead(root, name, false)
		if openErr != nil {
			return nil, errors.New("An export part cannot be read safely.")
		}
		stat, statErr := file.Stat()
		if statErr != nil || !stat.Mode().IsRegular() || !privateOwner(stat) || stat.Size() > 4<<20 {
			file.Close()
			return nil, errors.New("An export part is invalid.")
		}
		n, copyErr := io.Copy(io.MultiWriter(staged, hash), io.LimitReader(file, (4<<20)+1))
		file.Close()
		size += n
		if copyErr != nil || size > request.Bytes {
			return nil, errors.New("The export exceeds its declared size.")
		}
	}
	if size != request.Bytes {
		return nil, errors.New("The staged export is incomplete.")
	}
	if err = staged.Sync(); err != nil {
		return nil, err
	}
	if err = staged.Close(); err != nil {
		return nil, err
	}
	artifact := uploadArtifact{Name: request.Name, Local: staged.Name(), SHA256: hex.EncodeToString(hash.Sum(nil)), Size: size}
	location, err := e.deliverArtifact(ctx, request.Destination, artifact)
	if err != nil {
		return nil, err
	}
	return map[string]any{"location": location, "bytes": size, "sha256": artifact.SHA256, "retention": false}, nil
}

func (e *engine) deliverArtifact(ctx context.Context, d Destination, a uploadArtifact) (string, error) {
	if d.externalHTTP() {
		return uploadHTTPArtifact(ctx, d, a)
	}
	file, err := os.Open(a.Local)
	if err != nil {
		return "", err
	}
	defer file.Close()
	switch d.kind() {
	case "s3":
		client, err := s3Client(ctx, d)
		if err != nil {
			return "", err
		}
		key := path.Join(d.Path, a.Name)
		manager := transfermanager.New(client, func(o *transfermanager.Options) {
			o.Concurrency = 1
			o.PartSizeBytes = 8 << 20
			o.MultipartUploadThreshold = 16 << 20
			o.FailTimeout = 10 * time.Second
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		})
		_, err = manager.UploadObject(ctx, &transfermanager.UploadObjectInput{Bucket: aws.String(d.Bucket), Key: aws.String(key), Body: file, ContentLength: aws.Int64(a.Size), ContentType: aws.String("application/octet-stream"), IfNoneMatch: aws.String("*"), Metadata: map[string]string{"sha256": a.SHA256}})
		if err != nil {
			return "", errors.New("S3 export upload failed; existing objects were preserved.")
		}
		head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(d.Bucket), Key: aws.String(key)})
		if err != nil || aws.ToInt64(head.ContentLength) != a.Size {
			return "", errors.New("The S3 export size could not be verified.")
		}
		return "s3://" + d.Bucket + "/" + key, nil
	case "local":
		root, err := os.OpenRoot(d.Path)
		if err != nil {
			return "", err
		}
		defer root.Close()
		temporary := ".ervisio-export-" + newID() + ".tmp"
		output, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		defer root.Remove(temporary)
		_, err = io.Copy(output, file)
		if err == nil {
			err = output.Sync()
		}
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return "", err
		}
		if err = linkWithinRoot(root, temporary, a.Name); err != nil {
			return "", errors.New("The export could not be saved without replacing an existing file.")
		}
		return filepath.Join(d.Path, a.Name), nil
	default:
		target := path.Join(d.Path, a.Name)
		err = e.withRemote(ctx, d.Connection, func(ctx context.Context, fs remote.FS) error { return uploadAtomic(ctx, fs, target, file, a.Size) })
		return target, err
	}
}
