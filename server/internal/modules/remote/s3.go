package remote

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// s3FS is one bucket of an S3-compatible object store. Folders are key
// prefixes; an empty folder is a zero-byte object whose key ends in "/".
type s3FS struct {
	client *s3.Client
	bucket string
}

func dialS3(ctx context.Context, spec *Conn) (*s3FS, error) {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
	client := s3.New(s3.Options{
		Region:       spec.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(spec.User, spec.secret(secPassword), ""),
		UsePathStyle: spec.PathStyle,
		// Credentials are never replayed to another place.
		HTTPClient: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		// Most S3-compatible stores reject the newer default checksums.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}, func(o *s3.Options) {
		if spec.URL != "" {
			o.BaseEndpoint = aws.String(spec.URL)
		}
	})
	f := &s3FS{client: client, bucket: spec.Bucket}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	_, err := client.ListObjectsV2(dctx, &s3.ListObjectsV2Input{Bucket: &f.bucket, MaxKeys: aws.Int32(1)})
	if err == nil {
		return f, nil
	}
	var ue *url.Error
	switch {
	case s3Code(err, "NoSuchBucket"):
		return nil, rpc.Errorf(rpc.NotFound, "There is no bucket called %q at this address. Check the name, the region and the endpoint.", spec.Bucket)
	case errors.Is(s3Err("", err), fs.ErrPermission):
		return nil, rpc.Errorf(rpc.Forbidden, "The object storage refused the access key. Check the key, the secret and that the key may use the bucket %q.", spec.Bucket)
	case errors.As(err, &ue):
		return nil, rpc.Errorf(rpc.Unavailable, "Could not connect to the object storage: %s", netErrText(ue.Err))
	}
	return nil, rpc.Errorf(rpc.Unavailable, "The bucket %q could not be opened: %v", spec.Bucket, err)
}

func s3Code(err error, codes ...string) bool {
	var api smithy.APIError
	if !errors.As(err, &api) {
		return false
	}
	for _, c := range codes {
		if api.ErrorCode() == c {
			return true
		}
	}
	return false
}

// s3Err names the errors remoteErr tells apart.
func s3Err(p string, err error) error {
	switch {
	case err == nil:
		return nil
	case s3Code(err, "NoSuchKey", "NotFound", "NoSuchBucket"):
		return &fs.PathError{Op: "s3", Path: p, Err: fs.ErrNotExist}
	case s3Code(err, "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "Forbidden"):
		return &fs.PathError{Op: "s3", Path: p, Err: fs.ErrPermission}
	}
	return err
}

func s3Key(p string) string { return strings.TrimPrefix(p, "/") }

// s3Prefix is the key prefix of everything inside the folder p.
func s3Prefix(p string) string {
	if p == "/" {
		return ""
	}
	return s3Key(p) + "/"
}

func s3File(dir, name string, size int64, modified *time.Time) Entry {
	e := Entry{Name: name, Path: path.Join(dir, name), Type: "file", Size: size, Mode: "0644", Perm: "rw-r--r--", Mime: mimeOf(name)}
	if modified != nil {
		e.MTime = modified.UnixMilli()
	}
	return e
}

func (f *s3FS) List(ctx context.Context, dir string) ([]Entry, error) {
	prefix := s3Prefix(dir)
	pages := s3.NewListObjectsV2Paginator(f.client, &s3.ListObjectsV2Input{Bucket: &f.bucket, Prefix: &prefix, Delimiter: aws.String("/")})
	out := []Entry{}
	found := dir == "/"
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, s3Err(dir, err)
		}
		found = found || len(page.CommonPrefixes)+len(page.Contents) > 0
		for _, cp := range page.CommonPrefixes {
			if name := strings.TrimSuffix(strings.TrimPrefix(aws.ToString(cp.Prefix), prefix), "/"); name != "" {
				out = append(out, dirEntry(path.Join(dir, name)))
			}
		}
		for _, o := range page.Contents {
			if name := strings.TrimPrefix(aws.ToString(o.Key), prefix); name != "" {
				out = append(out, s3File(dir, name, aws.ToInt64(o.Size), o.LastModified))
			}
		}
		if len(out) > maxList {
			return nil, rpc.Errorf(rpc.Invalid, "%s holds more than %d items.", dir, maxList)
		}
	}
	if !found {
		return nil, &fs.PathError{Op: "s3", Path: dir, Err: fs.ErrNotExist}
	}
	return out, nil
}

func (f *s3FS) Stat(ctx context.Context, p string) (Entry, error) {
	if p == "/" {
		return dirEntry("/"), nil
	}
	head, err := f.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &f.bucket, Key: aws.String(s3Key(p))})
	if err == nil {
		return s3File(path.Dir(p), path.Base(p), aws.ToInt64(head.ContentLength), head.LastModified), nil
	}
	if err = s3Err(p, err); !errors.Is(err, fs.ErrNotExist) {
		return Entry{}, err
	}
	keys, lerr := f.keys(ctx, s3Prefix(p), 1)
	if lerr != nil {
		return Entry{}, lerr
	}
	if len(keys) == 0 {
		return Entry{}, err
	}
	return dirEntry(p), nil
}

