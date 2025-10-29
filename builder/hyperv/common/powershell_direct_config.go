package common

import (
	"fmt"
	"strings"

	"github.com/hashicorp/packer-plugin-hyperv/communicator/powershelldirect"
)

// PowershellDirectConfig stores credentials required by the PowerShell Direct communicator.
type PowershellDirectConfig struct {
	// VMName identifies the existing VM to connect to when using standalone provisioning.
	VMName   string `mapstructure:"powershell_direct_vm_name" hcl:"powershell_direct_vm_name"`
	Username string `mapstructure:"powershell_direct_username" hcl:"powershell_direct_username"`
	Password string `mapstructure:"powershell_direct_password" hcl:"powershell_direct_password"`
}

// Prepare validates the configuration and returns any accumulated errors.
func (c *PowershellDirectConfig) Prepare() []error {
	var errs []error

	if strings.TrimSpace(c.Username) == "" {
		errs = append(errs, fmt.Errorf("powershell_direct_username must be provided when communicator is %q", powershelldirect.Type))
	}

	if strings.TrimSpace(c.Password) == "" {
		errs = append(errs, fmt.Errorf("powershell_direct_password must be provided when communicator is %q", powershelldirect.Type))
	}

	return errs
}

// CommunicatorConfig returns the communicator-specific configuration payload.
func (c *PowershellDirectConfig) CommunicatorConfig() powershelldirect.Config {
	return powershelldirect.Config{
		VMName:   c.VMName,
		Username: c.Username,
		Password: c.Password,
	}
}
