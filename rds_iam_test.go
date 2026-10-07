package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

func TestGetDBConfigRDSIAMDirectives(t *testing.T) {
	tmpDir := t.TempDir()
	content := `[iam]
host=mydb.cluster-abc123.us-east-1.rds.amazonaws.com
user=iam_user
dbname=app
#psq:auth=rds-iam
#psq:aws_profile=prod
#psq:aws_region=us-west-2

[plain]
host=localhost
#psq:auth=rds-iam
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".pg_service.conf"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpDir)

	cfg, err := getDBConfig("iam")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UsesRDSIAM() || cfg.AWSProfile != "prod" || cfg.AWSRegion != "us-west-2" {
		t.Errorf("unexpected IAM settings: %+v", cfg)
	}

	// Directives must not leak from one service block into another.
	other, err := getDBConfig("plain")
	if err != nil {
		t.Fatal(err)
	}
	if other.AWSProfile != "" || other.AWSRegion != "" {
		t.Errorf("directives leaked across services: %+v", other)
	}
}

func TestRegionFromHost(t *testing.T) {
	tests := map[string]string{
		"mydb.cluster-abc123.us-east-1.rds.amazonaws.com":    "us-east-1",
		"mydb.cluster-ro-abc123.eu-west-2.rds.amazonaws.com": "eu-west-2",
		"db.abc.us-gov-west-1.rds.amazonaws.com":             "us-gov-west-1",
		"db.abc.cn-north-1.rds.amazonaws.com.cn":             "cn-north-1",
		"places-db.example.com":                              "",
		"localhost":                                          "",
	}
	for host, want := range tests {
		if got := regionFromHost(host); got != want {
			t.Errorf("regionFromHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestBuildDSNQuotesValues(t *testing.T) {
	cfg := &DBConfig{Host: "h", Port: "5432", Database: "d", User: "u"}
	dsn := buildDSN(cfg, `p a's\s`)
	if !strings.Contains(dsn, `password='p a\'s\\s'`) {
		t.Errorf("password not quoted correctly: %s", dsn)
	}
}

func TestRDSIAMToken(t *testing.T) {
	cfg := &DBConfig{Host: "mydb.abc.us-east-1.rds.amazonaws.com", Port: "5432", User: "iam_user"}
	awsCfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", ""),
	}
	token, err := rdsIAMToken(context.Background(), cfg, awsCfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, cfg.Host+":5432?Action=connect") || !strings.Contains(token, "DBUser=iam_user") {
		t.Errorf("unexpected token: %s", token)
	}
}
