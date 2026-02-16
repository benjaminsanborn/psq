package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDBConfig(t *testing.T) {
	// Create a temporary pg_service.conf for testing
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pg_service.conf")
	
	configContent := `# Test pg_service.conf
[test-service]
host=localhost
port=5433
dbname=testdb
user=testuser
password=testpass

[prod-service]
host=prod.example.com
dbname=proddb
user=produser
password=prodpass

[minimal-service]
host=minimal.example.com
dbname=minimaldb
user=minimaluser
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Temporarily override HOME to use test config
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	tests := []struct {
		name        string
		serviceName string
		wantConfig  *DBConfig
		wantErr     bool
	}{
		{
			name:        "full config",
			serviceName: "test-service",
			wantConfig: &DBConfig{
				Host:     "localhost",
				Port:     "5433",
				Database: "testdb",
				User:     "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
		{
			name:        "minimal config with defaults",
			serviceName: "minimal-service",
			wantConfig: &DBConfig{
				Host:     "minimal.example.com",
				Port:     "5432", // Should default to 5432
				Database: "minimaldb",
				User:     "minimaluser",
				Password: "",
			},
			wantErr: false,
		},
		{
			name:        "non-existent service",
			serviceName: "does-not-exist",
			wantConfig:  nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getDBConfig(tt.serviceName)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("getDBConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if tt.wantErr {
				return
			}

			if got.Host != tt.wantConfig.Host {
				t.Errorf("Host = %v, want %v", got.Host, tt.wantConfig.Host)
			}
			if got.Port != tt.wantConfig.Port {
				t.Errorf("Port = %v, want %v", got.Port, tt.wantConfig.Port)
			}
			if got.Database != tt.wantConfig.Database {
				t.Errorf("Database = %v, want %v", got.Database, tt.wantConfig.Database)
			}
			if got.User != tt.wantConfig.User {
				t.Errorf("User = %v, want %v", got.User, tt.wantConfig.User)
			}
			if got.Password != tt.wantConfig.Password {
				t.Errorf("Password = %v, want %v", got.Password, tt.wantConfig.Password)
			}
		})
	}
}

func TestListServices(t *testing.T) {
	// Create a temporary pg_service.conf for testing
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pg_service.conf")
	
	configContent := `# Test pg_service.conf
[service1]
host=host1.example.com

[service2]
host=host2.example.com

[service3]
host=host3.example.com
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Temporarily override HOME to use test config
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	services, err := listServices()
	if err != nil {
		t.Fatalf("listServices() error = %v", err)
	}

	want := []string{"service1", "service2", "service3"}
	if len(services) != len(want) {
		t.Errorf("listServices() returned %d services, want %d", len(services), len(want))
	}

	for i, service := range services {
		if service != want[i] {
			t.Errorf("Service[%d] = %v, want %v", i, service, want[i])
		}
	}
}
