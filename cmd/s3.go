package cmd

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const (
	awsMaxRetries = 10
)

// AWSConfig contains all information needed to access AWS services
type AWSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Concurrency     uint
}

// ImageDescription contains all info needed to find a specific image within the object store
type ImageDescription struct {
	GitHubRepo string
	CommitSHA  string
}

// ObjectStorageManger describes an object capable of pushing/pulling images from
// an object store
type ObjectStorageManger interface {
	Push(ImageDescription, io.Reader, interface{}) error
	Pull(ImageDescription, io.WriterAt, interface{}) error
}

// S3Options contains the information needed to push/pull an image from S3
type S3Options struct {
	Region    string
	Bucket    string
	KeyPrefix string
}

// S3StorageManager is an object capable of pushing/pulling from S3
type S3StorageManager struct {
	config        *AWSConfig
	creds         *credentials.Credentials
	mc            MetricsCollector
	awsLoggerFunc aws.Logger
	logger        *log.Logger
}

// NewS3StorageManager returns a new S3 manager
func NewS3StorageManager(config AWSConfig, mc MetricsCollector, logger *log.Logger) *S3StorageManager {
	smm := &S3StorageManager{
		creds:  credentials.NewStaticCredentials(config.AccessKeyID, config.SecretAccessKey, ""),
		config: &config,
		mc:     mc,
		logger: logger,
	}
	smm.awsLoggerFunc = aws.LoggerFunc(func(args ...interface{}) { smm.logf("s3: %v", args...) })
	return smm
}

func (sm *S3StorageManager) logf(msg string, params ...interface{}) {
	sm.logger.Printf(msg+"\n", params...)
}

func (sm S3StorageManager) getPath(kp string, desc ImageDescription) string {
	return fmt.Sprintf("%v%v/%v.tar.gz", kp, desc.GitHubRepo, desc.CommitSHA)
}

func (sm S3StorageManager) getOpts(opts interface{}) (*S3Options, error) {
	switch v := opts.(type) {
	case *S3Options:
		return v, nil
	default:
		return nil, fmt.Errorf("opts must be of type *S3Options (received %T)", v)
	}
}

func (sm S3StorageManager) getSession(region string) *session.Session {
	return session.New(
		aws.NewConfig().WithRegion(region).WithMaxRetries(awsMaxRetries).WithCredentials(sm.creds).WithLogger(sm.awsLoggerFunc))
}

// Push reads an image from in and pushes it to S3
func (sm *S3StorageManager) Push(desc ImageDescription, in io.Reader, opts interface{}) error {
	started := time.Now().UTC()
	s3opts, err := sm.getOpts(opts)
	if err != nil {
		return err
	}
	sess := sm.getSession(s3opts.Region)
	u := s3manager.NewUploaderWithClient(s3.New(sess), func(u *s3manager.Uploader) {
		u.Concurrency = int(sm.config.Concurrency)
	})
	ct := "application/gzip"
	k := sm.getPath(s3opts.KeyPrefix, desc)
	ui := &s3manager.UploadInput{
		ContentType: &ct,
		Bucket:      &s3opts.Bucket,
		Body:        in,
		Key:         &k,
	}
	uo, err := u.Upload(ui)
	if err != nil {
		return err
	}
	d := time.Now().UTC().Sub(started).Seconds()
	sm.mc.Duration("s3.push.duration", desc.GitHubRepo, desc.CommitSHA, nil, d)
	sm.logf("S3 push location: %v", uo.Location)
	sm.logf("S3 version ID: %v", uo.VersionID)
	sm.logf("S3 upload ID: %v", uo.UploadID)
	return nil
}

// Pull downloads an image from S3 and writes it to out
func (sm *S3StorageManager) Pull(desc ImageDescription, out io.WriterAt, opts interface{}) error {
	started := time.Now().UTC()
	s3opts, err := sm.getOpts(opts)
	if err != nil {
		return err
	}
	sess := sm.getSession(s3opts.Region)
	d := s3manager.NewDownloaderWithClient(s3.New(sess), func(d *s3manager.Downloader) {
		d.Concurrency = int(sm.config.Concurrency)
	})
	k := sm.getPath(s3opts.KeyPrefix, desc)
	di := &s3.GetObjectInput{
		Bucket: &s3opts.Bucket,
		Key:    &k,
	}
	n, err := d.Download(out, di)
	if err != nil {
		return err
	}
	duration := time.Now().UTC().Sub(started).Seconds()
	sm.mc.Duration("s3.pull.duration", desc.GitHubRepo, desc.CommitSHA, nil, duration)
	sm.logf("S3 bytes read: %v", n)
	return nil
}
