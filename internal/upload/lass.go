package upload

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/module"
	"maps6/internal/network"
)

type LASSModule struct {
	cfg      *config.Config
	bus      *bus.SensorBus
	deviceID string
	netMgr   *network.Manager

	mu         sync.RWMutex
	lastStatus string
	lastCode   int
	lastTime   string
	cancel     context.CancelFunc

	logger *slog.Logger
}

func NewLASSModule(cfg *config.Config, bus *bus.SensorBus, deviceID string, netMgr *network.Manager) *LASSModule {
	return &LASSModule{
		cfg:      cfg,
		bus:      bus,
		deviceID: deviceID,
		netMgr:   netMgr,
		logger:   slog.Default().With("module", "lass"),
	}
}

func (m *LASSModule) Name() string {
	return "lass"
}

func (m *LASSModule) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	interval := time.Duration(m.cfg.Upload.LASS.Interval)
	if interval <= 0 {
		interval = 300 * time.Second
	}

	go func() {
		subName := "lass_upload"
		ch := m.bus.Subscribe(subName, 1)
		defer m.bus.Unsubscribe(subName)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.upload()
			case <-ch: // Just drain to keep channel empty
			}
		}
	}()

	return nil
}

func (m *LASSModule) upload() {
	if !m.netMgr.IsConnected() {
		m.logger.Warn("network not connected, skipping LASS upload")
		m.updateStatus("network down", 0)
		return
	}

	data := m.bus.Latest()
	now := time.Now().UTC()
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")

	// Build payload
	payload := fmt.Sprintf("|s_g8=%d|s_t0=%.2f|app=MAPS6|date=%s|s_d0=%d|s_h0=%.2f|device_id=%s|s_gg=%d|ver_app=8.0.0|time=%s",
		data.CO2, data.Temp, dateStr, data.PM25_AE, data.Humi, m.deviceID, data.TVOC, timeStr)

	lassURL := m.cfg.Upload.LASS.URL
	if lassURL == "" {
		lassURL = "https://data.lass-net.org/Upload/MAPS-secure.php"
	}

	reqURL, err := url.Parse(lassURL)
	if err != nil {
		m.logger.Error("invalid LASS URL", "error", err)
		return
	}

	q := reqURL.Query()
	q.Set("topic", "MAPS6")
	q.Set("device_id", m.deviceID)
	q.Set("key", "NoKey")
	q.Set("msg", payload)
	reqURL.RawQuery = q.Encode()

	client := http.Client{Timeout: 10 * time.Second}
	
	err = m.doUpload(client, reqURL.String())
	if err != nil {
		m.logger.Error("LASS upload failed, will retry", "error", err)
		m.updateStatus(fmt.Sprintf("error: %v", err), 0)
		
		// Retry
		retryInterval := time.Duration(m.cfg.Upload.LASS.RetryInterval)
		if retryInterval <= 0 {
			retryInterval = 10 * time.Second
		}
		
		time.Sleep(retryInterval) // Sleep for retry interval
		
		err = m.doUpload(client, reqURL.String())
		if err != nil {
			m.logger.Error("LASS upload retry failed", "error", err)
			m.updateStatus(fmt.Sprintf("retry error: %v", err), 0)
		}
	}
}

func (m *LASSModule) doUpload(client http.Client, reqURL string) error {
	resp, err := client.Get(reqURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// Read body to reuse connection
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		m.updateStatus("http error", resp.StatusCode)
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	m.updateStatus("success", resp.StatusCode)
	m.logger.Info("LASS upload successful", "status", resp.StatusCode)
	return nil
}

func (m *LASSModule) updateStatus(status string, code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastStatus = status
	m.lastCode = code
	m.lastTime = time.Now().Format(time.RFC3339)
}

func (m *LASSModule) Stop() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
	return nil
}

func (m *LASSModule) Status() module.ModuleStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errStr string
	if m.lastStatus != "" && m.lastStatus != "success" {
		errStr = m.lastStatus
	}

	return module.ModuleStatus{
		Name:      m.Name(),
		Enabled:   true,
		Running:   m.cancel != nil,
		LastError: errStr,
	}
}

func (m *LASSModule) GetLastUploadInfo() (string, int, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastStatus, m.lastCode, m.lastTime
}
