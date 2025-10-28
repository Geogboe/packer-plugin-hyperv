// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package powershell

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common/wsl"
)

type localExecutor struct{}

func (e *localExecutor) Execute(script string, opts *ExecuteOptions) (string, error) {
	if opts == nil {
		opts = &ExecuteOptions{}
	}

	path, err := getPowerShellPath()
	if err != nil {
		return "", fmt.Errorf("cannot find PowerShell in the path")
	}

	filename, err := saveScript(script)
	if err != nil {
		return "", err
	}

	debug := os.Getenv("PACKER_POWERSHELL_DEBUG") != ""
	verbose := debug || os.Getenv("PACKER_POWERSHELL_VERBOSE") != ""

	if !debug {
		defer os.Remove(filename)
	}

	if wslPath, err := convertScriptPathForWSL(filename); err == nil {
		filename = wslPath
	} else {
		return "", err
	}

	args := createArgs(filename, opts.Params...)

	if verbose {
		log.Printf("Run: %s %s", path, args)
	}

	command := exec.Command(path, args...)
	command.Env = applyEnvironmentOverrides(os.Environ(), opts.Env)

	captureStdout := opts.CaptureOutput || verbose

	var stdoutBuf bytes.Buffer
	stdoutWriter := io.Writer(io.Discard)
	if captureStdout {
		stdoutWriter = &stdoutBuf
	}
	if opts.Stdout != nil {
		if stdoutWriter != io.Discard {
			stdoutWriter = io.MultiWriter(stdoutWriter, opts.Stdout)
		} else {
			stdoutWriter = opts.Stdout
		}
	}
	if stdoutWriter == nil {
		stdoutWriter = io.Discard
	}
	command.Stdout = stdoutWriter

	var stderrBuf bytes.Buffer
	stderrWriter := io.Writer(&stderrBuf)
	if opts.Stderr != nil {
		stderrWriter = io.MultiWriter(stderrWriter, opts.Stderr)
	}
	command.Stderr = stderrWriter

	err = command.Run()

	stderrString := strings.TrimSpace(stderrBuf.String())
	if stderrString != "" {
		err = fmt.Errorf("PowerShell error: %s", stderrString)
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		err = fmt.Errorf("PowerShell error: %s", strings.TrimSpace(exitErr.Error()))
	}

	stdoutString := ""
	if captureStdout {
		stdoutString = strings.TrimSpace(stdoutBuf.String())
	}

	if verbose && stdoutString != "" {
		log.Printf("stdout: %s", stdoutString)
	}

	if verbose && stderrString != "" {
		log.Printf("stderr: %s", stderrString)
	}

	if opts.CaptureOutput {
		return stdoutString, err
	}

	return "", err
}

func convertScriptPathForWSL(path string) (string, error) {
	if !wsl.IsWSL() {
		return path, nil
	}

	return wsl.ConvertWSlPathToWindowsPath(path)
}

func applyEnvironmentOverrides(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	result := make([]string, 0, len(base)+len(overrides))
	handled := make(map[string]struct{}, len(overrides))

	for _, kv := range base {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			result = append(result, kv)
			continue
		}

		key := parts[0]
		if value, ok := overrides[key]; ok {
			result = append(result, fmt.Sprintf("%s=%s", key, value))
			handled[key] = struct{}{}
		} else {
			result = append(result, kv)
		}
	}

	for key, value := range overrides {
		if _, ok := handled[key]; ok {
			continue
		}
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}

	return result
}
