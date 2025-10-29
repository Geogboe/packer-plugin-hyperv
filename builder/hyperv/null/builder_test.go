package null

import (
	"context"
	"testing"

	hypervcommon "github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common"
	"github.com/hashicorp/packer-plugin-hyperv/communicator/powershelldirect"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestPrepareRequiresVMName(t *testing.T) {
	builder := new(Builder)

	_, _, err := builder.Prepare(map[string]interface{}{
		"communicator":               powershelldirect.Type,
		"powershell_direct_username": "user",
		"powershell_direct_password": "pass",
	})
	if err == nil {
		t.Fatalf("expected error when vm_name missing")
	}
}

func TestPrepareDefaultsPowerShellDirect(t *testing.T) {
	builder := new(Builder)

	warnings, _, err := builder.Prepare(map[string]interface{}{
		"powershell_direct_vm_name":  "existing",
		"powershell_direct_username": "user",
		"powershell_direct_password": "pass",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	if builder.config.SSHConfig.Comm.Type != powershelldirect.Type {
		t.Fatalf("expected communicator %q, got %q", powershelldirect.Type, builder.config.SSHConfig.Comm.Type)
	}
	if builder.config.SSHConfig.PowerShellDirect.VMName != "existing" {
		t.Fatalf("expected ps direct vm name to default, got %q", builder.config.SSHConfig.PowerShellDirect.VMName)
	}
}

func TestRunConfiguresPowerShellDirectStep(t *testing.T) {
	builder := new(Builder)

	if _, _, err := builder.Prepare(map[string]interface{}{
		"powershell_direct_vm_name":  "existing",
		"communicator":               powershelldirect.Type,
		"powershell_direct_username": "user",
		"powershell_direct_password": "pass",
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	var captured *fakeRunner
	builder.newRunner = func(steps []multistep.Step) multistep.Runner {
		captured = &fakeRunner{steps: steps}
		return captured
	}

	ui := packersdk.TestUi(t)
	hook := &packersdk.MockHook{}

	art, err := builder.Run(context.Background(), ui, hook)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if art == nil {
		t.Fatal("expected artifact")
	}

	if captured == nil {
		t.Fatal("runner was not constructed")
	}

	if got := captured.state.Get("vmName").(string); got != "existing" {
		t.Fatalf("vmName not stored, got %q", got)
	}

	connect, ok := captured.steps[0].(*communicator.StepConnect)
	if !ok {
		t.Fatalf("first step not StepConnect: %T", captured.steps[0])
	}

	psStep, ok := connect.CustomConnect[powershelldirect.Type].(*hypervcommon.StepConnectPowerShellDirect)
	if !ok {
		t.Fatalf("powershell direct step not configured")
	}
	if psStep.Config.VMName != "existing" {
		t.Fatalf("powershell direct vm mismatch: %q", psStep.Config.VMName)
	}
}

type fakeRunner struct {
	steps []multistep.Step
	state multistep.StateBag
}

func (r *fakeRunner) Run(ctx context.Context, state multistep.StateBag) {
	r.state = state
}
