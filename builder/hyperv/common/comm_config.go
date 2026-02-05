// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	psrp "github.com/jasonsimons/packer-plugin-psrp/communicator/psrp"
)

// CommConfig contains configuration for all communicator types (SSH, WinRM, PSRP)
type CommConfig struct {
	// Standard communicator configuration (SSH/WinRM)
	Comm communicator.Config `mapstructure:",squash"`

	// PSRP communicator configuration
	PSRP psrp.Config `mapstructure:",squash"`
}

// Prepare validates the communicator configuration.
// For "psrp" type, we skip SDK's validation since it only accepts known types.
func (c *CommConfig) Prepare(ctx *interpolate.Context) []error {
	// If using PSRP communicator, validate PSRP config directly
	if c.Comm.Type == "psrp" {
		return c.PSRP.Prepare(ctx)
	}

	// For SSH/WinRM, use SDK's validation
	return c.Comm.Prepare(ctx)
}
