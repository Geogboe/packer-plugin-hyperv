package powershelldirect

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common/powershell"
	"github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common/wsl"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/tmp"
)

const (
	// Type identifies the communicator type as referenced in configuration.
	Type                 = "powershell-direct"
	commandFailureStatus = 1
)

var (
	errUnsupportedExclude = errors.New("powershell-direct communicator does not support exclude filters")
)

type runner interface {
	Run(script string, params ...string) error
	Output(script string, params ...string) (string, error)
	Stream(script string, params ...string) (streamProcess, error)
}

type streamProcess interface {
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	Kill() error
}

// Config stores connection details required to open a PowerShell Direct session.
type Config struct {
	VMName   string
	Username string
	Password string
}

// Option customises communicator construction.
type Option func(*Communicator)

// WithRunner overrides the PowerShell runner, primarily used in tests.
func WithRunner(r runner) Option {
	return func(c *Communicator) {
		c.runner = r
	}
}

// Communicator executes commands inside the guest via PowerShell Direct.
type Communicator struct {
	vmName string
	config Config
	runner runner
}

// New creates a Communicator instance ready to connect to the supplied VM.
func New(vmName string, cfg Config, opts ...Option) (*Communicator, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("%s communicator requires Windows host", Type)
	}

	if wsl.IsWSL() {
		return nil, fmt.Errorf("%s communicator is not supported when running under WSL", Type)
	}

	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		vmName = strings.TrimSpace(cfg.VMName)
	}
	if vmName == "" {
		return nil, errors.New("vm name must be provided")
	}

	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Password = strings.TrimSpace(cfg.Password)

	if cfg.Username == "" {
		return nil, errors.New("powershell direct username must be provided")
	}
	if cfg.Password == "" {
		return nil, errors.New("powershell direct password must be provided")
	}

	packersdk.LogSecretFilter.Set(cfg.Password)

	communicator := &Communicator{
		vmName: vmName,
		config: cfg,
		runner: &powershellRunner{},
	}

	for _, opt := range opts {
		opt(communicator)
	}

	return communicator, nil
}

// Start launches the provided command asynchronously inside the guest.
func (c *Communicator) Start(ctx context.Context, cmd *packersdk.RemoteCmd) error {
	if cmd == nil {
		return errors.New("remote command cannot be nil")
	}

	if strings.TrimSpace(cmd.Command) == "" {
		cmd.SetExited(0)
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	process, err := c.runner.Stream(executeCommandScript, c.vmName, c.config.Username, c.config.Password, cmd.Command)
	if err != nil {
		return err
	}

	stdout := process.Stdout()
	stderr := process.Stderr()

	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = process.Wait()
		if stdout != nil {
			_ = stdout.Close()
		}
		if stderr != nil {
			_ = stderr.Close()
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			_ = process.Kill()
		case <-done:
		}
	}()

	go c.consumeMessages(stdout, cmd)

	go func() {
		if stderr == nil {
			return
		}
		var target io.Writer = io.Discard
		if cmd.Stderr != nil {
			target = cmd.Stderr
		}
		if _, err := io.Copy(target, stderr); err != nil && cmd.Stderr != nil {
			fmt.Fprintf(cmd.Stderr, "%s\n", err.Error())
		}
	}()

	return nil
}

// Upload copies a single file into the guest operating system.
func (c *Communicator) Upload(path string, input io.Reader, fi *os.FileInfo) error {
	if input == nil {
		return errors.New("upload input cannot be nil")
	}

	tempFile, err := os.CreateTemp("", "packer-powershelldirect-upload")
	if err != nil {
		return err
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempFile.Name())
	}()

	if _, err := io.Copy(tempFile, input); err != nil {
		return err
	}

	if err := tempFile.Close(); err != nil {
		return err
	}

	sourcePath, err := c.hostPath(tempFile.Name())
	if err != nil {
		return err
	}

	return c.runner.Run(uploadFileScript, c.vmName, c.config.Username, c.config.Password, sourcePath, path)
}

