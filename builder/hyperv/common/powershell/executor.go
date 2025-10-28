// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package powershell

import (
	"io"
	"sync"
)

// Executor executes PowerShell scripts using the provided options.
type Executor interface {
	Execute(script string, opts *ExecuteOptions) (string, error)
}

// ExecuteOptions capture execution-time preferences such as parameters and
// stdout/stderr collectors. Options are best-effort; executors should ignore
// values they cannot honor.
type ExecuteOptions struct {
	Params []string
	Stdout io.Writer
	Stderr io.Writer
	Env    map[string]string

	// CaptureOutput instructs the executor to return stdout. When false the
	// returned string may be empty even if the script produced output.
	CaptureOutput bool
}

var (
	execMu   sync.RWMutex
	executor Executor = &localExecutor{}
)

// SetExecutor replaces the global executor used for subsequent PowerShell
// invocations. The returned function restores the previous executor and should
// be deferred to ensure cleanup.
func SetExecutor(e Executor) func() {
	execMu.Lock()
	prev := executor
	executor = e
	execMu.Unlock()

	return func() {
		execMu.Lock()
		executor = prev
		execMu.Unlock()
	}
}

func currentExecutor() Executor {
	execMu.RLock()
	defer execMu.RUnlock()
	return executor
}

func execute(script string, opts *ExecuteOptions) (string, error) {
	if opts == nil {
		opts = &ExecuteOptions{}
	}

	exec := currentExecutor()
	return exec.Execute(script, opts)
}
