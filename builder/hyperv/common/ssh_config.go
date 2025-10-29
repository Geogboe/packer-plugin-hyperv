// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"

	"github.com/hashicorp/packer-plugin-hyperv/communicator/powershelldirect"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

type SSHConfig struct {
	Comm             communicator.Config    `mapstructure:",squash"`
	PowerShellDirect PowershellDirectConfig `mapstructure:",squash"`
}

func (c *SSHConfig) Prepare(ctx *interpolate.Context) []error {
	if strings.EqualFold(c.Comm.Type, powershelldirect.Type) {
		return c.PowerShellDirect.Prepare()
	}

	return c.Comm.Prepare(ctx)
}
