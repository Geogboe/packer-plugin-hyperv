packer {
  required_plugins {
    hyperv = {
      version = ">= 0.0.1"
      source  = "github.com/Geogboe/hyperv"
    }
  }
}

variable "iso_url" {
  type    = string
  default = "https://software-static.download.prss.microsoft.com/sg/download/888969d5-f34g-4e03-ac9d-1f9786c66749/SERVER_EVAL_x64FRE_en-us.iso"
}

variable "iso_checksum" {
  type    = string
  default = "sha256:888969d5f34g4e03ac9d1f9786c66749" // Placeholder
}

// WARNING: This password is a placeholder for example purposes only.
// DO NOT use this value in production. Always override via variables,
// environment variables, or a secure secrets management solution.
variable "psrp_password" {
  type    = string
  default = "Password123!" // Placeholder; must be changed in real environments
}

source "hyperv-iso" "wsman" {
  vm_name           = "psrp-wsman-test"
  iso_url           = var.iso_url
  iso_checksum      = var.iso_checksum
  communicator      = "psrp"
  psrp_username     = "Administrator"
  psrp_password     = var.psrp_password
  psrp_transport    = "wsman"
  psrp_auth_type    = "negotiate"
  cpus              = 2
  memory            = 4096
  disk_size         = 20000
  enable_secure_boot = false
  generation        = 2
  switch_name       = "Default Switch"
  
  // Basic shutdown command
  shutdown_command  = "shutdown /s /t 10 /f /d p:4:1 /c \"Packer Shutdown\""
}

build {
  sources = ["source.hyperv-iso.wsman"]

  provisioner "powershell" {
    inline = [
      "Write-Host 'Connected via PSRP WSMan!'"
    ]
  }
}
