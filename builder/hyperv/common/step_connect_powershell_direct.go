package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/packer-plugin-hyperv/communicator/powershelldirect"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// PowerShellDirectFactory constructs a communicator using the supplied VM name and config.
type PowerShellDirectFactory func(vmName string, cfg powershelldirect.Config) (packersdk.Communicator, error)

// StepConnectPowerShellDirect initialises the PowerShell Direct communicator and stores it in state.
type StepConnectPowerShellDirect struct {
	Config  *PowershellDirectConfig
	Factory PowerShellDirectFactory
}

// Run establishes the communicator and persists it for subsequent steps.
func (s *StepConnectPowerShellDirect) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	if s.Config == nil {
		err := fmt.Errorf("powershell direct configuration is not initialised")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	var vmName string
	if rawVMName, ok := state.GetOk("vmName"); ok {
		vmName, ok = rawVMName.(string)
		if !ok {
			err := fmt.Errorf("vmName is not a valid string")
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
	}

	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		vmName = strings.TrimSpace(s.Config.VMName)
		if vmName != "" {
			state.Put("vmName", vmName)
			if _, exists := state.GetOk("instance_id"); !exists {
				state.Put("instance_id", vmName)
			}
		}
	}

	if vmName == "" {
		err := fmt.Errorf("vm name is required: set powershell_direct_vm_name or ensure the builder populates vmName")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	username := strings.TrimSpace(s.Config.Username)
	password := strings.TrimSpace(s.Config.Password)

	if username == "" {
		err := fmt.Errorf("powershell_direct_username must be provided")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	if password == "" {
		err := fmt.Errorf("powershell_direct_password must be provided")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Normalise any extra whitespace so downstream scripts receive clean credentials.
	s.Config.Username = username
	s.Config.Password = password

	factory := s.Factory
	if factory == nil {
		factory = func(name string, cfg powershelldirect.Config) (packersdk.Communicator, error) {
			return powershelldirect.New(name, cfg)
		}
	}

	ui.Say("Connecting to virtual machine using PowerShell Direct...")

	comm, err := factory(vmName, s.Config.CommunicatorConfig())
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	state.Put("communicator", comm)
	return multistep.ActionContinue
}

// Cleanup does not have anything to tear down for the communicator.
func (s *StepConnectPowerShellDirect) Cleanup(state multistep.StateBag) {}
