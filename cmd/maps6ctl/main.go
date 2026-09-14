package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"maps6/internal/ipc"
	"maps6/internal/tui"
)

func printData(data json.RawMessage) {
	if len(data) > 0 && string(data) != "null" {
		var obj interface{}
		if err := json.Unmarshal(data, &obj); err == nil {
			b, _ := json.MarshalIndent(obj, "", "  ")
			fmt.Printf("%s\n", string(b))
		} else {
			fmt.Printf("%s\n", string(data))
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	if cmd == "tui" {
		client, err := ipc.Connect()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot connect to maps6d: %v\n", err)
			os.Exit(1)
		}
		defer client.Close()
		tui.Run(client)
		return
	}

	client, err := ipc.Connect()
	if err != nil {
		fmt.Printf("Failed to connect to daemon: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	switch cmd {
	case "status":
		resp, err := client.Call(ipc.MethodGetSystemInfo, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if !resp.Success {
			fmt.Printf("Error: %s\n", resp.Error)
			return
		}
		fmt.Println("System Info:")
		printData(resp.Data)

	case "sensor":
		resp, err := client.Call(ipc.MethodGetSensorData, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if !resp.Success {
			fmt.Printf("Error: %s\n", resp.Error)
			return
		}
		fmt.Println("Sensor Data:")
		printData(resp.Data)

	case "module":
		if len(os.Args) < 3 {
			fmt.Println("Usage: maps6ctl module [list|enable <name>|disable <name>]")
			return
		}
		subcmd := os.Args[2]
		switch subcmd {
		case "list":
			resp, err := client.Call(ipc.MethodGetModuleStatus, nil)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			fmt.Println("Module Status:")
			printData(resp.Data)
		case "enable":
			if len(os.Args) < 4 {
				fmt.Println("Module name required")
				return
			}
			name := os.Args[3]
			resp, err := client.Call(ipc.MethodSetModuleEnabled, ipc.SetModuleEnabledParams{
				Name:    name,
				Enabled: true,
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			fmt.Printf("Module %s enabled\n", name)
		case "disable":
			if len(os.Args) < 4 {
				fmt.Println("Module name required")
				return
			}
			name := os.Args[3]
			resp, err := client.Call(ipc.MethodSetModuleEnabled, ipc.SetModuleEnabledParams{
				Name:    name,
				Enabled: false,
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			fmt.Printf("Module %s disabled\n", name)
		}

	case "co2-cal":
		fmt.Print("Are you sure? y/N: ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) == "y" {
			resp, err := client.Call(ipc.MethodTriggerCO2Cal, nil)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			fmt.Println("CO2 calibration triggered")
		}

	case "pms-reset":
		resp, err := client.Call(ipc.MethodTriggerPMSReset, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if !resp.Success {
			fmt.Printf("Error: %s\n", resp.Error)
			return
		}
		fmt.Println("PMS reset triggered")

	case "mqtt":
		resp, err := client.Call(ipc.MethodGetMQTTStatus, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if !resp.Success {
			fmt.Printf("Error: %s\n", resp.Error)
			return
		}
		var st ipc.MQTTStatus
		if err := json.Unmarshal(resp.Data, &st); err != nil {
			printData(resp.Data)
			return
		}
		printMQTTStatus(st)

	case "backfill":
		if len(os.Args) >= 3 && os.Args[2] == "trigger" {
			var dates []string
			if len(os.Args) >= 4 {
				dates = os.Args[3:]
			}
			params, _ := json.Marshal(ipc.TriggerBackfillParams{Dates: dates})
			resp, err := client.Call(ipc.MethodTriggerBackfill, params)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			fmt.Println("Historical backfill triggered successfully")
			return
		}

		resp, err := client.Call(ipc.MethodGetBackfillStatus, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if !resp.Success {
			fmt.Printf("Error: %s\n", resp.Error)
			return
		}
		var st ipc.BackfillStatus
		if err := json.Unmarshal(resp.Data, &st); err != nil {
			printData(resp.Data)
			return
		}
		printBackfillStatus(st)

	case "wifi":
		fmt.Println("wifi command not yet implemented")

	case "ota":
		if len(os.Args) < 3 {
			fmt.Println("Usage: maps6ctl ota [check|update]")
			return
		}
		subcmd := os.Args[2]
		if subcmd == "check" {
			resp, err := client.Call(ipc.MethodTriggerOTACheck, nil)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			if len(resp.Data) == 0 || string(resp.Data) == "null" {
				fmt.Println("No update available")
				return
			}
			fmt.Println("Update Info:")
			printData(resp.Data)
		} else if subcmd == "update" {
			resp, err := client.Call(ipc.MethodTriggerOTAUpdate, nil)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if !resp.Success {
				fmt.Printf("Error: %s\n", resp.Error)
				return
			}
			printData(resp.Data)
		}

	default:
		printUsage()
	}
}

func printBackfillStatus(st ipc.BackfillStatus) {
	fmt.Println("Historical Backfill Status:")
	runningStr := "Idle"
	if st.Running {
		runningStr = "Running"
		if st.CurrentDate != "" {
			runningStr = fmt.Sprintf("Running (processing: %s)", st.CurrentDate)
		}
	}
	fmt.Printf("  Status:             %s\n", runningStr)
	lastRun := st.LastRunTime
	if lastRun == "" {
		lastRun = "never"
	}
	fmt.Printf("  Last Run:           %s\n", lastRun)
	fmt.Printf("  Last Result:        %s\n", st.LastResult)
	fmt.Printf("  Records Uploaded:   %d records (%d chunk errors)\n", st.TotalUploaded, st.TotalErrors)
}

func printMQTTStatus(st ipc.MQTTStatus) {
	fmt.Println("MQTT Module Status:")
	fmt.Printf("  Module Enabled:     %v\n", st.Enabled)
	fmt.Printf("  Module Running:     %v\n", st.Running)

	connStr := "Disconnected"
	if st.Connected {
		connStr = "Connected"
	}
	fmt.Printf("  Connection State:   %s\n", connStr)
	scheme := "tcp"
	if st.UseTLS {
		scheme = "ssl"
	}
	fmt.Printf("  Broker Target:      %s://%s:%d\n", scheme, st.Broker, st.Port)
	fmt.Printf("  Client ID:          %s\n", st.ClientID)
	fmt.Printf("  Topic Prefix:       %s\n", st.TopicPrefix)
	fmt.Printf("  Sensor Telemetry:   %s/%s/sensor (interval: %ds)\n", st.TopicPrefix, st.ClientID, st.SensorIntervalSec)
	fmt.Printf("  Status Telemetry:   %s/%s/status (interval: %ds)\n", st.TopicPrefix, st.ClientID, st.StatusIntervalSec)

	lastSensor := st.LastSensorPublish
	if lastSensor == "" {
		lastSensor = "never"
	}
	fmt.Printf("  Sensor Publishes:   %d sent, %d failed (last: %s)\n", st.SensorPublishCount, st.SensorPublishErrors, lastSensor)

	lastStatus := st.LastStatusPublish
	if lastStatus == "" {
		lastStatus = "never"
	}
	fmt.Printf("  Status Publishes:   %d sent, %d failed (last: %s)\n", st.StatusPublishCount, st.StatusPublishErrors, lastStatus)

	if st.Backfill != nil {
		fmt.Printf("  Backfill Status:    %s (last run: %s, %d uploaded)\n", st.Backfill.LastResult, st.Backfill.LastRunTime, st.Backfill.TotalUploaded)
	}

	if st.LastError != "" {
		fmt.Printf("  Last Error:         %s\n", st.LastError)
	} else {
		fmt.Printf("  Last Error:         none\n")
	}
}

func printUsage() {
	fmt.Println(`Usage: maps6ctl <command> [options]
Commands:
  tui                           Start Terminal UI
  status                        Show system status
  sensor                        Show latest sensor data
  module                        Manage modules (list/enable/disable)
  mqtt                          Show MQTT connection and upload status
  backfill [status|trigger...]  Manage historical data backfill
  co2-cal                       Trigger CO2 baseline calibration
  pms-reset                     Trigger PMS sensor reset
  wifi                          Manage WiFi (scan/connect)
  ota                           OTA updates (check/update)`)
}
