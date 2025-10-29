// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package null provides a Hyper-V builder that targets an existing VM.
//
// This is intentionally minimal so we can iterate on communicators (notably
// PowerShell Direct) without provisioning or exporting new guests. The builder
// assumes the VM already exists, leaves it untouched, and emits a no-op
// artifact.
package null

//go:generate packer-sdc struct-markdown
//go:generate packer-sdc mapstructure-to-hcl2 -type Config

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2/hcldec"
	hypervcommon "github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common"
	"github.com/hashicorp/packer-plugin-hyperv/communicator/powershelldirect"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

// Builder implements packersdk.Builder for the Hyper-V null workflow.
type Builder struct {
	config Config
	runner multistep.Runner

	// newRunner allows tests to swap the runner; production uses BasicRunner.
	newRunner func([]multistep.Step) multistep.Runner
}

// Config captures the minimal data needed to connect to an existing VM.
type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	hypervcommon.SSHConfig `mapstructure:",squash"`
	VMName                 string `mapstructure:"powershell_direct_vm_name" required:"true"`
	ctx                    interpolate.Context
}

// Prepare validates the configuration and interpolates user input.
func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	b.config = Config{}

	if err := config.Decode(&b.config, &config.DecodeOpts{
		Interpolate:        true,
		InterpolateContext: &b.config.ctx,
	}, raws...); err != nil {
		return nil, nil, err
	}

	warnings := make([]string, 0)
	var errs *packersdk.MultiError

	if strings.TrimSpace(b.config.VMName) == "" {
		errs = packersdk.MultiErrorAppend(errs, fmt.Errorf("powershell_direct_vm_name must be provided"))
	}

	if strings.TrimSpace(b.config.SSHConfig.Comm.Type) == "" {
		// Default to PowerShell Direct so users can omit the communicator field
		// when iterating on Windows guests.
		b.config.SSHConfig.Comm.Type = powershelldirect.Type
	}

	if strings.EqualFold(b.config.SSHConfig.Comm.Type, powershelldirect.Type) {
		if strings.TrimSpace(b.config.SSHConfig.PowerShellDirect.VMName) == "" {
			b.config.SSHConfig.PowerShellDirect.VMName = b.config.VMName
		}
	}

	errs = packersdk.MultiErrorAppend(errs, b.config.SSHConfig.Prepare(&b.config.ctx)...)

	if errs != nil && len(errs.Errors) > 0 {
		return nil, warnings, errs
	}

	return nil, warnings, nil
}

// ConfigSpec delegates to the flattened HCL2 schema so the builder and docs
// stay consistent with the embedded communicator configuration.
func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

// Run connects to the existing VM and executes provisioners using the
// configured communicator.
func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {
	state := new(multistep.BasicStateBag)
	state.Put("debug", b.config.PackerDebug)
	state.Put("hook", hook)
	state.Put("ui", ui)
	state.Put("vmName", b.config.VMName)
	state.Put("instance_id", b.config.VMName)

	connectStep := &communicator.StepConnect{
		Config:    &b.config.SSHConfig.Comm,
		Host:      hypervcommon.CommHost(b.config.SSHConfig.Comm.Host()),
		SSHConfig: b.config.SSHConfig.Comm.SSHConfigFunc(),
	}

	if strings.EqualFold(b.config.SSHConfig.Comm.Type, powershelldirect.Type) {
		// The PowerShell Direct step needs access to the VM name and the
		// generated config. We reuse StepConnectPowerShellDirect so we benefit
		// from the shared validation and driver plumbing.
		connectStep.Host = hypervcommon.PowerShellDirectHost()
		connectStep.SSHConfig = nil
		connectStep.CustomConnect = map[string]multistep.Step{
			powershelldirect.Type: &hypervcommon.StepConnectPowerShellDirect{
				Config: &b.config.SSHConfig.PowerShellDirect,
			},
		}
	}

	steps := []multistep.Step{
		connectStep,
		&commonsteps.StepProvision{},
	}

	runner := b.runner
	if runner == nil {
		factory := b.newRunner
		if factory == nil {
			factory = func(s []multistep.Step) multistep.Runner {
				return &multistep.BasicRunner{Steps: s}
			}
		}
		runner = factory(steps)
	}
	b.runner = runner

	runner.Run(ctx, state)

	if rawErr, ok := state.GetOk("error"); ok && rawErr != nil {
		return nil, rawErr.(error)
	}

	return newArtifact(b.config.VMName), nil
}
