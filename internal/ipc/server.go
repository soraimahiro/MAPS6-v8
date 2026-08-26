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

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return // Connection closed by client
		}

		resp := s.dispatch(req)
		if err := encoder.Encode(resp); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(req Request) Response {
	switch req.Method {
	case MethodGetSensorData:
		latest := s.bus.Latest()
		return NewSuccessResponse(latest)

	case MethodGetModuleStatus:
		return NewSuccessResponse(s.registry.StatusAll())

	case MethodSetModuleEnabled:
		var params SetModuleEnabledParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse("invalid params format")
		}
		if params.Enabled {
			if err := s.registry.Enable(params.Name); err != nil {
				return NewErrorResponse(err.Error())
			}
		} else {
			if err := s.registry.Disable(params.Name); err != nil {
				return NewErrorResponse(err.Error())
			}
		}
		return NewSuccessResponse(nil)

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
		return NewSuccessResponse(info)

	case MethodTriggerCO2Cal:
		if err := s.mega.SetCO2Calibration(); err != nil {
			return NewErrorResponse(err.Error())
		}
		return NewSuccessResponse(nil)

	case MethodTriggerPMSReset:
		if err := s.mega.SetPMSReset(); err != nil {
			return NewErrorResponse(err.Error())
		}
		return NewSuccessResponse(nil)

	case MethodTriggerOTACheck:
		info, err := s.otaUpdater.CheckUpdate(s.deviceID)
		if err != nil {
			return NewErrorResponse(err.Error())
		}
		return NewSuccessResponse(info)

	case MethodTriggerOTAUpdate:
		info, err := s.otaUpdater.CheckUpdate(s.deviceID)
		if err != nil {
			return NewErrorResponse(err.Error())
		}
		if info != nil && info.Available {
			go func() {
				_ = s.otaUpdater.ApplyUpdate(info)
			}()
			return NewSuccessResponse("Update started")
		}
		return NewSuccessResponse("No update available")

	default:
		return NewErrorResponse("unknown method")
	}
}
