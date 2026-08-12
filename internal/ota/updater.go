package ota

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"maps6/internal/config"
	"maps6/internal/module"
)

type Updater struct {
	cfg            *config.OTAConfig
	currentVersion string
	binaryPath     string
	logger         *slog.Logger
	cancel         context.CancelFunc
}

type UpdateInfo struct {
	Available      bool   `json:"available"`
	Version        string `json:"version"`
	DownloadURL    string `json:"download_url"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	ReleaseNotes   string `json:"release_notes"`
	SizeBytes      int64  `json:"size_bytes"`
	Mandatory      bool   `json:"mandatory"`
}

func NewUpdater(cfg *config.OTAConfig, currentVersion string) *Updater {
	return &Updater{
		cfg:            cfg,
		currentVersion: currentVersion,
		binaryPath:     "/home/pi/maps6/maps6d",
		logger:         slog.With("pkg", "ota"),
	}
}

func (u *Updater) Name() string {
	return "ota"
}

func (u *Updater) Start(ctx context.Context) error {
	if u.cfg.ServerURL == "" {
		u.logger.Info("OTA ServerURL is empty, updates disabled")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	u.cancel = cancel

	go func() {
		interval := 24 * time.Hour
		if u.cfg.CheckInterval > 0 {
			interval = time.Duration(u.cfg.CheckInterval) * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if u.cfg.AutoUpdate {
					info, err := u.CheckUpdate("auto-check")
					if err != nil {
						u.logger.Error("Failed to check for updates", "err", err)
						continue
					}
					if info != nil && info.Available {
						u.logger.Info("Update available, auto-updating", "version", info.Version)
						if err := u.ApplyUpdate(info); err != nil {
							u.logger.Error("Failed to apply update", "err", err)
						}
					}
				}
			}
		}
	}()

	return nil
}

func (u *Updater) Stop() error {
	if u.cancel != nil {
		u.cancel()
	}
	return nil
}

func (u *Updater) Status() module.ModuleStatus {
	// Need to import "maps6/internal/module" in updater.go
	return module.ModuleStatus{Name: "ota", Enabled: u.cancel != nil}
}

func (u *Updater) CheckUpdate(deviceID string) (*UpdateInfo, error) {
	if u.cfg.ServerURL == "" {
		return nil, fmt.Errorf("OTA server URL not configured")
	}

	url := fmt.Sprintf("%s/api/ota/check?device_id=%s&current_version=%s&app=MAPS6", u.cfg.ServerURL, deviceID, u.currentVersion)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var info UpdateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	if !info.Available {
		return nil, nil
	}

	return &info, nil
}

func (u *Updater) ApplyUpdate(info *UpdateInfo) error {
	u.logger.Info("Applying update", "version", info.Version)

	resp, err := http.Get(info.DownloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	tmpPath := "/tmp/maps6d.new"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		out.Close()
		return fmt.Errorf("failed to write download: %w", err)
	}
	out.Close()

	hash := fmt.Sprintf("%x", hasher.Sum(nil))
	if hash != info.ChecksumSHA256 {
		os.Remove(tmpPath)
		return fmt.Errorf("checksum mismatch: expected %s, got %s", info.ChecksumSHA256, hash)
	}

	bakPath := u.binaryPath + ".bak"
	if err := os.Rename(u.binaryPath, bakPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	if err := os.Rename(tmpPath, u.binaryPath); err != nil {
		os.Rename(bakPath, u.binaryPath) // Try to restore
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	u.logger.Info("Update applied successfully, restarting service")
	cmd := exec.Command("systemctl", "restart", "maps6d")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	return nil
}
