package benchmark

import (
	"encoding/json"
	"io"
	"os"

	"github.com/awslabs/soci-snapshotter/benchmark/framework"
	"github.com/containerd/containerd"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
)

type SociBenchmarkImage struct {
	Image *containerd.Image
	Desc  *ImageDescriptor
}

type SociBenchmarkTask struct {
	Task *framework.TaskDetails
	Desc *ImageDescriptor
}

type SociBenchmarkContainer struct {
	Container *containerd.Container
	Desc      *ImageDescriptor
}

type ImageDescriptorWithSidecars struct {
	ShortName       string                   `json:"short_name"`
	ImageRef        string                   `json:"image_ref"`
	SociIndexDigest string                   `json:"soci_index_digest"`
	ReadyLine       string                   `json:"ready_line"`
	TimeoutSec      int64                    `json:"timeout_sec"`
	ImageOptions    ImageOptionsWithSidecars `json:"options"`
}

type ImageOptionsWithSidecars struct {
	Net      string              `json:"net"`
	Mounts   []runtimespec.Mount `json:"mounts"`
	Gpu      bool                `json:"gpu"`
	Env      []string            `json:"env"`
	ShmSize  int64               `json:"shm_size"`
	Sidecars []ImageDescriptor   `json:"side_cars"`
}

func (i *ImageDescriptorWithSidecars) ToImageDescriptor() *ImageDescriptor {
	// convert to normal image descriptor object without sidecars
	// this is called after the sidecars have been processed
	return &ImageDescriptor{
		ShortName:       i.ShortName,
		ImageRef:        i.ImageRef,
		SociIndexDigest: i.SociIndexDigest,
		ReadyLine:       i.ReadyLine,
		TimeoutSec:      i.TimeoutSec,
		ImageOptions: ImageOptions{
			Net:     i.ImageOptions.Net,
			Mounts:  i.ImageOptions.Mounts,
			Gpu:     i.ImageOptions.Gpu,
			Env:     i.ImageOptions.Env,
			ShmSize: i.ImageOptions.ShmSize,
		},
	}
}

func GetImageListWithSidecars(file string) ([]ImageDescriptorWithSidecars, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return GetImageListWithSidecarsFromJSON(f)

}

func GetImageListWithSidecarsFromJSON(r io.Reader) ([]ImageDescriptorWithSidecars, error) {
	var images []ImageDescriptorWithSidecars
	err := json.NewDecoder(r).Decode(&images)
	if err != nil {
		return nil, err
	}
	return images, nil
}

func GetDefaultWorkloadsWithSidecars() []ImageDescriptorWithSidecars {
	return []ImageDescriptorWithSidecars{
		{
			ShortName:       "ECR-public-rabbitmq",
			ImageRef:        "public.ecr.aws/soci-workshop-examples/rabbitmq:latest",
			SociIndexDigest: "sha256:3882f9609c0c2da044173710f3905f4bc6c09228f2a5b5a0a5fdce2537677c17",
			ReadyLine:       "Server startup complete",
			ImageOptions: ImageOptionsWithSidecars{
				Sidecars: []ImageDescriptor{
					{
						ShortName:       "ECR-public-node",
						ImageRef:        "public.ecr.aws/soci-workshop-examples/node:latest",
						SociIndexDigest: "sha256:544d42d3447fe7833c2e798b8a342f5102022188e814de0aa6ce980e76c62894",
						ReadyLine:       "Server ready",
					},
					{
						ShortName:       "ECR-public-busybox",
						ImageRef:        "public.ecr.aws/soci-workshop-examples/busybox:latest",
						SociIndexDigest: "sha256:deaaf67bb4baa293dadcfbeb1f511c181f89a05a042ee92dd2e43e7b7295b1c0",
						ReadyLine:       "Hello World",
					},
					{
						ShortName:       "ECR-public-mongo",
						ImageRef:        "public.ecr.aws/soci-workshop-examples/mongo:latest",
						SociIndexDigest: "sha256:ecdd6dcc917d09ec7673288e8ba83270542b71959db2ac731fbeb42aa0b038e0",
						ReadyLine:       "Waiting for connections",
					},
				},
			},
		},
	}
}
