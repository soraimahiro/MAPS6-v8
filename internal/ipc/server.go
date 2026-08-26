package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"maps6/internal/bus"
	"maps6/internal/config"
	"maps6/internal/mcu"
	"maps6/internal/module"
	"maps6/internal/ota"
)

type Server struct {
	bus            *bus.SensorBus
	registry       *module.Registry
	mega           *mcu.Mega2560
	networkStateFn func() (string, string, string)
	otaUpdater     *ota.Updater
	deviceID       string
	version        string
	startTime      time.Time
	cfg            *config.Config
	logger         *slog.Logger
	listener       net.Listener
}

func NewServer(
	b *bus.SensorBus,
	r *module.Registry,
	m *mcu.Mega2560,
	netFn func() (string, string, string),
	updater *ota.Updater,
	devID string,
	ver string,
	cfg *config.Config,
) *Server {
	return &Server{
		bus:            b,
		registry:       r,
		mega:           m,
		networkStateFn: netFn,
		otaUpdater:     updater,
		deviceID:       devID,
		version:        ver,
		startTime:      time.Now(),
		cfg:            cfg,
		logger:         slog.With("pkg", "ipc"),
	}
}

func (s *Server) Start(ctx context.Context) error {
	os.Remove(SocketPath)

	l, err := net.Listen("unix", SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", SocketPath, err)
	}
	s.listener = l
	_ = os.Chmod(SocketPath, 0666) // Allow non-root users (like 'pi') to connect without sudo
	s.logger.Info("IPC server listening", "socket", SocketPath)

	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					s.logger.Error("IPC accept error", "err", err)
				}
				continue
			}
			go s.handleConnection(conn)
		}
	}()

	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	return nil
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(SocketPath)
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		s.logger.Error("Failed to decode request", "err", err)
		return
	}

	var resp Response

	switch req.Method {
	case MethodGetSensorData:
		latest := s.bus.Latest()
		resp = NewSuccessResponse(latest)
	case MethodGetModuleStatus:
		resp = NewSuccessResponse(s.registry.StatusAll())
	case MethodSetModuleEnabled:
		var params SetModuleEnabledParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp = NewErrorResponse("invalid params format")
			break
		}
		if params.Enabled {
			err := s.registry.Enable(params.Name)
			if err != nil {
				resp = NewErrorResponse(err.Error())
			} else {
				resp = NewSuccessResponse(nil)
			}
		} else {
			err := s.registry.Disable(params.Name)
			if err != nil {
				resp = NewErrorResponse(err.Error())
			} else {
				resp = NewSuccessResponse(nil)
			}
		}
	case MethodGetSystemInfo:
		mcuFirmware, _ := s.mega.GetFirmwareVersion()
		netType, ip, ssid := "", "", ""
		if s.networkStateFn != nil {
			netType, ip, ssid = s.networkStateFn()
		}
		modulesMap := make(map[string]bool)
		for _, status := range s.registry.StatusAll() {
			modulesMap[status.Name] = status.Enabled
		}
		info := SystemInfo{
			DeviceID:     s.deviceID,
			Version:      s.version,
			UptimeSec:    int64(time.Since(s.startTime).Seconds()),
			MCUFirmware:  mcuFirmware,
			NetworkState: netType,
			IP:           ip,
			SSID:         ssid,
			Modules:      modulesMap,
		}
		resp = NewSuccessResponse(info)
	case MethodTriggerCO2Cal:
		err := s.mega.SetCO2Calibration()
		if err != nil {
			resp = NewErrorResponse(err.Error())
		} else {
			resp = NewSuccessResponse(nil)
		}
	case MethodTriggerPMSReset:
		err := s.mega.SetPMSReset()
		if err != nil {
			resp = NewErrorResponse(err.Error())
		} else {
			resp = NewSuccessResponse(nil)
		}
	case MethodTriggerOTACheck:
		info, err := s.otaUpdater.CheckUpdate(s.deviceID)
		if err != nil {
			resp = NewErrorResponse(err.Error())
		} else {
			resp = NewSuccessResponse(info)
		}
	case MethodTriggerOTAUpdate:
		info, err := s.otaUpdater.CheckUpdate(s.deviceID)
		if err != nil {
			resp = NewErrorResponse(err.Error())
			break
		}
		if info != nil && info.Available {
			go func() {
				_ = s.otaUpdater.ApplyUpdate(info)
			}()
			resp = NewSuccessResponse("Update started")
		} else {
			resp = NewSuccessResponse("No update available")
		}
	default:
		resp = NewErrorResponse("unknown method")
	}

	json.NewEncoder(conn).Encode(resp)
}
