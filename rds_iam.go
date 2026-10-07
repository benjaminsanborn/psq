package main

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/lib/pq"
)

// rdsHostRegion matches RDS/Aurora endpoints like
// mydb.cluster-abc123.us-east-1.rds.amazonaws.com and captures the region.
var rdsHostRegion = regexp.MustCompile(`\.([a-z]{2}(?:-gov)?-[a-z]+-\d)\.rds\.amazonaws\.com(?:\.cn)?$`)

// regionFromHost infers the AWS region from an RDS endpoint hostname.
func regionFromHost(host string) string {
	if m := rdsHostRegion.FindStringSubmatch(host); m != nil {
		return m[1]
	}
	return ""
}

// loadAWSConfig resolves AWS credentials using the standard chain
// (env vars, shared config/SSO, instance role), honoring the service's
// aws_profile and aws_region settings.
func loadAWSConfig(ctx context.Context, cfg *DBConfig) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if cfg.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.AWSProfile))
	}
	region := cfg.AWSRegion
	if region == "" {
		region = regionFromHost(cfg.Host)
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}
	if awsCfg.Region == "" {
		return aws.Config{}, fmt.Errorf("could not determine AWS region for %s; set psq:aws_region in ~/.pg_service.conf", cfg.Host)
	}
	return awsCfg, nil
}

// rdsIAMToken generates a short-lived (15 minute) RDS IAM auth token.
func rdsIAMToken(ctx context.Context, cfg *DBConfig, awsCfg aws.Config) (string, error) {
	endpoint := net.JoinHostPort(cfg.Host, cfg.Port)
	token, err := auth.BuildAuthToken(ctx, endpoint, awsCfg.Region, cfg.User, awsCfg.Credentials)
	if err != nil {
		return "", fmt.Errorf("failed to build RDS IAM auth token: %w", err)
	}
	return token, nil
}

// rdsIAMConnector mints a fresh IAM token for every new physical connection,
// so the sql.DB pool keeps working after the 15 minute token lifetime.
type rdsIAMConnector struct {
	cfg    *DBConfig
	awsCfg aws.Config
}

func newRDSIAMConnector(cfg *DBConfig) (*rdsIAMConnector, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &rdsIAMConnector{cfg: cfg, awsCfg: awsCfg}, nil
}

func (c *rdsIAMConnector) Connect(ctx context.Context) (driver.Conn, error) {
	token, err := rdsIAMToken(ctx, c.cfg, c.awsCfg)
	if err != nil {
		return nil, err
	}
	connector, err := pq.NewConnector(buildDSN(c.cfg, token))
	if err != nil {
		return nil, err
	}
	return connector.Connect(ctx)
}

func (c *rdsIAMConnector) Driver() driver.Driver {
	return &pq.Driver{}
}

// rdsIAMTokenForConfig mints a one-off token, e.g. for handing to psql.
func rdsIAMTokenForConfig(cfg *DBConfig) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return "", err
	}
	return rdsIAMToken(ctx, cfg, awsCfg)
}
