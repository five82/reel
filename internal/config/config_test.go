package config

import (
	"testing"
)

func TestNewConfig(t *testing.T) {
	cfg := NewConfig("/input", "/output", "/log")

	if cfg.InputDir != "/input" {
		t.Errorf("expected InputDir=/input, got %s", cfg.InputDir)
	}
	if cfg.OutputDir != "/output" {
		t.Errorf("expected OutputDir=/output, got %s", cfg.OutputDir)
	}
	if cfg.LogDir != "/log" {
		t.Errorf("expected LogDir=/log, got %s", cfg.LogDir)
	}

	// Check defaults
	if cfg.SVTAV1Preset != DefaultSVTAV1Preset {
		t.Errorf("expected SVTAV1Preset=%d, got %d", DefaultSVTAV1Preset, cfg.SVTAV1Preset)
	}
	if cfg.CRFSD != DefaultCRFSD {
		t.Errorf("expected CRFSD=%d, got %d", DefaultCRFSD, cfg.CRFSD)
	}
	if cfg.CRFHD != DefaultCRFHD {
		t.Errorf("expected CRFHD=%d, got %d", DefaultCRFHD, cfg.CRFHD)
	}
	if cfg.CRFUHD != DefaultCRFUHD {
		t.Errorf("expected CRFUHD=%d, got %d", DefaultCRFUHD, cfg.CRFUHD)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "default config is valid",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "preset 14 is invalid",
			modify:  func(c *Config) { c.SVTAV1Preset = 14 },
			wantErr: true,
		},
		{
			name:    "preset 13 is valid",
			modify:  func(c *Config) { c.SVTAV1Preset = 13 },
			wantErr: false,
		},
		{
			name:    "crf-sd 64 is invalid",
			modify:  func(c *Config) { c.CRFSD = 64 },
			wantErr: true,
		},
		{
			name:    "crf-hd 64 is invalid",
			modify:  func(c *Config) { c.CRFHD = 64 },
			wantErr: true,
		},
		{
			name:    "crf-uhd 64 is invalid",
			modify:  func(c *Config) { c.CRFUHD = 64 },
			wantErr: true,
		},
		{
			name:    "chunk_duration_sd 0 is invalid",
			modify:  func(c *Config) { c.ChunkDurationSD = 0 },
			wantErr: true,
		},
		{
			name:    "chunk_duration_hd 121 is invalid",
			modify:  func(c *Config) { c.ChunkDurationHD = 121 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig("/input", "/output", "/log")
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCRFForWidth(t *testing.T) {
	cfg := NewConfig("/input", "/output", "/log")
	cfg.CRFSD = 25
	cfg.CRFHD = 27
	cfg.CRFUHD = 29

	tests := []struct {
		width    uint32
		expected uint8
	}{
		{width: 720, expected: 25},  // SD
		{width: 1280, expected: 25}, // SD (720p)
		{width: 1919, expected: 25}, // Just below HD threshold
		{width: 1920, expected: 27}, // HD threshold
		{width: 2560, expected: 27}, // 1440p
		{width: 3839, expected: 27}, // Just below UHD threshold
		{width: 3840, expected: 29}, // UHD threshold
		{width: 7680, expected: 29}, // 8K
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := cfg.CRFForWidth(tt.width)
			if got != tt.expected {
				t.Errorf("CRFForWidth(%d) = %d, want %d", tt.width, got, tt.expected)
			}
		})
	}
}

func TestChunkDurationForWidth(t *testing.T) {
	cfg := NewConfig("/input", "/output", "/log")
	cfg.ChunkDurationSD = 20.0
	cfg.ChunkDurationHD = 30.0
	cfg.ChunkDurationUHD = 45.0

	tests := []struct {
		width    uint32
		expected float64
	}{
		{width: 720, expected: 20.0},  // SD
		{width: 1280, expected: 20.0}, // SD (720p)
		{width: 1919, expected: 20.0}, // Just below HD threshold
		{width: 1920, expected: 30.0}, // HD threshold
		{width: 2560, expected: 30.0}, // 1440p
		{width: 3839, expected: 30.0}, // Just below UHD threshold
		{width: 3840, expected: 45.0}, // UHD threshold
		{width: 7680, expected: 45.0}, // 8K
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := cfg.ChunkDurationForWidth(tt.width)
			if got != tt.expected {
				t.Errorf("ChunkDurationForWidth(%d) = %f, want %f", tt.width, got, tt.expected)
			}
		})
	}
}
