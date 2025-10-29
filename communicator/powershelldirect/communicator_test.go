package powershelldirect

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

type runnerCall struct {
	script string
	params []string
}

type stubRunner struct {
	mu              sync.Mutex
	runCalls        []runnerCall
	outputCalls     []runnerCall
	streamCalls     []runnerCall
	runErrors       []error
	outputErrors    []error
	streamErrors    []error
	outputResponses []string
	streamProcesses []streamProcess
	runHook         func(string, ...string) error
	outputHook      func(string, ...string) (string, error)
	streamHook      func(string, ...string) (streamProcess, error)
}

func (s *stubRunner) Run(script string, params ...string) error {
	s.mu.Lock()
	call := runnerCall{script: script, params: append([]string(nil), params...)}
	s.runCalls = append(s.runCalls, call)
	runHook := s.runHook
	var err error
	if runHook == nil && len(s.runErrors) > 0 {
		err = s.runErrors[0]
		s.runErrors = s.runErrors[1:]
	}
	s.mu.Unlock()

	if runHook != nil {
		return runHook(script, params...)
	}

	return err
}

func (s *stubRunner) Output(script string, params ...string) (string, error) {
	s.mu.Lock()
	call := runnerCall{script: script, params: append([]string(nil), params...)}
	s.outputCalls = append(s.outputCalls, call)
	outputHook := s.outputHook
	var resp string
	var err error
	if outputHook == nil {
		if len(s.outputResponses) > 0 {
			resp = s.outputResponses[0]
			s.outputResponses = s.outputResponses[1:]
		}
		if len(s.outputErrors) > 0 {
			err = s.outputErrors[0]
			s.outputErrors = s.outputErrors[1:]
		}
	}
	s.mu.Unlock()

	if outputHook != nil {
		return outputHook(script, params...)
	}

	return resp, err
}

func (s *stubRunner) Stream(script string, params ...string) (streamProcess, error) {
	s.mu.Lock()
	call := runnerCall{script: script, params: append([]string(nil), params...)}
	s.streamCalls = append(s.streamCalls, call)
	streamHook := s.streamHook
	var proc streamProcess
	var err error
	if streamHook == nil {
		if len(s.streamProcesses) > 0 {
			proc = s.streamProcesses[0]
			s.streamProcesses = s.streamProcesses[1:]
		}
		if len(s.streamErrors) > 0 {
			err = s.streamErrors[0]
			s.streamErrors = s.streamErrors[1:]
		}
	}
	s.mu.Unlock()

	if streamHook != nil {
		return streamHook(script, params...)
	}

	return proc, err
}

type stubStreamProcess struct {
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	waitErr  error
	killErr  error
	waitHook func() error
	killHook func() error
}

func (p *stubStreamProcess) Stdout() io.ReadCloser {
	return p.stdout
}

func (p *stubStreamProcess) Stderr() io.ReadCloser {
	return p.stderr
}

func (p *stubStreamProcess) Wait() error {
	if p.waitHook != nil {
		return p.waitHook()
	}
	return p.waitErr
}

func (p *stubStreamProcess) Kill() error {
	if p.killHook != nil {
		return p.killHook()
	}
	return p.killErr
}

func newTestCommunicator(r runner) *Communicator {
	return &Communicator{
		vmName: "test-vm",
		config: Config{VMName: "test-vm", Username: "user", Password: "pass"},
		runner: r,
	}
}

func TestNewUsesConfigVMName(t *testing.T) {
	comm, err := New("", Config{VMName: "configured", Username: "user", Password: "pass"})
	if err != nil {
		t.Fatalf("new communicator: %v", err)
	}

	if comm.vmName != "configured" {
		t.Fatalf("expected vm name 'configured', got %q", comm.vmName)
	}
}

func TestStartExecutionSuccess(t *testing.T) {
	stdoutPayload := "hello\r\n"
	stderrPayload := "there\r\n"

	lines := strings.Join([]string{
		fmt.Sprintf(`{"stream":"stdout","data":"%s"}`, base64.StdEncoding.EncodeToString([]byte(stdoutPayload))),
		fmt.Sprintf(`{"stream":"stderr","data":"%s"}`, base64.StdEncoding.EncodeToString([]byte(stderrPayload))),
		`{"stream":"exit","code":7}`,
	}, "\n")

	proc := &stubStreamProcess{
		stdout: io.NopCloser(strings.NewReader(lines)),
		stderr: io.NopCloser(strings.NewReader("")),
	}

	stub := &stubRunner{streamProcesses: []streamProcess{proc}}
	comm := newTestCommunicator(stub)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := &packersdk.RemoteCmd{Command: "Write-Output hello", Stdout: &stdout, Stderr: &stderr}

	if err := comm.Start(context.Background(), cmd); err != nil {
		t.Fatalf("start communicator: %v", err)
	}

	exitCode := cmd.Wait()

	if exitCode != 7 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}

	if stdout.String() != stdoutPayload {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}

	if stderr.String() != stderrPayload {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}

	if len(stub.streamCalls) != 1 {
		t.Fatalf("expected one stream call, got %d", len(stub.streamCalls))
	}

	call := stub.streamCalls[0]
	if call.script != executeCommandScript {
		t.Fatalf("unexpected script used: %q", call.script)
	}

	if len(call.params) != 4 {
		t.Fatalf("unexpected param count: %d", len(call.params))
	}

	if call.params[0] != "test-vm" {
		t.Fatalf("unexpected vm parameter: %q", call.params[0])
	}
}

