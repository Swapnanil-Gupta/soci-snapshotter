/*
   Copyright The Soci Snapshotter Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package benchmark

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/awslabs/soci-snapshotter/benchmark/framework"
	"github.com/awslabs/soci-snapshotter/benchmark/framework/kerneltrace"
	"github.com/containerd/containerd"
	"github.com/containerd/log"
	"github.com/google/uuid"
)

var (
	outputDir              = "./output"
	kernelTraceScriptPath  = "../kernel_trace.py"
	kernelTraceOutDir      = outputDir + "/kernel_trace_script_out"
	containerdAddress      = "/tmp/containerd-grpc/containerd.sock"
	containerdRoot         = "/tmp/lib/containerd"
	containerdState        = "/tmp/containerd"
	containerdSociConfig   = "../containerd_soci_config.toml"
	containerdStargzConfig = "../containerd_stargz_config.toml"
	platform               = "linux/amd64"
	sociBinary             = "../../out/soci-snapshotter-grpc"
	sociAddress            = "/tmp/soci-snapshotter-grpc/soci-snapshotter-grpc.sock"
	sociRoot               = "/tmp/lib/soci-snapshotter-grpc"
	sociConfig             = "../soci_config.toml"
	sociFastPullConfig     = "../soci_fastpull_config.toml"
	stargzAddress          = "/tmp/containerd-stargz-grpc/containerd-stargz-grpc.sock"
	stargzConfig           = "../stargz_config.toml"
	stargzRoot             = "/tmp/lib/containerd-stargz-grpc"
)

// fatalf prints a formatted fatal error mesage to both stdout and a testing.B.
// When running benchmarks with `go test -bench=.`, the `testing.B`'s output `io.Writer`
// is set to `os.Stdout`. When calling `testing.Benchmark` directly, the `testing.B`'s
// output `io.Writer` is set to discard, so all messages are lost. This manually writes
// to stdout to work around the issue.
//
// see: https://github.com/golang/go/issues/32066
func fatalf(b *testing.B, message string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, message, args...)
	b.Fatalf(message, args...)
}

func SociFastPullFullRun(
	ctx context.Context,
	b *testing.B,
	testName string,
	imageDescriptor ImageDescriptor) {
	if kerneltrace.IsEnabled() {
		log.G(ctx).Info("Starting kernel trace")
		kerneltrace.IncRunNum()
		stopKernelTrace, err := kerneltrace.Start(kernelTraceScriptPath, containerdState, kernelTraceOutDir, testName)
		if err != nil {
			fatalf(b, "Failed to start kernel trace: %v\n", err)
		}
		log.G(ctx).Info("Started kernel trace")

		defer func() {
			log.G(ctx).Info("Stopping kernel trace")
			if err := stopKernelTrace(); err != nil {
				fatalf(b, "Failed to stop kernel trace: %v\n", err)
			}
			log.G(ctx).Info("Stopped kernel trace")
			time.Sleep(5 * time.Second)
		}()

		log.G(ctx).Info("Sleeping for 5 seconds after starting kernel trace...")
		time.Sleep(5 * time.Second)
		log.G(ctx).Info("Continuing...")
	} else {
		log.G(ctx).Info("Kernel trace disabled")
	}

	testUUID := uuid.New().String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("test_name", testName))
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("uuid", testUUID))
	containerdProcess, err := getContainerdProcess(ctx, containerdSociConfig)
	if err != nil {
		fatalf(b, "Failed to create containerd proc: %v\n", err)
	}
	defer containerdProcess.StopProcess()
	sociProcess, err := getSociProcess(sociFastPullConfig)
	if err != nil {
		fatalf(b, "Failed to create soci proc: %v\n", err)
	}
	defer sociProcess.StopProcess()
	sociContainerdProc := SociContainerdProcess{containerdProcess}
	b.ResetTimer()
	pullStart := time.Now()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Start").Infof("Start Test")
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Start").Infof("Start Pull Image")
	image, err := sociContainerdProc.SociRPullImageFromRegistry(ctx, imageDescriptor.ImageRef, "", true)
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Stop").Infof("Stop Pull Image")
	pullDuration := time.Since(pullStart)
	b.ReportMetric(float64(pullDuration.Milliseconds()), "pullDuration")
	b.ReportMetric(0, "unpackDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}

	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Start").Infof("Start Unpack Image")
	unpackStart := time.Now()
	err = image.Unpack(ctx, "soci")
	unpackDuration := time.Since(unpackStart)
	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Stop").Infof("Stop Unpack Image")
	b.ReportMetric(float64(unpackDuration.Milliseconds()), "unpackDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}

	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Start").Infof("Start Create Container")
	container, cleanupContainer, err := sociContainerdProc.CreateSociContainer(ctx, image, imageDescriptor)
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Stop").Infof("Stop Create Container")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainer()

	if kerneltrace.IsEnabled() {
		log.G(ctx).Infof("Reporting containerd id to kernel trace %s", container.ID())
		kerneltrace.ReportContainerId(container.ID())
	}

	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Start").Infof("Start Create Task")
	taskDetails, cleanupTask, err := sociContainerdProc.CreateTask(ctx, container)
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Stop").Infof("Stop Create Task")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTask()
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Start").Infof("Start Run Task")
	runLazyTaskStart := time.Now()
	cleanupRun, err := sociContainerdProc.RunContainerTaskForReadyLine(ctx, taskDetails, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	lazyTaskDuration := time.Since(runLazyTaskStart)
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Stop").Infof("Stop Run Task")
	b.ReportMetric(float64(lazyTaskDuration.Milliseconds()), "lazyTaskDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}
	// In order for host networking to work, we need to clean up the task so that any network resources are released before running the second container
	// We don't want this cleanup time included in the benchmark, though.
	b.StopTimer()
	cleanupRun()
	b.StartTimer()
	containerSecondRun, cleanupContainerSecondRun, err := sociContainerdProc.CreateSociContainer(ctx, image, imageDescriptor)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainerSecondRun()

	if kerneltrace.IsEnabled() {
		log.G(ctx).Infof("Reporting containerd id to kernel trace %s", containerSecondRun.ID())
		kerneltrace.ReportContainerId(containerSecondRun.ID())
	}

	taskDetailsSecondRun, cleanupTaskSecondRun, err := sociContainerdProc.CreateTask(ctx, containerSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTaskSecondRun()
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Start").Infof("Start Run Task Twice")
	runLocalStart := time.Now()
	cleanupRunSecond, err := sociContainerdProc.RunContainerTaskForReadyLine(ctx, taskDetailsSecondRun, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	localTaskStats := time.Since(runLocalStart)
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Stop").Infof("Stop Run Task Twice")
	b.ReportMetric(float64(localTaskStats.Milliseconds()), "localTaskStats")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupRunSecond()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Stop").Infof("Stop Test")
	b.StopTimer()
}

func SociFullRun(
	ctx context.Context,
	b *testing.B,
	testName string,
	imageDescriptor ImageDescriptor) {
	if kerneltrace.IsEnabled() {
		log.G(ctx).Info("Starting kernel trace")
		kerneltrace.IncRunNum()
		stopKernelTrace, err := kerneltrace.Start(kernelTraceScriptPath, containerdState, kernelTraceOutDir, testName)
		if err != nil {
			fatalf(b, "Failed to start kernel trace: %v\n", err)
		}
		log.G(ctx).Info("Started kernel trace")

		defer func() {
			log.G(ctx).Info("Stopping kernel trace")
			if err := stopKernelTrace(); err != nil {
				fatalf(b, "Failed to stop kernel trace: %v\n", err)
			}
			log.G(ctx).Info("Stopped kernel trace")
			time.Sleep(5 * time.Second)
		}()

		log.G(ctx).Info("Sleeping for 5 seconds after starting kernel trace...")
		time.Sleep(5 * time.Second)
		log.G(ctx).Info("Continuing...")
	} else {
		log.G(ctx).Info("Kernel trace disabled")
	}

	testUUID := uuid.New().String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("test_name", testName))
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("uuid", testUUID))
	containerdProcess, err := getContainerdProcess(ctx, containerdSociConfig)
	if err != nil {
		fatalf(b, "Failed to create containerd proc: %v\n", err)
	}
	defer containerdProcess.StopProcess()
	sociProcess, err := getSociProcess(sociConfig)
	if err != nil {
		fatalf(b, "Failed to create soci proc: %v\n", err)
	}
	defer sociProcess.StopProcess()
	sociContainerdProc := SociContainerdProcess{containerdProcess}
	b.ResetTimer()
	pullStart := time.Now()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Start").Infof("Start Test")
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Start").Infof("Start Pull Image")
	image, err := sociContainerdProc.SociRPullImageFromRegistry(ctx, imageDescriptor.ImageRef, imageDescriptor.SociIndexDigest, false)
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Stop").Infof("Stop Pull Image")
	pullDuration := time.Since(pullStart)
	b.ReportMetric(float64(pullDuration.Milliseconds()), "pullDuration")
	b.ReportMetric(0, "unpackDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Start").Infof("Start Create Container")
	container, cleanupContainer, err := sociContainerdProc.CreateSociContainer(ctx, image, imageDescriptor)
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Stop").Infof("Stop Create Container")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainer()

	if kerneltrace.IsEnabled() {
		log.G(ctx).Infof("Reporting containerd id to kernel trace %s", container.ID())
		kerneltrace.ReportContainerId(container.ID())
	}

	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Start").Infof("Start Create Task")
	taskDetails, cleanupTask, err := sociContainerdProc.CreateTask(ctx, container)
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Stop").Infof("Stop Create Task")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTask()
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Start").Infof("Start Run Task")
	runLazyTaskStart := time.Now()
	cleanupRun, err := sociContainerdProc.RunContainerTaskForReadyLine(ctx, taskDetails, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	lazyTaskDuration := time.Since(runLazyTaskStart)
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Stop").Infof("Stop Run Task")
	b.ReportMetric(float64(lazyTaskDuration.Milliseconds()), "lazyTaskDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}
	// In order for host networking to work, we need to clean up the task so that any network resources are released before running the second container
	// We don't want this cleanup time included in the benchmark, though.
	b.StopTimer()
	cleanupRun()
	b.StartTimer()
	containerSecondRun, cleanupContainerSecondRun, err := sociContainerdProc.CreateSociContainer(ctx, image, imageDescriptor)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainerSecondRun()

	if kerneltrace.IsEnabled() {
		log.G(ctx).Infof("Reporting containerd id to kernel trace %s", containerSecondRun.ID())
		kerneltrace.ReportContainerId(containerSecondRun.ID())
	}

	taskDetailsSecondRun, cleanupTaskSecondRun, err := sociContainerdProc.CreateTask(ctx, containerSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTaskSecondRun()
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Start").Infof("Start Run Task Twice")
	runLocalStart := time.Now()
	cleanupRunSecond, err := sociContainerdProc.RunContainerTaskForReadyLine(ctx, taskDetailsSecondRun, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	localTaskStats := time.Since(runLocalStart)
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Stop").Infof("Stop Run Task Twice")
	b.ReportMetric(float64(localTaskStats.Milliseconds()), "localTaskStats")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupRunSecond()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Stop").Infof("Stop Test")
	b.StopTimer()
}

func OverlayFSFullRun(
	ctx context.Context,
	b *testing.B,
	testName string,
	imageDescriptor ImageDescriptor) {
	if kerneltrace.IsEnabled() {
		log.G(ctx).Info("Starting kernel trace")
		kerneltrace.IncRunNum()
		stopKernelTrace, err := kerneltrace.Start(kernelTraceScriptPath, containerdState, kernelTraceOutDir, testName)
		if err != nil {
			fatalf(b, "Failed to start kernel trace: %v\n", err)
		}
		log.G(ctx).Info("Started kernel trace")

		defer func() {
			log.G(ctx).Info("Stopping kernel trace")
			if err := stopKernelTrace(); err != nil {
				fatalf(b, "Failed to stop kernel trace: %v\n", err)
			}
			log.G(ctx).Info("Stopped kernel trace")
			time.Sleep(5 * time.Second)
		}()

		log.G(ctx).Info("Sleeping for 5 seconds after starting kernel trace...")
		time.Sleep(5 * time.Second)
		log.G(ctx).Info("Continuing...")
	} else {
		log.G(ctx).Info("Kernel trace disabled")
	}

	testUUID := uuid.New().String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("test_name", testName))
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("uuid", testUUID))
	containerdProcess, err := getContainerdProcess(ctx, containerdSociConfig)
	if err != nil {
		fatalf(b, "Failed to create containerd proc: %v\n", err)
	}
	defer containerdProcess.StopProcess()
	b.ResetTimer()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Start").Infof("Start Test")
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Start").Infof("Start Pull Image")
	pullStart := time.Now()
	image, err := containerdProcess.PullImageFromRegistry(ctx, imageDescriptor.ImageRef, platform)
	pullDuration := time.Since(pullStart)
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Stop").Infof("Stop Pull Image")
	b.ReportMetric(float64(pullDuration.Milliseconds()), "pullDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}
	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Start").Infof("Start Unpack Image")
	unpackStart := time.Now()
	err = image.Unpack(ctx, "overlayfs")
	unpackDuration := time.Since(unpackStart)
	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Stop").Infof("Stop Unpack Image")
	b.ReportMetric(float64(unpackDuration.Milliseconds()), "unpackDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Start").Infof("Start Create Container")
	container, cleanupContainer, err := containerdProcess.CreateContainer(ctx, imageDescriptor.ContainerOpts(image)...)
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Stop").Infof("Stop Create Container")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainer()

	if kerneltrace.IsEnabled() {
		log.G(ctx).Infof("Reporting containerd id to kernel trace %s", container.ID())
		kerneltrace.ReportContainerId(container.ID())
	}

	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Start").Infof("Start Create Task")
	taskDetails, cleanupTask, err := containerdProcess.CreateTask(ctx, container)
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Stop").Infof("Stop Create Task")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTask()
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Start").Infof("Start Run Task")
	runLazyTaskStart := time.Now()
	cleanupRun, err := containerdProcess.RunContainerTaskForReadyLine(ctx, taskDetails, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	lazyTaskDuration := time.Since(runLazyTaskStart)
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Stop").Infof("Stop Run Task")
	b.ReportMetric(float64(lazyTaskDuration.Milliseconds()), "lazyTaskDuration")
	if err != nil {
		fatalf(b, "%s", err)
	}
	// In order for host networking to work, we need to clean up the task so that any network resources are released before running the second container
	// We don't want this cleanup time included in the benchmark, though.
	b.StopTimer()
	cleanupRun()
	b.StartTimer()
	containerSecondRun, cleanupContainerSecondRun, err := containerdProcess.CreateContainer(ctx, imageDescriptor.ContainerOpts(image)...)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainerSecondRun()

	if kerneltrace.IsEnabled() {
		log.G(ctx).Infof("Reporting containerd id to kernel trace %s", containerSecondRun.ID())
		kerneltrace.ReportContainerId(containerSecondRun.ID())
	}

	taskDetailsSecondRun, cleanupTaskSecondRun, err := containerdProcess.CreateTask(ctx, containerSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTaskSecondRun()
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Start").Infof("Start Run Task Twice")
	runLocalStart := time.Now()
	cleanupRunSecond, err := containerdProcess.RunContainerTaskForReadyLine(ctx, taskDetailsSecondRun, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	localTaskStats := time.Since(runLocalStart)
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Stop").Infof("Stop Run Task Twice")
	b.ReportMetric(float64(localTaskStats.Milliseconds()), "localTaskStats")
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupRunSecond()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Stop").Infof("Stop Test")
	b.StopTimer()
}

func StargzFullRun(
	ctx context.Context,
	b *testing.B,
	imageDescriptor ImageDescriptor,
	stargzBinary string) {
	containerdProcess, err := getContainerdProcess(ctx, containerdStargzConfig)
	if err != nil {
		fatalf(b, "Failed to create containerd proc: %v\n", err)
	}
	defer containerdProcess.StopProcess()
	stargzProcess, err := getStargzProcess(stargzBinary)
	if err != nil {
		fatalf(b, "Failed to create stargz proc: %v\n", err)
	}
	defer stargzProcess.StopProcess()
	stargzContainerdProc := StargzContainerdProcess{containerdProcess}
	b.ResetTimer()
	image, err := stargzContainerdProc.StargzRpullImageFromRegistry(ctx, imageDescriptor.ImageRef)
	if err != nil {
		fatalf(b, "%s", err)
	}
	container, cleanupContainer, err := stargzContainerdProc.CreateContainer(ctx, imageDescriptor.ContainerOpts(image, containerd.WithSnapshotter("stargz"))...)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupContainer()
	taskDetails, cleanupTask, err := containerdProcess.CreateTask(ctx, container)
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupTask()
	cleanupRun, err := containerdProcess.RunContainerTaskForReadyLine(ctx, taskDetails, imageDescriptor.ReadyLine, imageDescriptor.Timeout())
	if err != nil {
		fatalf(b, "%s", err)
	}
	defer cleanupRun()
	b.StopTimer()
}

func getContainerdProcess(ctx context.Context, containerdConfig string) (*framework.ContainerdProcess, error) {
	return framework.StartContainerd(
		containerdAddress,
		containerdRoot,
		containerdState,
		containerdConfig,
		outputDir)
}

func getSociProcess(sociConfig string) (*SociProcess, error) {
	return StartSoci(
		sociBinary,
		sociAddress,
		sociRoot,
		containerdAddress,
		sociConfig,
		outputDir)
}

func getStargzProcess(stargzBinary string) (*StargzProcess, error) {
	return StartStargz(
		stargzBinary,
		stargzAddress,
		stargzConfig,
		stargzRoot,
		outputDir)
}
