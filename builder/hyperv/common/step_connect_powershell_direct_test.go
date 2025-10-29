package common

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/packer-plugin-hyperv/communicator/powershelldirect"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepConnectPowerShellDirectRunSuccess(t *testing.T) {
	state := testState(t)
	state.Put("vmName", "test-vm")

	step := &StepConnectPowerShellDirect{
		Config: &PowershellDirectConfig{VMName: "unused", Username: "packer", Password: "secret"},
		Factory: func(vmName string, cfg powershelldirect.Config) (packersdk.Communicator, error) {
			if vmName != "test-vm" {
				t.Fatalf("expected vmName 'test-vm', got %q", vmName)
			}
			if cfg.Username != "packer" {
				t.Fatalf("unexpected username %q", cfg.Username)
			}
			if cfg.Password != "secret" {
				t.Fatalf("unexpected password %q", cfg.Password)
			}
			return &packersdk.MockCommunicator{}, nil
		},
	}

	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}

	if _, ok := state.GetOk("communicator"); !ok {
		t.Fatal("communicator was not stored in state")
	}
}

func TestStepConnectPowerShellDirectRunFailure(t *testing.T) {
	state := testState(t)
	state.Put("vmName", "test-vm")

	expectedErr := errors.New("failed to connect")

	step := &StepConnectPowerShellDirect{
		Config: &PowershellDirectConfig{VMName: "test-vm", Username: "packer", Password: "secret"},
		Factory: func(string, powershelldirect.Config) (packersdk.Communicator, error) {
			return nil, expectedErr
		},
	}

	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}

	errVal, ok := state.GetOk("error")
	if !ok {
		t.Fatal("expected error in state")
	}

	if !errors.Is(errVal.(error), expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, errVal)
	}
}

func TestStepConnectPowerShellDirectMissingVMName(t *testing.T) {
	state := testState(t)

	step := &StepConnectPowerShellDirect{Config: &PowershellDirectConfig{Username: "user", Password: "pass"}}

	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}

	if _, ok := state.GetOk("error"); !ok {
		t.Fatal("expected error when vmName missing")
	}
}

func TestStepConnectPowerShellDirectMissingUsername(t *testing.T) {
	state := testState(t)
	state.Put("vmName", "vm")

	step := &StepConnectPowerShellDirect{Config: &PowershellDirectConfig{Password: "pass"}}

	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}

	if _, ok := state.GetOk("error"); !ok {
		t.Fatal("expected error when username missing")
	}
}

func TestStepConnectPowerShellDirectMissingPassword(t *testing.T) {
	state := testState(t)
	state.Put("vmName", "vm")

	step := &StepConnectPowerShellDirect{Config: &PowershellDirectConfig{Username: "user"}}

	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}

	if _, ok := state.GetOk("error"); !ok {
		t.Fatal("expected error when password missing")
	}
}

func TestStepConnectPowerShellDirectUsesConfigVMName(t *testing.T) {
	state := testState(t)

	step := &StepConnectPowerShellDirect{
		Config: &PowershellDirectConfig{VMName: "configured", Username: "user", Password: "pass"},
		Factory: func(vmName string, cfg powershelldirect.Config) (packersdk.Communicator, error) {
			if vmName != "configured" {
				t.Fatalf("expected vm name 'configured', got %q", vmName)
			}
			if cfg.VMName != "configured" {
				t.Fatalf("expected cfg vm %q, got %q", "configured", cfg.VMName)
			}
			return &packersdk.MockCommunicator{}, nil
		},
	}

	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}

	stored, ok := state.GetOk("vmName")
	if !ok {
		t.Fatalf("expected vmName to be stored in state")
	}
	if stored.(string) != "configured" {
		t.Fatalf("unexpected vmName in state: %q", stored)
	}
}
