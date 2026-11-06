package backup

import (
	"errors"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

func (d Destination) kind() string {
	if d.Kind == "" {
		return "remote"
	}
	return d.Kind
}
func (d Destination) externalHTTP() bool {
	return d.Kind == "http" || d.Kind == "drive" || d.Kind == "repo"
}
func (d *Destination) secrets() map[string]**string {
	return map[string]**string{"apiKey": &d.APIKey, "accessKey": &d.AccessKey, "secretKey": &d.SecretKey, "sessionToken": &d.SessionToken}
}

func sameCredentialTarget(a, b Destination) bool {
	return a.kind() == b.kind() && a.URL == b.URL && a.Endpoint == b.Endpoint && a.Bucket == b.Bucket && a.CredentialMode == b.CredentialMode
}

func destinationURL(raw string, allowHTTP bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !text(raw, 2048) {
		return nil, errors.New("Choose an upload URL without embedded credentials, query parameters or fragments.")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (allowHTTP || u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return nil, errors.New("Use HTTPS or explicitly allow an unencrypted HTTP connection.")
	}
	return u, nil
}

var fieldPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,99}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)
var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func validateDestination(d *Destination, requireSecrets bool) error {
	for _, secret := range d.secrets() {
		if *secret != nil && !text(**secret, 8192) {
			return errors.New("Destination credentials cannot contain control characters.")
		}
	}
	switch d.kind() {
	case "remote":
		if !connectionPattern.MatchString(d.Connection) {
			return errors.New("Choose a saved SFTP or SMB connection.")
		}
		p, err := cleanDestination(d.Path)
		if err != nil {
			return err
		}
		d.Path = p
	case "local":
		if !text(d.Path, 4096) || !filepath.IsAbs(d.Path) {
			return errors.New("Choose an absolute destination folder on this server.")
		}
		d.Path = filepath.Clean(d.Path)
	case "http", "drive", "repo":
		if _, err := destinationURL(d.URL, d.AllowHTTP); err != nil {
			return err
		}
		if d.Kind == "http" {
			if d.Method == "" {
				d.Method = "POST"
			}
			if d.Method != "POST" && d.Method != "PUT" {
				return errors.New("Choose POST or PUT for the upload method.")
			}
			if d.UploadMode == "" {
				d.UploadMode = "multipart"
			}
			if d.UploadMode != "multipart" && d.UploadMode != "raw" {
				return errors.New("Choose a multipart or raw upload body.")
			}
			if d.Field == "" {
				d.Field = "file"
			}
			if !fieldPattern.MatchString(d.Field) {
				return errors.New("Choose a valid multipart file field.")
			}
			if d.APIHeader == "" {
				d.APIHeader = "Authorization"
			}
			header := strings.ToLower(d.APIHeader)
			if !fieldPattern.MatchString(d.APIHeader) || strings.HasPrefix(header, "proxy-") || strings.HasPrefix(header, "sec-") || strings.HasPrefix(header, "x-amz-") {
				return errors.New("Choose a valid API credential header.")
			}
			switch header {
			case "host", "connection", "content-length", "content-type", "transfer-encoding", "cookie", "origin", "referer", "trailer", "te", "upgrade", "accept-encoding":
				return errors.New("That header is controlled by the upload transport.")
			}
			if d.AuthScheme == "" {
				d.AuthScheme = "bearer"
			}
			if d.AuthScheme != "bearer" && d.AuthScheme != "raw" {
				return errors.New("Choose Bearer or a raw API key header.")
			}
		}
		if d.Kind == "drive" && (!text(d.Folder, 150) || strings.ContainsAny(d.Folder, "/\\")) {
			return errors.New("Choose an ErvisioDrive folder slug, not a filesystem path.")
		}
		if d.Kind == "repo" && (!repositoryPattern.MatchString(d.Owner) || !repositoryPattern.MatchString(d.Repository) || !text(d.Target, 255)) {
			return errors.New("Choose an ErvisioRepo owner, repository and optional target branch.")
		}
		if requireSecrets && d.Kind != "http" && (d.APIKey == nil || *d.APIKey == "") {
			return errors.New("Set an API token for the upload service.")
		}
	case "s3":
		if !bucketPattern.MatchString(d.Bucket) || strings.Contains(d.Bucket, "..") {
			return errors.New("Choose a valid S3 bucket name.")
		}
		if d.Region == "" {
			d.Region = "us-east-1"
		}
		if !regexp.MustCompile(`^[a-z0-9-]{1,64}$`).MatchString(d.Region) {
			return errors.New("Choose a valid S3 region.")
		}
		if d.Endpoint != "" {
			if _, err := destinationURL(d.Endpoint, d.AllowHTTP); err != nil {
				return err
			}
		}
		if !text(d.Path, 1024) || strings.Contains(d.Path, `\`) {
			return errors.New("Choose a valid S3 prefix.")
		}
		for _, part := range strings.Split(d.Path, "/") {
			if part == ".." || part == "." {
				return errors.New("S3 prefixes cannot contain parent or dot segments.")
			}
		}
		d.Path = strings.Trim(path.Clean("/"+d.Path), "/")
		if d.CredentialMode == "" {
			d.CredentialMode = "default"
		}
		if d.CredentialMode != "default" && d.CredentialMode != "keys" {
			return errors.New("Choose the AWS credential chain or an access key.")
		}
		if requireSecrets && d.CredentialMode == "keys" && (d.AccessKey == nil || *d.AccessKey == "" || d.SecretKey == nil || *d.SecretKey == "") {
			return errors.New("Set both the S3 access key ID and secret access key.")
		}
	default:
		return errors.New("Choose a supported backup destination.")
	}
	return nil
}
