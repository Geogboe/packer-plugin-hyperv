// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"
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

// PowerShellDirectHost returns the VM name for display purposes when using PowerShell Direct.
func PowerShellDirectHost() func(multistep.StateBag) (string, error) {
	return func(state multistep.StateBag) (string, error) {
		rawName, ok := state.GetOk("vmName")
		if !ok {
			return "", fmt.Errorf("vmName is not present in state")
		}

		name, ok := rawName.(string)
		if !ok || name == "" {
			return "", fmt.Errorf("vmName is not a valid string")
		}

		return name, nil
	}
}
