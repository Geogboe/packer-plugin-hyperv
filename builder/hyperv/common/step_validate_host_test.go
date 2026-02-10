// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepValidateHost_impl(t *testing.T) {
	var _ multistep.Step = new(StepValidateHost)
}

func testValidateHostState(t *testing.T) (multistep.StateBag, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	state := new(multistep.BasicStateBag)
	state.Put("driver", new(DriverMock))
	writer := new(bytes.Buffer)
	errWriter := new(bytes.Buffer)
	state.Put("ui", &packersdk.BasicUi{
		Reader:      new(bytes.Buffer),
		Writer:      writer,
		ErrorWriter: errWriter,
	})
	return state, writer, errWriter
}

func TestStepValidateHost_VirtExtDisabled(t *testing.T) {
	state, _, _ := testValidateHostState(t)
	step := &StepValidateHost{
		EnableVirtualizationExtensions: false,
		RamSize:                        1024,
		GetHostMemoryFunc:              func() float64 { return 8192 },
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if _, ok := state.GetOk("error"); ok {
		t.Fatal("should NOT have error")
	}
}

func TestStepValidateHost_VirtExtSupported(t *testing.T) {
	state, _, _ := testValidateHostState(t)
	step := &StepValidateHost{
		EnableVirtualizationExtensions: true,
		RamSize:                        1024,
		HasVirtExtFunc:                 func() (bool, error) { return true, nil },
		GetHostMemoryFunc:              func() float64 { return 8192 },
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if _, ok := state.GetOk("error"); ok {
		t.Fatal("should NOT have error")
	}
}

func TestStepValidateHost_VirtExtNotSupported(t *testing.T) {
	state, _, errWriter := testValidateHostState(t)
	step := &StepValidateHost{
		EnableVirtualizationExtensions: true,
		RamSize:                        1024,
		HasVirtExtFunc:                 func() (bool, error) { return false, nil },
		GetHostMemoryFunc:              func() float64 { return 8192 },
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}
	if _, ok := state.GetOk("error"); !ok {
		t.Fatal("should have error in state")
	}
	if !strings.Contains(errWriter.String(), "does not support") {
		t.Fatalf("expected ui.Error output about unsupported extensions, got: %q", errWriter.String())
	}
}

func TestStepValidateHost_VirtExtDetectionError(t *testing.T) {
	state, _, errWriter := testValidateHostState(t)
	step := &StepValidateHost{
		EnableVirtualizationExtensions: true,
		RamSize:                        1024,
		HasVirtExtFunc:                 func() (bool, error) { return false, fmt.Errorf("powershell not found") },
		GetHostMemoryFunc:              func() float64 { return 8192 },
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}
	if _, ok := state.GetOk("error"); !ok {
		t.Fatal("should have error in state")
	}
	if !strings.Contains(errWriter.String(), "failed detecting") {
		t.Fatalf("expected ui.Error output about detection failure, got: %q", errWriter.String())
	}
}

func TestStepValidateHost_LowMemoryWarning(t *testing.T) {
	state, writer, _ := testValidateHostState(t)
	step := &StepValidateHost{
		EnableVirtualizationExtensions: false,
		RamSize:                        1024,
		// 1024 (RAM) + 256 (LowRam) = 1280 needed; 1200 < 1280 triggers warning
		GetHostMemoryFunc: func() float64 { return 1200 },
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if !strings.Contains(writer.String(), "Warning") {
		t.Fatalf("expected low memory warning in output, got: %q", writer.String())
	}
}

func TestStepValidateHost_SufficientMemory(t *testing.T) {
	state, writer, _ := testValidateHostState(t)
	step := &StepValidateHost{
		EnableVirtualizationExtensions: false,
		RamSize:                        1024,
		// 8192 - 1024 = 7168, well above LowRam (256)
		GetHostMemoryFunc: func() float64 { return 8192 },
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if strings.Contains(writer.String(), "Warning") {
		t.Fatalf("should NOT have memory warning, got: %q", writer.String())
	}
}