func TestStartCommandErrorReported(t *testing.T) {
	stderrPayload := "invoke failed\r\n"
	lines := strings.Join([]string{
		fmt.Sprintf(`{"stream":"stderr","data":"%s"}`, base64.StdEncoding.EncodeToString([]byte(stderrPayload))),
		`{"stream":"exit","code":1}`,
	}, "\n")

	proc := &stubStreamProcess{
		stdout: io.NopCloser(strings.NewReader(lines)),
		stderr: io.NopCloser(strings.NewReader("")),
	}

	stub := &stubRunner{streamProcesses: []streamProcess{proc}}
	comm := newTestCommunicator(stub)

	var stderr bytes.Buffer
	cmd := &packersdk.RemoteCmd{Command: "Write-Output hello", Stderr: &stderr}

	if err := comm.Start(context.Background(), cmd); err != nil {
		t.Fatalf("start communicator: %v", err)
	}

	exitCode := cmd.Wait()

	if exitCode != commandFailureStatus {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}

	if stderr.String() != stderrPayload {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}

	if len(stub.streamCalls) != 1 {
		t.Fatalf("expected one stream call, got %d", len(stub.streamCalls))
	}
}

func TestStartContextCanceled(t *testing.T) {
	stub := &stubRunner{}
	comm := newTestCommunicator(stub)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := comm.Start(ctx, &packersdk.RemoteCmd{Command: "noop"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}

	if len(stub.streamCalls) != 0 {
		t.Fatalf("expected no calls when context canceled")
	}
}

func TestStartNilCommand(t *testing.T) {
	comm := newTestCommunicator(&stubRunner{})
	if err := comm.Start(context.Background(), nil); err == nil {
		t.Fatalf("expected error when command is nil")
	}
}

func TestStartEmptyCommandSkipsExecution(t *testing.T) {
	stub := &stubRunner{}
	comm := newTestCommunicator(stub)

	cmd := &packersdk.RemoteCmd{Command: "   "}
	if err := comm.Start(context.Background(), cmd); err != nil {
		t.Fatalf("start communicator: %v", err)
	}

	if exit := cmd.Wait(); exit != 0 {
		t.Fatalf("expected zero exit status, got %d", exit)
	}

	if len(stub.streamCalls) != 0 {
		t.Fatalf("expected no PowerShell calls for blank command")
	}
}

func TestUploadInputRequired(t *testing.T) {
	comm := newTestCommunicator(&stubRunner{})
	if err := comm.Upload("/tmp/file", nil, nil); err == nil {
		t.Fatalf("expected error for nil input")
	}
}

func TestUploadInvokesCopyScript(t *testing.T) {
	stub := &stubRunner{}
	comm := newTestCommunicator(stub)

	source := bytes.NewBufferString("hello world")
	if err := comm.Upload("/remote/path.txt", source, nil); err != nil {
		t.Fatalf("upload: %v", err)
	}

	if len(stub.runCalls) != 1 {
		t.Fatalf("expected single PowerShell invocation, got %d", len(stub.runCalls))
	}

	call := stub.runCalls[0]
	if call.script != uploadFileScript {
		t.Fatalf("unexpected script: %q", call.script)
	}

	if len(call.params) != 5 {
		t.Fatalf("unexpected param count: %d", len(call.params))
	}

	if call.params[4] != "/remote/path.txt" {
		t.Fatalf("unexpected destination path: %q", call.params[4])
	}

	if strings.TrimSpace(call.params[3]) == "" {
		t.Fatalf("expected host temp path to be provided")
	}
}

func TestUploadDirRejectsExcludeFilters(t *testing.T) {
	comm := newTestCommunicator(&stubRunner{})
	if err := comm.UploadDir("/remote", "/local", []string{"*.tmp"}); !errors.Is(err, errUnsupportedExclude) {
		t.Fatalf("expected exclude error, got %v", err)
	}
}

func TestUploadDirRunsDirectoryScript(t *testing.T) {
	stub := &stubRunner{}
	comm := newTestCommunicator(stub)

	srcDir := t.TempDir()
	dest := "C:/remote"

	if err := comm.UploadDir(dest, srcDir, nil); err != nil {
		t.Fatalf("upload dir: %v", err)
	}

	if len(stub.runCalls) != 1 {
		t.Fatalf("expected single PowerShell invocation, got %d", len(stub.runCalls))
	}

	call := stub.runCalls[0]
	if call.script != uploadDirectoryScript {
		t.Fatalf("unexpected script: %q", call.script)
	}

	if len(call.params) != 6 {
		t.Fatalf("unexpected param count: %d", len(call.params))
	}

	if call.params[4] != dest {
		t.Fatalf("unexpected destination path: %q", call.params[4])
	}

	if call.params[5] != "true" {
		t.Fatalf("unexpected include root flag: %q", call.params[5])
	}
}

func TestDownloadOutputRequired(t *testing.T) {
	comm := newTestCommunicator(&stubRunner{})
	if err := comm.Download("/remote/file", nil); err == nil {
		t.Fatalf("expected error for nil output")
	}
}

func TestDownloadInvokesCopyScript(t *testing.T) {
	stub := &stubRunner{}
	stub.runHook = func(script string, params ...string) error {
		if script == downloadFileScript {
			return os.WriteFile(params[3], []byte("downloaded"), 0o644)
		}
		return nil
	}
	comm := newTestCommunicator(stub)

	var buf bytes.Buffer
	if err := comm.Download("/remote/path.txt", &buf); err != nil {
		t.Fatalf("download: %v", err)
	}

	if buf.String() != "downloaded" {
		t.Fatalf("unexpected downloaded contents: %q", buf.String())
	}

	if len(stub.runCalls) != 1 {
		t.Fatalf("expected single PowerShell invocation, got %d", len(stub.runCalls))
	}

	call := stub.runCalls[0]
	if call.script != downloadFileScript {
		t.Fatalf("unexpected script: %q", call.script)
	}

	if len(call.params) != 5 {
		t.Fatalf("unexpected param count: %d", len(call.params))
	}

	if call.params[4] != "/remote/path.txt" {
		t.Fatalf("unexpected remote path: %q", call.params[4])
	}
}

func TestDownloadDirRejectsExcludeFilters(t *testing.T) {
	comm := newTestCommunicator(&stubRunner{})
	if err := comm.DownloadDir("/remote", t.TempDir(), []string{"*.tmp"}); !errors.Is(err, errUnsupportedExclude) {
		t.Fatalf("expected exclude error, got %v", err)
	}
}

func TestDownloadDirRunsDirectoryScript(t *testing.T) {
	stub := &stubRunner{}
	comm := newTestCommunicator(stub)

	dst := filepath.Join(t.TempDir(), "output")

	if err := comm.DownloadDir("/remote", dst, nil); err != nil {
		t.Fatalf("download dir: %v", err)
	}

	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected destination directory to exist: %v", err)
	}

	if len(stub.runCalls) != 1 {
		t.Fatalf("expected single PowerShell invocation, got %d", len(stub.runCalls))
	}

	call := stub.runCalls[0]
	if call.script != downloadDirectoryScript {
		t.Fatalf("unexpected script: %q", call.script)
	}

	if len(call.params) != 6 {
		t.Fatalf("unexpected param count: %d", len(call.params))
	}

	if call.params[0] != "test-vm" {
		t.Fatalf("unexpected vm parameter: %q", call.params[0])
	}

	if call.params[5] != "true" {
		t.Fatalf("unexpected include root flag: %q", call.params[5])
	}
}

func TestIncludeSourceRoot(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "empty", path: "", expected: false},
		{name: "no trailing slash", path: "C:/dir", expected: true},
		{name: "windows trailing slash", path: "C:/dir/", expected: false},
		{name: "unix trailing slash", path: "/tmp/dir/", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := includeSourceRoot(tt.path); got != tt.expected {
				t.Fatalf("includeSourceRoot(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestDecodeBase64(t *testing.T) {
	data, err := decodeBase64("aGVsbG8=")
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected decoded data: %q", string(data))
	}

	empty, err := decodeBase64("")
	if err != nil {
		t.Fatalf("unexpected error decoding empty string: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected nil slice for empty input")
	}

	if _, err := decodeBase64("not-base64"); err == nil {
		t.Fatalf("expected error for invalid base64 input")
	}
}
