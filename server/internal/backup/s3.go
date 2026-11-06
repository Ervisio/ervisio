package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

func backupHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func s3Client(ctx context.Context, d Destination) (*s3.Client, error) {
	if err := validateDestination(&d, true); err != nil {
		return nil, err
	}
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(d.Region), awsconfig.WithRetryMaxAttempts(3), awsconfig.WithHTTPClient(backupHTTPClient())}
	if d.CredentialMode == "keys" {
		token := ""
		if d.SessionToken != nil {
			token = *d.SessionToken
		}
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(*d.AccessKey, *d.SecretKey, token)))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, errors.New("AWS credentials or region could not be loaded by this account.")
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.UsePathStyle = d.PathStyle
		if d.Endpoint != "" {
			options.BaseEndpoint = aws.String(d.Endpoint)
		}
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	}), nil
}

func s3Code(err error, code string) bool {
	var api smithy.APIError
	return errors.As(err, &api) && api.ErrorCode() == code
}

func s3Read(ctx context.Context, client *s3.Client, bucket, key string, limit int64) ([]byte, string, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return nil, "", err
	}
	defer object.Body.Close()
	if object.ContentLength != nil && *object.ContentLength > limit {
		return nil, "", errors.New("S3 backup metadata exceeds its size limit.")
	}
	data, err := io.ReadAll(io.LimitReader(object.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", errors.New("S3 backup metadata exceeds its size limit.")
	}
	return data, aws.ToString(object.ETag), nil
}

func s3PutMetadata(ctx context.Context, client *s3.Client, bucket, key string, data []byte) (*s3.PutObjectOutput, error) {
	return client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String("application/json"), IfNoneMatch: aws.String("*")})
}

func s3Owner(ctx context.Context, client *s3.Client, c *Config, prefix string) error {
	digest := sha256.Sum256(c.ownerKey)
	expected, _ := json.Marshal(map[string]string{"schema": "ervisio-backup-owner-v1", "jobId": c.ID, "keyId": hex.EncodeToString(digest[:])})
	key := prefix + ".ervisio-backup.json"
	actual, _, err := s3Read(ctx, client, c.Destination.Bucket, key, 4096)
	if s3Code(err, "NoSuchKey") || s3Code(err, "NotFound") {
		_, err = s3PutMetadata(ctx, client, c.Destination.Bucket, key, expected)
		if err != nil && !s3Code(err, "PreconditionFailed") {
			return errors.New("The S3 backup ownership marker could not be created conditionally.")
		}
		actual, _, err = s3Read(ctx, client, c.Destination.Bucket, key, 4096)
	}
	if err != nil || !bytes.Equal(actual, expected) {
		return errors.New("The S3 destination ownership marker is unavailable or belongs to another plan; no existing backups were deleted.")
	}
	return nil
}

func backupArchiveName(c *Config, info archiveInfo) string {
	stamp, _ := time.Parse(time.RFC3339, info.Created)
	return "ervisio-backup-" + c.ID + "-" + stamp.UTC().Format("20060102T150405Z") + "-" + info.RunID + ".tar.gz"
}

func publishS3(ctx context.Context, c *Config, local, hash string, info archiveInfo) (string, []string, error) {
	client, err := s3Client(ctx, c.Destination)
	if err != nil {
		return "", nil, err
	}
	prefix := path.Join(c.Destination.Path, "ervisio-backup-"+c.ID) + "/"
	if err := s3Owner(ctx, client, c, prefix); err != nil {
		return "", nil, err
	}
	file, err := os.Open(local)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	name := backupArchiveName(c, info)
	manager := transfermanager.New(client, func(options *transfermanager.Options) {
		options.Concurrency = 1
		options.PartSizeBytes = 8 << 20
		options.MultipartUploadThreshold = 16 << 20
		options.FailTimeout = 10 * time.Second
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	_, err = manager.UploadObject(ctx, &transfermanager.UploadObjectInput{Bucket: aws.String(c.Destination.Bucket), Key: aws.String(prefix + name), Body: file, ContentLength: aws.Int64(fi.Size()), ContentType: aws.String("application/gzip"), IfNoneMatch: aws.String("*"), Metadata: map[string]string{"ervisio-plan": c.ID, "sha256": hash}})
	if err != nil {
		return "", nil, errors.New("S3 upload failed. Check this account's credentials, bucket access, endpoint and available space; previous backups were preserved.")
	}
	remote, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.Destination.Bucket), Key: aws.String(prefix + name)})
	if err != nil || aws.ToInt64(remote.ContentLength) != fi.Size() {
		return "", nil, errors.New("The completed S3 archive size could not be verified; retention was not run.")
	}
	m := manifest{archiveInfo: info, Archive: name, Bytes: fi.Size(), SHA256: hash}
	m.MAC = m.mac(c.ownerKey)
	metadata, _ := json.Marshal(m)
	if _, err = s3PutMetadata(ctx, client, c.Destination.Bucket, prefix+name+".manifest.json", metadata); err != nil {
		return "", nil, errors.New("The archive uploaded to S3, but its provenance manifest could not be committed; previous backups were preserved.")
	}
	warnings := []string{}
	if c.Destination.Retention {
		if err := retainS3(ctx, client, c, prefix, name, time.Now()); err != nil {
			warnings = append(warnings, "The S3 backup is complete, but retention could not finish safely. Existing objects were preserved where cleanup failed. Versioned buckets retain old versions according to their bucket lifecycle.")
			retentionLog(ctx, "retention.failed", "WARN", "S3 retention cleanup was incomplete", info.RunID, 0)
		}
	}
	return "s3://" + c.Destination.Bucket + "/" + prefix + name, warnings, nil
}

