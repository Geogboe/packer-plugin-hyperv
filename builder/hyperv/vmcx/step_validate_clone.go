// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vmcx

import (
	"context"
	"fmt"

	powershell "github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common/powershell"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// StepValidateClone checks if the VM to clone exists and is in a valid state.
// This logic was moved from Builder.Prepare to avoid side effects during validation.
type StepValidateClone struct{}

func (s *StepValidateClone) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packersdk.Ui)

	if config.CloneFromVMName == "" {
		// Nothing to validate here if we are not cloning from a named VM (e.g. using VMCX path)
		return multistep.ActionContinue
	}

	ui.Say("Validating clone source VM...")

	virtualMachineExists, err := powershell.DoesVirtualMachineExist(config.CloneFromVMName)
	if err != nil {
		state.Put("error", fmt.Errorf("Failed detecting if virtual machine to clone from exists: %s", err))
		return multistep.ActionHalt
	}

	if !virtualMachineExists {
		state.Put("error", fmt.Errorf("Virtual machine '%s' to clone from does not exist.", config.CloneFromVMName))
		return multistep.ActionHalt
	}

	// Side effect: Update Generation in config
	config.Generation, err = powershell.GetVirtualMachineGeneration(config.CloneFromVMName)
	if err != nil {
		state.Put("error", fmt.Errorf("Failed detecting virtual machine to clone from generation: %s", err))
		return multistep.ActionHalt
	}

	if config.CloneFromSnapshotName != "" {
		virtualMachineSnapshotExists, err := powershell.DoesVirtualMachineSnapshotExist(
			config.CloneFromVMName, config.CloneFromSnapshotName)
		if err != nil {
			state.Put("error", fmt.Errorf("Failed detecting if virtual machine snapshot to clone from exists: %s", err))
			return multistep.ActionHalt
		}

		if !virtualMachineSnapshotExists {
			state.Put("error", fmt.Errorf("Virtual machine snapshot '%s' on virtual machine '%s' to clone from does not exist.",
				config.CloneFromSnapshotName, config.CloneFromVMName))
			return multistep.ActionHalt
		}
	}

	virtualMachineOn, err := powershell.IsVirtualMachineOn(config.CloneFromVMName)
	if err != nil {
		state.Put("error", fmt.Errorf("Failed detecting if virtual machine to clone is running: %s", err))
		return multistep.ActionHalt
	}

	if virtualMachineOn {
		// Just a warning, we don't halt
		ui.Error(fmt.Sprintf("Warning: Cloning from a virtual machine that is running (%s).", config.CloneFromVMName))
	}

	return multistep.ActionContinue
}

func (s *StepValidateClone) Cleanup(state multistep.StateBag) {}
