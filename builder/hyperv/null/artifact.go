package null

import (
	"fmt"

	hypervcommon "github.com/hashicorp/packer-plugin-hyperv/builder/hyperv/common"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// artifact represents a no-op builder result that simply reuses an existing VM.
type artifact struct {
	vmName string
}

func newArtifact(vmName string) packersdk.Artifact {
	return &artifact{vmName: vmName}
}

func (a *artifact) BuilderId() string {
	return hypervcommon.BuilderId
}

func (a *artifact) Files() []string {
	return nil
}

func (a *artifact) Id() string {
	return a.vmName
}

func (a *artifact) String() string {
	return fmt.Sprintf("Existing Hyper-V VM: %s", a.vmName)
}

func (a *artifact) State(string) interface{} {
	return nil
}

func (a *artifact) Destroy() error {
	return nil
}
