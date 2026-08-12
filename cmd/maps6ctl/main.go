package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"maps6/internal/ipc"
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
		fmt.Println("TUI not yet implemented")
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

func printUsage() {
	fmt.Println(`Usage: maps6ctl <command> [options]
Commands:
  tui                 Start Terminal UI
  status              Show system status
  sensor              Show latest sensor data
  module              Manage modules (list/enable/disable)
  co2-cal             Trigger CO2 baseline calibration
  pms-reset           Trigger PMS sensor reset
  wifi                Manage WiFi (scan/connect)
  ota                 OTA updates (check/update)`)
}