// UploadDir copies a directory tree into the guest.
func (c *Communicator) UploadDir(dst string, src string, exclude []string) error {
	if len(exclude) > 0 {
		return errUnsupportedExclude
	}

	includeRoot := includeSourceRoot(src)

	hostPath, err := c.hostPath(src)
	if err != nil {
		return err
	}

	return c.runner.Run(uploadDirectoryScript, c.vmName, c.config.Username, c.config.Password, hostPath, dst, strconv.FormatBool(includeRoot))
}

// Download retrieves a file from the guest and writes it into the given writer.
func (c *Communicator) Download(path string, output io.Writer) error {
	if output == nil {
		return errors.New("download output cannot be nil")
	}

	tempFile, err := os.CreateTemp("", "packer-powershelldirect-download")
	if err != nil {
		return err
	}
	tempFilePath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempFilePath)

	hostPath, err := c.hostPath(tempFilePath)
	if err != nil {
		return err
	}

	if err := c.runner.Run(downloadFileScript, c.vmName, c.config.Username, c.config.Password, hostPath, path); err != nil {
		return err
	}

	file, err := os.Open(tempFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(output, file)
	return err
}

// DownloadDir copies a directory tree from the guest onto the host filesystem.
func (c *Communicator) DownloadDir(src string, dst string, exclude []string) error {
	if len(exclude) > 0 {
		return errUnsupportedExclude
	}

	includeRoot := includeSourceRoot(src)

	destPath, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(destPath, 0o755); err != nil {
		return err
	}

	hostPath, err := c.hostPath(destPath)
	if err != nil {
		return err
	}

	return c.runner.Run(downloadDirectoryScript, c.vmName, c.config.Username, c.config.Password, src, hostPath, strconv.FormatBool(includeRoot))
}

func (c *Communicator) hostPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	if wsl.IsWSL() {
		converted, err := wsl.ConvertWSlPathToWindowsPath(absolute)
		if err != nil {
			return "", err
		}
		return converted, nil
	}

	return absolute, nil
}

type streamMessage struct {
	Stream string `json:"stream"`
	Data   string `json:"data"`
	Code   int    `json:"code"`
}

func (c *Communicator) consumeMessages(reader io.Reader, cmd *packersdk.RemoteCmd) {
	if reader == nil {
		cmd.SetExited(commandFailureStatus)
		return
	}

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 128*1024)
	scanner.Buffer(buf, 2*1024*1024)

	exitHandled := false
	exitCode := commandFailureStatus

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg streamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			if cmd.Stderr != nil {
				fmt.Fprintf(cmd.Stderr, "decode stream message: %v\n", err)
			}
			continue
		}

		switch msg.Stream {
		case "stdout":
			if cmd.Stdout == nil {
				continue
			}

			data, err := decodeBase64(msg.Data)
			if err != nil {
				if cmd.Stderr != nil {
					fmt.Fprintf(cmd.Stderr, "decode stdout payload: %v\n", err)
				}
				continue
			}

			if len(data) > 0 {
				_, _ = cmd.Stdout.Write(data)
			}

		case "stderr":
			if cmd.Stderr == nil {
				continue
			}

			data, err := decodeBase64(msg.Data)
			if err != nil {
				fmt.Fprintf(cmd.Stderr, "decode stderr payload: %v\n", err)
				continue
			}

			if len(data) > 0 {
				_, _ = cmd.Stderr.Write(data)
			}

		case "exit":
			exitHandled = true
			exitCode = msg.Code
			cmd.SetExited(exitCode)
			return

		default:
			if cmd.Stderr != nil {
				fmt.Fprintf(cmd.Stderr, "unexpected stream message type: %s\n", msg.Stream)
			}
		}
	}

	if err := scanner.Err(); err != nil && cmd.Stderr != nil {
		fmt.Fprintf(cmd.Stderr, "stream read error: %v\n", err)
	}

	if !exitHandled {
		cmd.SetExited(exitCode)
	}
}

func decodeBase64(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	return base64.StdEncoding.DecodeString(value)
}

func includeSourceRoot(path string) bool {
	if path == "" {
		return false
	}

	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\") {
		return false
	}

	return true
}

type powershellRunner struct{}

func (p *powershellRunner) Run(script string, params ...string) error {
	var base powershell.PowerShellCmd
	return base.Run(script, params...)
}