// keys returns up to limit keys below prefix (limit 0: all of them).
func (f *s3FS) keys(ctx context.Context, prefix string, limit int) ([]string, error) {
	in := &s3.ListObjectsV2Input{Bucket: &f.bucket, Prefix: &prefix}
	if limit > 0 {
		in.MaxKeys = aws.Int32(int32(limit))
	}
	pages := s3.NewListObjectsV2Paginator(f.client, in)
	var out []string
	for pages.HasMorePages() && (limit == 0 || len(out) < limit) {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, s3Err("/"+prefix, err)
		}
		for _, o := range page.Contents {
			out = append(out, aws.ToString(o.Key))
		}
	}
	return out, nil
}

func (f *s3FS) Mkdir(ctx context.Context, p string) error {
	_, err := f.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &f.bucket, Key: aws.String(s3Prefix(p)), Body: http.NoBody, ContentLength: aws.Int64(0)})
	return s3Err(p, err)
}

func (f *s3FS) copy(ctx context.Context, from, to string) error {
	// The copy source is a URL path: the bucket, then the escaped key.
	src := f.bucket + "/" + strings.ReplaceAll(url.PathEscape(from), "%2F", "/")
	_, err := f.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: &f.bucket, Key: &to, CopySource: &src})
	if s3Code(err, "InvalidRequest", "EntityTooLarge") {
		return rpc.Errorf(rpc.Invalid, "/%s is too large to be moved or renamed inside the object storage (5 GiB at most). Download it and upload it again.", from)
	}
	return s3Err("/"+from, err)
}

// Rename copies and then deletes: object stores cannot move a key.
func (f *s3FS) Rename(ctx context.Context, from, to string) error {
	e, err := f.Stat(ctx, from)
	if err != nil {
		return err
	}
	if e.Type != "dir" {
		if err := f.copy(ctx, s3Key(from), s3Key(to)); err != nil {
			return err
		}
		return f.Remove(ctx, from, false)
	}
	keys, err := f.keys(ctx, s3Prefix(from), 0)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := f.copy(ctx, k, s3Prefix(to)+strings.TrimPrefix(k, s3Prefix(from))); err != nil {
			return err
		}
	}
	return f.deleteKeys(ctx, keys)
}

func (f *s3FS) Remove(ctx context.Context, p string, dir bool) error {
	key := s3Key(p)
	if dir {
		key = s3Prefix(p)
		keys, err := f.keys(ctx, key, 2)
		if err != nil {
			return err
		}
		if len(keys) > 1 || len(keys) == 1 && keys[0] != key {
			return rpc.Errorf(rpc.Conflict, "%s is not empty.", p)
		}
	}
	_, err := f.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &f.bucket, Key: &key})
	return s3Err(p, err)
}

// RemoveAll deletes a file or a whole folder in batches of 1000 keys.
func (f *s3FS) RemoveAll(ctx context.Context, p string) error {
	e, err := f.Stat(ctx, p)
	if err != nil {
		return err
	}
	if e.Type != "dir" {
		return f.Remove(ctx, p, false)
	}
	keys, err := f.keys(ctx, s3Prefix(p), 0)
	if err != nil {
		return err
	}
	return f.deleteKeys(ctx, keys)
}

func (f *s3FS) deleteKeys(ctx context.Context, keys []string) error {
	for len(keys) > 0 {
		n := min(len(keys), 1000)
		ids := make([]types.ObjectIdentifier, n)
		for i, k := range keys[:n] {
			ids[i] = types.ObjectIdentifier{Key: aws.String(k)}
		}
		out, err := f.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: &f.bucket, Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)}})
		if err != nil {
			return s3Err("/"+keys[0], err)
		}
		if len(out.Errors) > 0 {
			return rpc.Errorf(rpc.Forbidden, "/%s could not be deleted: %s", aws.ToString(out.Errors[0].Key), aws.ToString(out.Errors[0].Message))
		}
		keys = keys[n:]
	}
	return nil
}

func (f *s3FS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	// No ctx: the body outlives this call (it is read by the stream).
	out, err := f.client.GetObject(context.WithoutCancel(ctx), &s3.GetObjectInput{Bucket: &f.bucket, Key: aws.String(s3Key(p))})
	if err != nil {
		return nil, s3Err(p, err)
	}
	return out.Body, nil
}

func (f *s3FS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	up := transfermanager.New(f.client, func(o *transfermanager.Options) {
		o.Concurrency = 1
		o.PartSizeBytes = 16 << 20
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return newPipeWriter(func(r io.Reader) error {
		in := &transfermanager.UploadObjectInput{Bucket: &f.bucket, Key: aws.String(s3Key(p)), Body: r}
		if size == 0 {
			in.Body = http.NoBody
		}
		_, err := up.UploadObject(context.WithoutCancel(ctx), in)
		return s3Err(p, err)
	}), nil
}

// atomicCreate: an object only appears once its upload is complete.
func (f *s3FS) atomicCreate() {}

func (f *s3FS) Close() error { return nil }