// Verify the endpoint enforces conditional deletion using only a disposable,
// uniquely named probe. Older S3-compatible servers may ignore If-Match.
func s3ConditionalDelete(ctx context.Context, client *s3.Client, bucket, prefix string) error {
	key := prefix + ".ervisio-probe-" + newID()
	created, err := s3PutMetadata(ctx, client, bucket, key, []byte(`{"probe":"ervisio-retention"}`))
	if err != nil || aws.ToString(created.ETag) == "" {
		return errors.New("S3 conditional deletion could not be verified.")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), IfMatch: created.ETag})
	}()
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), IfMatch: aws.String(`"` + newID() + `"`)})
	if !s3Code(err, "PreconditionFailed") {
		return errors.New("This S3 endpoint did not enforce conditional deletion; automatic cleanup was refused.")
	}
	return nil
}

func retainS3(ctx context.Context, client *s3.Client, c *Config, prefix, protected string, now time.Time) error {
	if err := s3ConditionalDelete(ctx, client, c.Destination.Bucket, prefix); err != nil {
		return err
	}
	list := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(c.Destination.Bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(500)}, func(options *s3.ListObjectsV2PaginatorOptions) { options.StopOnDuplicateToken = true })
	pattern := regexp.MustCompile(`^ervisio-backup-` + regexp.QuoteMeta(c.ID) + `-[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}\.tar\.gz$`)
	type candidate struct {
		m       manifest
		created time.Time
		etag    string
	}
	items := []candidate{}
	count := 0
	pages := 0
	for list.HasMorePages() {
		pages++
		if pages > 20 {
			return errors.New("Too many S3 result pages for safe retention.")
		}
		page, err := list.NextPage(ctx)
		if err != nil {
			return err
		}
		count += len(page.Contents)
		if count > 5000 {
			return errors.New("Too many S3 destination objects for safe retention.")
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			name := strings.TrimSuffix(strings.TrimPrefix(key, prefix), ".manifest.json")
			if !strings.HasSuffix(key, ".manifest.json") || !pattern.MatchString(name) {
				continue
			}
			data, etag, err := s3Read(ctx, client, c.Destination.Bucket, key, 64<<10)
			if err != nil {
				continue
			}
			var m manifest
			if decodeStrict(data, &m) != nil || m.JobID != c.ID || m.Schema != "ervisio-backup-v1" || m.Archive != name || !idPattern.MatchString(m.RunID) || !m.authentic(c.ownerKey) || etag == "" {
				continue
			}
			created, err := time.Parse(time.RFC3339, m.Created)
			if err != nil {
				continue
			}
			archive, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.Destination.Bucket), Key: aws.String(prefix + m.Archive)})
			if err != nil || aws.ToInt64(archive.ContentLength) != m.Bytes || aws.ToString(archive.ETag) == "" {
				continue
			}
			items = append(items, candidate{m, created, etag})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].created.Equal(items[j].created) {
			return items[i].m.RunID > items[j].m.RunID
		}
		return items[i].created.After(items[j].created)
	})
	var cutoff time.Time
	if c.MaxAge != nil {
		period := *c.MaxAge
		period.Value = -period.Value
		cutoff = period.Next(now, now.Day())
	}
	for i, item := range items {
		if i == 0 || item.m.Archive == protected || i < c.KeepLast && (cutoff.IsZero() || !item.created.Before(cutoff)) {
			continue
		}
		key := prefix + item.m.Archive
		current, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.Destination.Bucket), Key: aws.String(key)})
		if err != nil || aws.ToInt64(current.ContentLength) != item.m.Bytes || aws.ToString(current.ETag) == "" {
			continue
		}
		if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.Destination.Bucket), Key: aws.String(key), IfMatch: current.ETag}); err != nil {
			return errors.New("An S3 archive changed or could not be deleted conditionally.")
		}
		retentionLog(ctx, "retention.deleted", "INFO", "Expired S3 backup archive deleted", item.m.RunID, item.m.Bytes)
		if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.Destination.Bucket), Key: aws.String(key + ".manifest.json"), IfMatch: aws.String(item.etag)}); err != nil {
			return errors.New("An S3 provenance manifest changed or could not be deleted conditionally.")
		}
	}
	return nil
}