func (p *powershellRunner) Output(script string, params ...string) (string, error) {
	var base powershell.PowerShellCmd
	return base.Output(script, params...)
}

func (p *powershellRunner) Stream(script string, params ...string) (streamProcess, error) {
	available, path, err := powershell.IsPowershellAvailable()
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, errors.New("cannot find PowerShell in PATH")
	}

	filename, cleanup, err := saveStreamingScript(script)
	if err != nil {
		return nil, err
	}

	if wsl.IsWSL() {
		converted, err := wsl.ConvertWSlPathToWindowsPath(filename)
		if err != nil {
			cleanup()
			return nil, err
		}
		filename = converted
	}

	args := buildPowerShellArgs(filename, params...)

	cmd := exec.Command(path, args...)
	cmd.Env = powershell.CommandEnv()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}

	return &execStreamProcess{
		cmd:     cmd,
		stdout:  stdout,
		stderr:  stderr,
		cleanup: cleanup,
	}, nil
}

type execStreamProcess struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	cleanup func()
	once    sync.Once
}

func (p *execStreamProcess) Stdout() io.ReadCloser {
	return p.stdout
}

func (p *execStreamProcess) Stderr() io.ReadCloser {
	return p.stderr
}

func (p *execStreamProcess) Wait() error {
	err := p.cmd.Wait()
	p.close()
	return err
}

func (p *execStreamProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	err := p.cmd.Process.Kill()
	p.close()
	return err
}

func (p *execStreamProcess) close() {
	p.once.Do(func() {
		if p.cleanup != nil {
			p.cleanup()
		}
	})
}

func saveStreamingScript(contents string) (string, func(), error) {
	var (
		file *os.File
		err  error
	)

	debug := os.Getenv("PACKER_POWERSHELL_DEBUG") != ""

	if wsl.IsWSL() {
		tmpDir, err := wsl.GetWSlTemp()
		if err != nil {
			return "", func() {}, err
		}

		wslTempDir, err := wsl.ConvertWindowsPathToWSlPath(tmpDir)
		if err != nil {
			return "", func() {}, err
		}

		file, err = os.CreateTemp(wslTempDir, "powershell")
	} else {
		file, err = tmp.File("Powershell")
	}

	if err != nil {
		return "", func() {}, err
	}

	if _, err = file.Write([]byte(contents)); err != nil {
		file.Close()
		return "", func() {}, err
	}

	if err = file.Close(); err != nil {
		return "", func() {}, err
	}

	newFilename := file.Name() + ".ps1"
	if err = os.Rename(file.Name(), newFilename); err != nil {
		return "", func() {}, err
	}

	cleanup := func() {
		if debug {
			return
		}
		_ = os.Remove(newFilename)
	}

	return newFilename, cleanup, nil
}

func buildPowerShellArgs(filename string, params ...string) []string {
	args := make([]string, len(params)+5)
	args[0] = "-ExecutionPolicy"
	args[1] = "Bypass"
	args[2] = "-NoProfile"
	args[3] = "-File"
	args[4] = filename

	copy(args[5:], params)

	return args
}

