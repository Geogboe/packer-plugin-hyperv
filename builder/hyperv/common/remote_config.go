// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

const (
	transportWinRM = "winrm"
	transportSSH   = "ssh"

	defaultWinRMPortHTTP  = 5985
	defaultWinRMPortHTTPS = 5986

	authNegotiate = "negotiate"
	authKerberos  = "kerberos"
	authBasic     = "basic"
)

// RemoteConfig captures connection information for executing Hyper-V commands on
// a remote Windows host.
type RemoteConfig struct {
	Host              string `mapstructure:"hyperv_host"`
	Username          string `mapstructure:"hyperv_username"`
	Password          string `mapstructure:"hyperv_password"`
	Transport         string `mapstructure:"hyperv_transport"`
	PowerShellCommand string `mapstructure:"hyperv_powershell_command"`
	KeepRemoteScripts bool   `mapstructure:"hyperv_keep_remote_scripts"`
	SkipRemoteCleanup bool   `mapstructure:"hyperv_skip_remote_cleanup"`

	// WinRM specific settings
	WinRMAuth     string `mapstructure:"hyperv_winrm_auth"`
	WinRMUseSSL   bool   `mapstructure:"hyperv_winrm_use_ssl"`
	WinRMInsecure bool   `mapstructure:"hyperv_winrm_insecure"`
	WinRMPort     int    `mapstructure:"hyperv_winrm_port"`
	WinRMDomain   string `mapstructure:"hyperv_winrm_domain"`

	// SSH specific settings
	SSHPort               int    `mapstructure:"hyperv_ssh_port"`
	SSHPassword           string `mapstructure:"hyperv_ssh_password"`
	SSHPrivateKey         string `mapstructure:"hyperv_ssh_private_key"`
	SSHPrivateKeyPassword string `mapstructure:"hyperv_ssh_private_key_password"`
}

// Enabled returns true when remote execution has been requested.
func (c *RemoteConfig) Enabled() bool {
	return strings.TrimSpace(c.Host) != ""
}

// Prepare validates the supplied configuration and applies defaults.
func (c *RemoteConfig) Prepare(_ *interpolate.Context) ([]error, []string) {
	if !c.Enabled() {
		return nil, nil
	}

	var errs []error
	var warns []string

	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" {
		c.Transport = transportWinRM
	}

	switch c.Transport {
	case transportWinRM:
		errs = append(errs, c.prepareWinRM()...)
	case transportSSH:
		errs = append(errs, c.prepareSSH()...)
	default:
		errs = append(errs, fmt.Errorf("hyperv_transport must be one of %q or %q", transportWinRM, transportSSH))
	}

	return errs, warns
}

func (c *RemoteConfig) prepareWinRM() []error {
	var errs []error

	if strings.TrimSpace(c.Username) == "" {
		errs = append(errs, fmt.Errorf("hyperv_username must be provided when using remote Hyper-V"))
	}

	if c.WinRMPort == 0 {
		if c.WinRMUseSSL {
			c.WinRMPort = defaultWinRMPortHTTPS
		} else {
			c.WinRMPort = defaultWinRMPortHTTP
		}
	}

	if hostErr := validateHostPortCombination(c.Host, c.WinRMPort); hostErr != nil {
		errs = append(errs, hostErr)
	}

	c.WinRMAuth = strings.ToLower(strings.TrimSpace(c.WinRMAuth))
	if c.WinRMAuth == "" {
		c.WinRMAuth = authNegotiate
	}

	switch c.WinRMAuth {
	case authNegotiate, authKerberos, authBasic:
		// valid
	default:
		errs = append(errs, fmt.Errorf("hyperv_winrm_auth must be one of %q, %q, or %q", authNegotiate, authKerberos, authBasic))
	}

	if c.WinRMAuth == authBasic && !c.WinRMUseSSL {
		errs = append(errs, fmt.Errorf("hyperv_winrm_auth \"basic\" requires hyperv_winrm_use_ssl to be true"))
	}

	return errs
}

func (c *RemoteConfig) prepareSSH() []error {
	var errs []error

	if strings.TrimSpace(c.Username) == "" {
		errs = append(errs, fmt.Errorf("hyperv_username must be provided when using remote Hyper-V"))
	}

	if c.SSHPort == 0 {
		c.SSHPort = 22
	}

	if hostErr := validateHostPortCombination(c.Host, c.SSHPort); hostErr != nil {
		errs = append(errs, hostErr)
	}

	passwordProvided := strings.TrimSpace(c.SSHPassword) != ""
	keyProvided := strings.TrimSpace(c.SSHPrivateKey) != ""

	if !passwordProvided && !keyProvided {
		errs = append(errs, fmt.Errorf("either hyperv_ssh_password or hyperv_ssh_private_key must be provided when hyperv_transport is ssh"))
	}

	return errs
}

func validateHostPortCombination(host string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d for remote Hyper-V host", port)
	}

	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return fmt.Errorf("hyperv_host must be provided when remote Hyper-V is enabled")
	}

	if strings.Contains(trimmed, ":") {
		// If host already contains a port, ensure it matches provided value.
		parsedHost, parsedPort, err := net.SplitHostPort(trimmed)
		if err != nil {
			return fmt.Errorf("hyperv_host could not be parsed: %w", err)
		}
		if parsedPort != strconv.Itoa(port) {
			return fmt.Errorf("hyperv_host includes port %s but hyperv_*_port is %d; please specify the port in only one place", parsedPort, port)
		}
		if parsedHost == "" {
			return fmt.Errorf("hyperv_host must include a hostname when specifying a port")
		}
	}

	return nil
}
