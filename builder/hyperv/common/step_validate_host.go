// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"fmt"

	"github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common/powershell"
	"github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common/powershell/hyperv"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// StepValidateHost performs runtime host validation that requires PowerShell.
// These checks were previously in CommonConfig.Prepare() but are side effects
// that belong in the build execution phase, not configuration parsing.
type StepValidateHost struct {
	EnableVirtualizationExtensions bool
	RamSize                        uint

	// Injectable for testing. Nil means use real PowerShell functions.
	HasVirtExtFunc    func() (bool, error)
	GetHostMemoryFunc func() float64
}

func (s *StepValidateHost) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	// Validate virtualization extensions if enabled.
	if s.EnableVirtualizationExtensions {
		hasVirtExt := powershell.HasVirtualMachineVirtualizationExtensions
		if s.HasVirtExtFunc != nil {
			hasVirtExt = s.HasVirtExtFunc
		}

		hasExt, err := hasVirtExt()
		if err != nil {
			err := fmt.Errorf("failed detecting virtualization extensions support: %w", err)
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
		if !hasExt {
			err := fmt.Errorf("this version of Hyper-V does not support " +
				"virtual machine virtualization extensions; use Windows 10 or Windows Server 2016 or newer")
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}

	// Check host memory (warning only).
	if warning := s.checkHostAvailableMemory(); warning != "" {
		ui.Say(fmt.Sprintf("Warning: %s", warning))
	}

	return multistep.ActionContinue
}

func (s *StepValidateHost) Cleanup(state multistep.StateBag) {}

func (s *StepValidateHost) checkHostAvailableMemory() string {
	getMemory := powershell.GetHostAvailableMemory
	if s.GetHostMemoryFunc != nil {
		getMemory = s.GetHostMemoryFunc
	}

	freeMB := getMemory()
	if (freeMB - float64(s.RamSize)) < LowRam {
		return "Hyper-V might fail to create a VM if there is not enough free memory in the system."
	}

	return ""
}

// detectSwitchName auto-detects a Hyper-V virtual switch via PowerShell.
// Called from CommonConfig.Prepare() when no switch_name is configured.
func detectSwitchName(buildName string) string {
	powershellAvailable, _, _ := powershell.IsPowershellAvailable()

	if powershellAvailable {
		onlineSwitchName, err := hyperv.GetExternalOnlineVirtualSwitch()
		if onlineSwitchName != "" && err == nil {
			return onlineSwitchName
		}
	}

	return fmt.Sprintf("packer-%s", buildName)
}