const executeCommandScript = `
using module Microsoft.PowerShell.Utility
using module Hyper-V
using module Microsoft.PowerShell.Security
using module Microsoft.PowerShell.Management

param(
	[string]$VmName,
	[string]$UserName,
	[string]$Password,
	[string]$CommandText
)

function Write-StreamMessage {
	param(
		[string]$Stream,
		[string]$Text
	)

	if ([string]::IsNullOrEmpty($Text)) {
		return
	}

	$bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
	$encoded = [Convert]::ToBase64String($bytes)

	[PSCustomObject]@{
		stream = $Stream
		data   = $encoded
	} | ConvertTo-Json -Compress
}

function Write-ExitMessage {
	param(
		[int]$Code
	)

	[PSCustomObject]@{
		stream = 'exit'
		code   = $Code
	} | ConvertTo-Json -Compress
}

trap {
	$message = $_ | Out-String
	$sessionVar = Get-Variable -Name session -Scope script -ErrorAction SilentlyContinue
	if ($null -ne $sessionVar) {
		$scriptSession = $sessionVar.Value
		if ($scriptSession -ne $null) {
			Remove-PSSession -Session $scriptSession -ErrorAction SilentlyContinue
		}
	}
	Write-Output (Write-StreamMessage -Stream 'stderr' -Text $message)
	Write-Output (Write-ExitMessage -Code 1)
	exit 1
}

$ErrorActionPreference = 'Stop'

$PSModuleAutoLoadingPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
Remove-Module VMware* -Force -ea 0
Import-Module Hyper-V -Prefix packer

if ([string]::IsNullOrWhiteSpace($UserName) -or [string]::IsNullOrWhiteSpace($Password)) {
	$msg = 'PowerShell Direct credentials are not set. Specify powershell_direct_username and powershell_direct_password.'
	Write-Output (Write-StreamMessage -Stream 'stderr' -Text $msg)
	Write-Output (Write-ExitMessage -Code 1)
	exit 1
}

$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
$credential = New-Object System.Management.Automation.PSCredential($UserName, $securePassword)
$session = New-PSSession -VMName $VmName -Credential $credential

try {
	Invoke-Command -Session $session -ArgumentList $CommandText -ScriptBlock {
		param($Cmd)

		function Write-StreamMessage {
			param(
				[string]$Stream,
				[string]$Text
			)

			if ([string]::IsNullOrEmpty($Text)) {
				return
			}

			$bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
			$encoded = [Convert]::ToBase64String($bytes)

			[PSCustomObject]@{
				stream = $Stream
				data   = $encoded
			} | ConvertTo-Json -Compress
		}

		function Write-ExitMessage {
			param(
				[int]$Code
			)

			[PSCustomObject]@{
				stream = 'exit'
				code   = $Code
			} | ConvertTo-Json -Compress
		}

		trap {
			$message = $_ | Out-String
			Write-Output (Write-StreamMessage -Stream 'stderr' -Text $message)
			Write-Output (Write-ExitMessage -Code 1)
			exit 1
		}

		$ErrorActionPreference = 'Stop'

		$commandBytes = [System.Text.Encoding]::Unicode.GetBytes($Cmd)
		$encodedCommand = [Convert]::ToBase64String($commandBytes)

		$startInfo = New-Object System.Diagnostics.ProcessStartInfo
		$startInfo.FileName = 'powershell.exe'
		$startInfo.Arguments = '-NoProfile -NonInteractive -EncodedCommand ' + $encodedCommand
		$startInfo.RedirectStandardOutput = $true
		$startInfo.RedirectStandardError = $true
		$startInfo.UseShellExecute = $false
		$startInfo.CreateNoWindow = $true
		$startInfo.StandardOutputEncoding = [System.Text.Encoding]::UTF8
		$startInfo.StandardErrorEncoding = [System.Text.Encoding]::UTF8

		$process = New-Object System.Diagnostics.Process
		$process.StartInfo = $startInfo

		$exitCode = 1

		try {
			if (-not $process.Start()) {
				throw 'Failed to start process.'
			}

			while (-not $process.HasExited) {
				while (-not $process.StandardOutput.EndOfStream) {
					$line = $process.StandardOutput.ReadLine()
					if ($line -ne $null) {
						Write-Output (Write-StreamMessage -Stream 'stdout' -Text ($line + [System.Environment]::NewLine))
					}
				}

				while (-not $process.StandardError.EndOfStream) {
					$line = $process.StandardError.ReadLine()
					if ($line -ne $null) {
						Write-Output (Write-StreamMessage -Stream 'stderr' -Text ($line + [System.Environment]::NewLine))
					}
				}

				Start-Sleep -Milliseconds 25
			}

			while (-not $process.StandardOutput.EndOfStream) {
				$line = $process.StandardOutput.ReadLine()
				if ($line -ne $null) {
					Write-Output (Write-StreamMessage -Stream 'stdout' -Text ($line + [System.Environment]::NewLine))
				}
			}

			while (-not $process.StandardError.EndOfStream) {
				$line = $process.StandardError.ReadLine()
				if ($line -ne $null) {
					Write-Output (Write-StreamMessage -Stream 'stderr' -Text ($line + [System.Environment]::NewLine))
				}
			}

			$exitCode = $process.ExitCode
		} catch {
			Write-Output (Write-StreamMessage -Stream 'stderr' -Text ($_ | Out-String))
		} finally {
			if ($process -ne $null) {
				$process.Dispose()
			}
		}

		Write-Output (Write-ExitMessage -Code $exitCode)
	}
} catch {
	$message = $_ | Out-String
	Write-Output (Write-StreamMessage -Stream 'stderr' -Text $message)
	Write-Output (Write-ExitMessage -Code 1)
} finally {
	if ($session -ne $null) {
		Remove-PSSession -Session $session
	}
}
`

