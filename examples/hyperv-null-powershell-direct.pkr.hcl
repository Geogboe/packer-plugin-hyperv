packer {
  required_plugins {
    hyperv = {
      version = ">= 1.1.6"
      source  = "github.com/hashicorp/hyperv"
    }
  }
}

variable "ps_vm_name" {
  type        = string
  description = "Name of the existing Hyper-V VM reachable via PowerShell Direct."
  default     = env("HYPERV_PS_VM_NAME")
}

variable "ps_username" {
  type        = string
  description = "Local Windows account inside the VM."
  default     = env("HYPERV_PS_USERNAME")
}

variable "ps_password" {
  type        = string
  description = "Password for PowerShell Direct authentication."
  default     = env("HYPERV_PS_PASSWORD")
  sensitive   = true
}

# Hyper-V null builder connects to an existing VM so we can exercise the communicator.
source "hyperv-null" "powershell_direct_smoke" {
  powershell_direct_vm_name = var.ps_vm_name
  communicator              = "powershell-direct"
  powershell_direct_username = var.ps_username
  powershell_direct_password = var.ps_password
}

build {
  sources = [
    "source.hyperv-null.powershell_direct_smoke",
  ]

  provisioner "powershell" {
    inline = [
      "Write-Host \"Validating PowerShell Direct access to $env:COMPUTERNAME\"",
      "whoami",
    ]
  }
}
