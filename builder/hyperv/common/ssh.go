// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

func CommHost(host string) func(multistep.StateBag) (string, error) {
	return func(state multistep.StateBag) (string, error) {

		// Skip IP auto detection if the configuration has an ssh host configured.
		if host != "" {
			log.Printf("Using host value: %s", host)
			return host, nil
		}

		vmName := state.Get("vmName").(string)
		driver := state.Get("driver").(Driver)

		mac, err := driver.Mac(vmName)
		if err != nil {
			return "", err
		}

		ip, err := driver.IpAddress(mac)
		if err != nil {
			return "", err
		}

		return ip, nil
	}
}

// PSRPHost returns the connection information for PSRP communicator.
// For HvSocket transport, it returns the VM GUID; for WSMan, it returns the IP address.
func PSRPHost(config interface{}) func(multistep.StateBag) (string, error) {
	return func(state multistep.StateBag) (string, error) {
		// Get config to check transport type
		cfg, ok := config.(*CommConfig)
		if !ok {
			log.Printf("Warning: PSRPHost config type assertion failed, falling back to IP")
			return CommHost("")(state)
		}

		// For HvSocket transport, return VM GUID
		if cfg.PSRP.PSRPTransport == "hvsock" {
			// If VMID is explicitly set in config, use it
			if cfg.PSRP.PSRPVMID != "" {
				log.Printf("Using configured VMID: %s", cfg.PSRP.PSRPVMID)
				return cfg.PSRP.PSRPVMID, nil
			}

			// Auto-detect VMID from state bag
			vmName := state.Get("vmName").(string)
			driver := state.Get("driver").(Driver)

			log.Printf("Auto-detecting VM GUID for VM: %s", vmName)
			vmId, err := driver.GetVMId(vmName)
			if err != nil {
				return "", err
			}

			log.Printf("Detected VM GUID: %s", vmId)
			return vmId, nil
		}

		// For WSMan transport, return IP address (same as CommHost)
		return CommHost(cfg.PSRP.PSRPHost)(state)
	}
}