const uploadFileScript = `
using module Microsoft.PowerShell.Utility
using module Hyper-V
using module Microsoft.PowerShell.Security
using module Microsoft.PowerShell.Management

param(
	[string]$VmName,
	[string]$UserName,
	[string]$Password,
	[string]$SourcePath,
	[string]$DestinationPath
)

trap {
	$message = $_ | Out-String
	$sessionVar = Get-Variable -Name session -Scope script -ErrorAction SilentlyContinue
	if ($null -ne $sessionVar) {
		$scriptSession = $sessionVar.Value
		if ($scriptSession -ne $null) {
			Remove-PSSession -Session $scriptSession -ErrorAction SilentlyContinue
		}
	}
	Write-Error -Message $message
	exit 1
}

$ErrorActionPreference = 'Stop'

$PSModuleAutoLoadingPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
Remove-Module VMware* -Force -ea 0
Import-Module Hyper-V -Prefix packer

if ([string]::IsNullOrWhiteSpace($UserName) -or [string]::IsNullOrWhiteSpace($Password)) {
	throw 'PowerShell Direct credentials are not set. Specify powershell_direct_username and powershell_direct_password.'
}

$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
$credential = New-Object System.Management.Automation.PSCredential($UserName, $securePassword)
$session = New-PSSession -VMName $VmName -Credential $credential

try {
	$destinationParent = Split-Path -Parent -Path $DestinationPath
	if (![string]::IsNullOrEmpty($destinationParent)) {
		Invoke-Command -Session $session -ScriptBlock {
			param($Path)
			if (-not (Test-Path -Path $Path)) {
				New-Item -ItemType Directory -Force -Path $Path | Out-Null
			}
		} -ArgumentList $destinationParent
	}

	Copy-Item -Path $SourcePath -Destination $DestinationPath -ToSession $session -Force
}
finally {
	Remove-PSSession -Session $session
}
`

const uploadDirectoryScript = `
using module Microsoft.PowerShell.Utility
using module Hyper-V
using module Microsoft.PowerShell.Security
using module Microsoft.PowerShell.Management

param(
	[string]$VmName,
	[string]$UserName,
	[string]$Password,
	[string]$SourcePath,
	[string]$DestinationPath,
	[bool]$IncludeRoot
)

trap {
	$message = $_ | Out-String
	$sessionVar = Get-Variable -Name session -Scope script -ErrorAction SilentlyContinue
	if ($null -ne $sessionVar) {
		$scriptSession = $sessionVar.Value
		if ($scriptSession -ne $null) {
			Remove-PSSession -Session $scriptSession -ErrorAction SilentlyContinue
		}
	}
	Write-Error -Message $message
	exit 1
}

$PSModuleAutoLoadingPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
Remove-Module VMware* -Force -ea 0
Import-Module Hyper-V -Prefix packer

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($UserName) -or [string]::IsNullOrWhiteSpace($Password)) {
	throw 'PowerShell Direct credentials are not set. Specify powershell_direct_username and powershell_direct_password.'
}

$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
$credential = New-Object System.Management.Automation.PSCredential($UserName, $securePassword)
$session = New-PSSession -VMName $VmName -Credential $credential

try {
	Invoke-Command -Session $session -ScriptBlock {
		param($Path)
		if (-not (Test-Path -Path $Path)) {
			New-Item -ItemType Directory -Force -Path $Path | Out-Null
		}
	} -ArgumentList $DestinationPath

	if ($IncludeRoot) {
		$leaf = Split-Path -Leaf -Path $SourcePath
		$target = Join-Path -Path $DestinationPath -ChildPath $leaf
		Copy-Item -Path $SourcePath -Destination $target -ToSession $session -Recurse -Force
	} else {
		$items = Get-ChildItem -LiteralPath $SourcePath -Force
		foreach ($item in $items) {
			Copy-Item -Path $item.FullName -Destination $DestinationPath -ToSession $session -Recurse -Force
		}
	}
}
finally {
	Remove-PSSession -Session $session
}
`

const downloadFileScript = `
using module Microsoft.PowerShell.Utility
using module Hyper-V
using module Microsoft.PowerShell.Security
using module Microsoft.PowerShell.Management

param(
	[string]$VmName,
	[string]$UserName,
	[string]$Password,
	[string]$LocalPath,
	[string]$RemotePath
)

trap {
	$message = $_ | Out-String
	$sessionVar = Get-Variable -Name session -Scope script -ErrorAction SilentlyContinue
	if ($null -ne $sessionVar) {
		$scriptSession = $sessionVar.Value
		if ($scriptSession -ne $null) {
			Remove-PSSession -Session $scriptSession -ErrorAction SilentlyContinue
		}
	}
	Write-Error -Message $message
	exit 1
}

$PSModuleAutoLoadingPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($UserName) -or [string]::IsNullOrWhiteSpace($Password)) {
	throw 'PowerShell Direct credentials are not set. Specify powershell_direct_username and powershell_direct_password.'
}

$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
$credential = New-Object System.Management.Automation.PSCredential($UserName, $securePassword)
$session = New-PSSession -VMName $VmName -Credential $credential

try {
	$parent = Split-Path -Parent -Path $LocalPath
	if (![string]::IsNullOrEmpty($parent)) {
		New-Item -ItemType Directory -Force -Path $parent -ErrorAction SilentlyContinue | Out-Null
	}

	Copy-Item -FromSession $session -Path $RemotePath -Destination $LocalPath -Force
}
finally {
	Remove-PSSession -Session $session
}
`

const downloadDirectoryScript = `
using module Microsoft.PowerShell.Utility
using module Hyper-V
using module Microsoft.PowerShell.Security
using module Microsoft.PowerShell.Management

param(
	[string]$VmName,
	[string]$UserName,
	[string]$Password,
	[string]$RemotePath,
	[string]$LocalPath,
	[bool]$IncludeRoot
)

$PSModuleAutoLoadingPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

trap {
	$message = $_ | Out-String
	$sessionVar = Get-Variable -Name session -Scope script -ErrorAction SilentlyContinue
	if ($null -ne $sessionVar) {
		$scriptSession = $sessionVar.Value
		if ($scriptSession -ne $null) {
			Remove-PSSession -Session $scriptSession -ErrorAction SilentlyContinue
		}
	}
	Write-Error -Message $message
	exit 1
}

if ([string]::IsNullOrWhiteSpace($UserName) -or [string]::IsNullOrWhiteSpace($Password)) {
	throw 'PowerShell Direct credentials are not set. Specify powershell_direct_username and powershell_direct_password.'
}

$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
$credential = New-Object System.Management.Automation.PSCredential($UserName, $securePassword)
$session = New-PSSession -VMName $VmName -Credential $credential

try {
	New-Item -ItemType Directory -Force -Path $LocalPath -ErrorAction SilentlyContinue | Out-Null

	if ($IncludeRoot) {
		$leaf = Split-Path -Leaf -Path $RemotePath
		$target = Join-Path -Path $LocalPath -ChildPath $leaf
		New-Item -ItemType Directory -Force -Path $target -ErrorAction SilentlyContinue | Out-Null
		Copy-Item -FromSession $session -Path $RemotePath -Destination $target -Recurse -Force
	} else {
		$items = Invoke-Command -Session $session -ScriptBlock {
			param($Path)
			Get-ChildItem -LiteralPath $Path -Force | Select-Object -ExpandProperty FullName
		} -ArgumentList $RemotePath

		foreach ($item in $items) {
			Copy-Item -FromSession $session -Path $item -Destination $LocalPath -Recurse -Force
		}
	}
}
finally {
	Remove-PSSession -Session $session
}
`

var _ packersdk.Communicator = (*Communicator)(nil)
