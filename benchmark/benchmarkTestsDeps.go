package benchmark

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/awslabs/soci-snapshotter/benchmark/framework"
	"github.com/awslabs/soci-snapshotter/benchmark/framework/cpumemtrace"
	"github.com/containerd/log"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

func SociFastPullFullRunWithSidecars(
	ctx context.Context,
	b *testing.B,
	testName string,
	testNum int,
	imageDescriptor ImageDescriptorWithSidecars,
	traceCpuMemUsage bool,
	cpuMemTraceIntervalMs int,
) {
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

	// cpu and memory usage trace first task
	var stopFirstCpuMemTrace func() error
	if traceCpuMemUsage {
		log.G(ctx).Info("starting first cpu and memory trace")
		stopFirstCpuMemTrace, err = cpumemtrace.Start(
			testName,
			testNum,
			cpumemtrace.FirstTask,
			cpuMemTraceOutDir,
			cpuMemTraceIntervalMs,
		)
		if err != nil {
			fatalf(b, "failed to start first cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("started first cpu and memory trace")
	} else {
		log.G(ctx).Info("cpu and memory trace is disabled")
	}

	b.ResetTimer()
	pullStart := time.Now()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Start").Infof("Start Test")
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Start").Infof("Start Pull Image")
	images, err := pullSociImageWithSidecars(ctx, sociContainerdProc, imageDescriptor, true)
	if err != nil {
		fatalf(b, "%s", err)
	}
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Stop").Infof("Stop Pull Image")
	pullDuration := time.Since(pullStart)
	b.ReportMetric(float64(pullDuration.Milliseconds()), "pullDuration")

	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Start").Infof("Start Unpack Image")
	unpackStart := time.Now()
	for _, image := range images {
		err = (*image.Image).Unpack(ctx, "soci")
		if err != nil {
			fatalf(b, "%s", err)
		}
	}
	unpackDuration := time.Since(unpackStart)
	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Stop").Infof("Stop Unpack Image")
	b.ReportMetric(float64(unpackDuration.Milliseconds()), "unpackDuration")

	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Start").Infof("Start Create Container")
	sociContainers, cleanupContainers, err := createSociContainersFromImages(ctx, sociContainerdProc, images)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupContainer := range cleanupContainers {
		defer cleanupContainer()
	}
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Stop").Infof("Stop Create Container")

	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Start").Infof("Start Create Task")
	tasks, cleanUpTasks, err := createSociTasksFromContainers(ctx, sociContainerdProc, sociContainers)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupTask := range cleanUpTasks {
		defer cleanupTask()
	}
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Stop").Infof("Stop Create Task")

	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Start").Infof("Start Run Task")
	runLazyTaskStart := time.Now()
	cleanupRuns, err := runSociTasksWithSidecars(ctx, sociContainerdProc, tasks)
	if err != nil {
		fatalf(b, "%s", err)
	}
	lazyTaskDuration := time.Since(runLazyTaskStart)
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Stop").Infof("Stop Run Task")
	b.ReportMetric(float64(lazyTaskDuration.Milliseconds()), "lazyTaskDuration")
	// In order for host networking to work, we need to clean up the task so that any network resources are released before running the second container
	// We don't want this cleanup time included in the benchmark, though.
	b.StopTimer()
	for _, cleanupRun := range cleanupRuns {
		cleanupRun()
	}

	// stop first cpu/mem trace
	if traceCpuMemUsage && stopFirstCpuMemTrace != nil {
		log.G(ctx).Info("stopping first cpu and memory trace")
		if err := stopFirstCpuMemTrace(); err != nil {
			fatalf(b, "failed to stop first cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("stopped first cpu and memory trace")
	}

	// cpu mem trace second task
	if traceCpuMemUsage {
		log.G(ctx).Info("starting second cpu and memory trace")
		stopSecondCpuMemTrace, err := cpumemtrace.Start(
			testName,
			testNum,
			cpumemtrace.SecondTask,
			cpuMemTraceOutDir,
			cpuMemTraceIntervalMs,
		)
		if err != nil {
			fatalf(b, "failed to start second cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("started second cpu and memory trace")
		defer func() {
			log.G(ctx).Info("stopping second cpu and memory trace")
			if err := stopSecondCpuMemTrace(); err != nil {
				fatalf(b, "failed to stop second cpu and memory trace: %v\n", err)
			}
			log.G(ctx).Info("stopped second cpu and memory trace")
		}()
	}

	b.StartTimer()
	containersSecondRun, cleanupContainersSecondRun, err := createSociContainersFromImages(ctx, sociContainerdProc, images)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupContainerSecondRun := range cleanupContainersSecondRun {
		defer cleanupContainerSecondRun()
	}

	tasksSecondRun, cleanupTasksSecondRun, err := createSociTasksFromContainers(ctx, sociContainerdProc, containersSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupTaskSecondRun := range cleanupTasksSecondRun {
		defer cleanupTaskSecondRun()
	}

	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Start").Infof("Start Run Task Twice")
	runLocalStart := time.Now()
	cleanupRunsSecond, err := runSociTasksWithSidecars(ctx, sociContainerdProc, tasksSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupRunSecond := range cleanupRunsSecond {
		defer cleanupRunSecond()
	}
	localTaskStats := time.Since(runLocalStart)
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Stop").Infof("Stop Run Task Twice")
	b.ReportMetric(float64(localTaskStats.Milliseconds()), "localTaskStats")
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Stop").Infof("Stop Test")
	b.StopTimer()
}

func SociFullRunWithSidecars(
	ctx context.Context,
	b *testing.B,
	testName string,
	testNum int,
	imageDescriptor ImageDescriptorWithSidecars,
	traceCpuMemUsage bool,
	cpuMemTraceIntervalMs int,
) {
	testUUID := uuid.New().String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("test_name", testName))
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("uuid", testUUID))

	// get containerd process
	containerdProcess, err := getContainerdProcess(ctx, containerdSociConfig)
	if err != nil {
		fatalf(b, "Failed to create containerd proc: %v\n", err)
	}
	defer containerdProcess.StopProcess()

	// get soci process
	sociProcess, err := getSociProcess(sociConfig)
	if err != nil {
		fatalf(b, "Failed to create soci proc: %v\n", err)
	}
	defer sociProcess.StopProcess()
	sociContainerdProc := SociContainerdProcess{containerdProcess}

	// cpu and memory usage trace first task
	var stopFirstCpuMemTrace func() error
	if traceCpuMemUsage {
		log.G(ctx).Info("starting first cpu and memory trace")
		stopFirstCpuMemTrace, err = cpumemtrace.Start(
			testName,
			testNum,
			cpumemtrace.FirstTask,
			cpuMemTraceOutDir,
			cpuMemTraceIntervalMs,
		)
		if err != nil {
			fatalf(b, "failed to start first cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("started first cpu and memory trace")
	} else {
		log.G(ctx).Info("cpu and memory trace is disabled")
	}

	// pull image and sidecars parallelly
	b.ResetTimer()
	pullStart := time.Now()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Start").Infof("Start Test")
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Start").Infof("Start Pull Image")
	images, err := pullSociImageWithSidecars(ctx, sociContainerdProc, imageDescriptor, false)
	if err != nil {
		fatalf(b, "%s", err)
	}
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Stop").Infof("Stop Pull Image")
	pullDuration := time.Since(pullStart)
	b.ReportMetric(float64(pullDuration.Milliseconds()), "pullDuration")

	// no unpack for soci
	b.ReportMetric(0, "unpackDuration")

	// create containers
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Start").Infof("Start Create Container")
	sociContainers, cleanupContainers, err := createSociContainersFromImages(ctx, sociContainerdProc, images)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupContainer := range cleanupContainers {
		defer cleanupContainer()
	}
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Stop").Infof("Stop Create Container")

	// create tasks
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Start").Infof("Start Create Task")
	tasks, cleanUpTasks, err := createSociTasksFromContainers(ctx, sociContainerdProc, sociContainers)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupTask := range cleanUpTasks {
		defer cleanupTask()
	}
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Stop").Infof("Stop Create Task")

	// run tasks
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Start").Infof("Start Run Task")
	runLazyTaskStart := time.Now()
	cleanupRuns, err := runSociTasksWithSidecars(ctx, sociContainerdProc, tasks)
	if err != nil {
		fatalf(b, "%s", err)
	}
	lazyTaskDuration := time.Since(runLazyTaskStart)
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Stop").Infof("Stop Run Task")
	b.ReportMetric(float64(lazyTaskDuration.Milliseconds()), "lazyTaskDuration")
	// In order for host networking to work, we need to clean up the task so that any network resources are released before running the second container
	// We don't want this cleanup time included in the benchmark, though.
	b.StopTimer()
	for _, cleanupRun := range cleanupRuns {
		cleanupRun()
	}

	// stop first cpu/mem trace
	if traceCpuMemUsage && stopFirstCpuMemTrace != nil {
		log.G(ctx).Info("stopping first cpu and memory trace")
		if err := stopFirstCpuMemTrace(); err != nil {
			fatalf(b, "failed to stop first cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("stopped first cpu and memory trace")
	}

	// cpu mem trace second task
	if traceCpuMemUsage {
		log.G(ctx).Info("starting second cpu and memory trace")
		stopSecondCpuMemTrace, err := cpumemtrace.Start(
			testName,
			testNum,
			cpumemtrace.SecondTask,
			cpuMemTraceOutDir,
			cpuMemTraceIntervalMs,
		)
		if err != nil {
			fatalf(b, "failed to start second cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("started second cpu and memory trace")
		defer func() {
			log.G(ctx).Info("stopping second cpu and memory trace")
			if err := stopSecondCpuMemTrace(); err != nil {
				fatalf(b, "failed to stop second cpu and memory trace: %v\n", err)
			}
			log.G(ctx).Info("stopped second cpu and memory trace")
		}()
	}

	// create 2nd set of containers
	b.StartTimer()
	containersSecondRun, cleanupContainersSecondRun, err := createSociContainersFromImages(ctx, sociContainerdProc, images)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupContainerSecondRun := range cleanupContainersSecondRun {
		defer cleanupContainerSecondRun()
	}

	// create 2nd set of tasks
	tasksSecondRun, cleanupTasksSecondRun, err := createSociTasksFromContainers(ctx, sociContainerdProc, containersSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupTaskSecondRun := range cleanupTasksSecondRun {
		defer cleanupTaskSecondRun()
	}

	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Start").Infof("Start Run Task Twice")
	runLocalStart := time.Now()
	cleanupRunsSecond, err := runSociTasksWithSidecars(ctx, sociContainerdProc, tasksSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupRunSecond := range cleanupRunsSecond {
		defer cleanupRunSecond()
	}
	localTaskStats := time.Since(runLocalStart)
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Stop").Infof("Stop Run Task Twice")
	b.ReportMetric(float64(localTaskStats.Milliseconds()), "localTaskStats")
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Stop").Infof("Stop Test")
	b.StopTimer()
}

func OverlayFSFullRunWithSidecars(
	ctx context.Context,
	b *testing.B,
	testName string,
	testNum int,
	imageDescriptor ImageDescriptorWithSidecars,
	traceCpuMemUsage bool,
	cpuMemTraceIntervalMs int,
) {
	testUUID := uuid.New().String()
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("test_name", testName))
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("uuid", testUUID))

	containerdProcess, err := getContainerdProcess(ctx, containerdSociConfig)
	if err != nil {
		fatalf(b, "Failed to create containerd proc: %v\n", err)
	}
	defer containerdProcess.StopProcess()

	// cpu and memory usage trace first task
	var stopFirstCpuMemTrace func() error
	if traceCpuMemUsage {
		log.G(ctx).Info("starting first cpu and memory trace")
		stopFirstCpuMemTrace, err = cpumemtrace.Start(
			testName,
			testNum,
			cpumemtrace.FirstTask,
			cpuMemTraceOutDir,
			cpuMemTraceIntervalMs,
		)
		if err != nil {
			fatalf(b, "failed to start first cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("started first cpu and memory trace")
	} else {
		log.G(ctx).Info("cpu and memory trace is disabled")
	}

	b.ResetTimer()
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Start").Infof("Start Test")
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Start").Infof("Start Pull Image")
	pullStart := time.Now()
	images, err := pullImageWithSidecars(ctx, containerdProcess, imageDescriptor, platform)
	if err != nil {
		fatalf(b, "%s", err)
	}
	pullDuration := time.Since(pullStart)
	log.G(ctx).WithField("benchmark", "Pull").WithField("event", "Stop").Infof("Stop Pull Image")
	b.ReportMetric(float64(pullDuration.Milliseconds()), "pullDuration")

	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Start").Infof("Start Unpack Image")
	unpackStart := time.Now()
	for _, image := range images {
		err = (*image.Image).Unpack(ctx, "overlayfs")
		if err != nil {
			fatalf(b, "%s", err)
		}
	}
	unpackDuration := time.Since(unpackStart)
	log.G(ctx).WithField("benchmark", "Unpack").WithField("event", "Stop").Infof("Stop Unpack Image")
	b.ReportMetric(float64(unpackDuration.Milliseconds()), "unpackDuration")

	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Start").Infof("Start Create Container")
	containers, cleanupContainers, err := createContainersFromImages(ctx, containerdProcess, images)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupContainer := range cleanupContainers {
		defer cleanupContainer()
	}
	log.G(ctx).WithField("benchmark", "CreateContainer").WithField("event", "Stop").Infof("Stop Create Container")

	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Start").Infof("Start Create Task")
	tasks, cleanUpTasks, err := createTasksFromContainers(ctx, containerdProcess, containers)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupTask := range cleanUpTasks {
		defer cleanupTask()
	}
	log.G(ctx).WithField("benchmark", "CreateTask").WithField("event", "Stop").Infof("Stop Create Task")

	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Start").Infof("Start Run Task")
	runLazyTaskStart := time.Now()
	cleanupRuns, err := runTasksWithSidecars(ctx, containerdProcess, tasks)
	if err != nil {
		fatalf(b, "%s", err)
	}
	lazyTaskDuration := time.Since(runLazyTaskStart)
	log.G(ctx).WithField("benchmark", "RunTask").WithField("event", "Stop").Infof("Stop Run Task")
	b.ReportMetric(float64(lazyTaskDuration.Milliseconds()), "lazyTaskDuration")
	// In order for host networking to work, we need to clean up the task so that any network resources are released before running the second container
	// We don't want this cleanup time included in the benchmark, though.
	b.StopTimer()
	for _, cleanupRun := range cleanupRuns {
		cleanupRun()
	}

	// stop first cpu/mem trace
	if traceCpuMemUsage && stopFirstCpuMemTrace != nil {
		log.G(ctx).Info("stopping first cpu and memory trace")
		if err := stopFirstCpuMemTrace(); err != nil {
			fatalf(b, "failed to stop first cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("stopped first cpu and memory trace")
	}

	// cpu mem trace second task
	if traceCpuMemUsage {
		log.G(ctx).Info("starting second cpu and memory trace")
		stopSecondCpuMemTrace, err := cpumemtrace.Start(
			testName,
			testNum,
			cpumemtrace.SecondTask,
			cpuMemTraceOutDir,
			cpuMemTraceIntervalMs,
		)
		if err != nil {
			fatalf(b, "failed to start second cpu and memory trace: %v\n", err)
		}
		log.G(ctx).Info("started second cpu and memory trace")
		defer func() {
			log.G(ctx).Info("stopping second cpu and memory trace")
			if err := stopSecondCpuMemTrace(); err != nil {
				fatalf(b, "failed to stop second cpu and memory trace: %v\n", err)
			}
			log.G(ctx).Info("stopped second cpu and memory trace")
		}()
	}

	b.StartTimer()
	containerSecondRun, cleanupContainerSecondRun, err := createContainersFromImages(ctx, containerdProcess, images)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupContainerSecondRun := range cleanupContainerSecondRun {
		defer cleanupContainerSecondRun()
	}

	tasksSecondRun, cleanupTaskSecondRun, err := createTasksFromContainers(ctx, containerdProcess, containerSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupTaskSecondRun := range cleanupTaskSecondRun {
		defer cleanupTaskSecondRun()
	}

	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Start").Infof("Start Run Task Twice")
	runLocalStart := time.Now()
	cleanupRunsSecond, err := runTasksWithSidecars(ctx, containerdProcess, tasksSecondRun)
	if err != nil {
		fatalf(b, "%s", err)
	}
	for _, cleanupRunSecond := range cleanupRunsSecond {
		defer cleanupRunSecond()
	}
	localTaskStats := time.Since(runLocalStart)
	log.G(ctx).WithField("benchmark", "RunTaskTwice").WithField("event", "Stop").Infof("Stop Run Task Twice")
	b.ReportMetric(float64(localTaskStats.Milliseconds()), "localTaskStats")
	log.G(ctx).WithField("benchmark", "Test").WithField("event", "Stop").Infof("Stop Test")
	b.StopTimer()
}

func pullSociImageWithSidecars(
	ctx context.Context,
	sociContainerdProc SociContainerdProcess,
	imageDescriptor ImageDescriptorWithSidecars,
	fastPull bool,
) ([]SociBenchmarkImage, error) {
	// eg, ctx := errgroup.WithContext(ctx)
	var eg errgroup.Group
	lenSidecars := len(imageDescriptor.ImageOptions.Sidecars)
	imageChan := make(chan SociBenchmarkImage, lenSidecars+1) // +1 for the main image
	// pull sidecars
	for _, sideImageDesc := range imageDescriptor.ImageOptions.Sidecars {
		eg.Go(func() error {
			fmt.Println("pulling sidecar ====>", sideImageDesc.ImageRef)
			image, err := sociContainerdProc.SociRPullImageFromRegistry(
				ctx,
				sideImageDesc.ImageRef,
				sideImageDesc.SociIndexDigest,
				fastPull,
			)
			if err != nil {
				return err
			}
			fmt.Println("done pulling sidecar ====>", sideImageDesc.ImageRef)
			imageChan <- SociBenchmarkImage{Image: &image, Desc: &sideImageDesc}
			return nil
		})
	}
	// pull main image
	eg.Go(func() error {
		fmt.Println("pulling image ====>", imageDescriptor.ImageRef)
		image, err := sociContainerdProc.SociRPullImageFromRegistry(
			ctx,
			imageDescriptor.ImageRef,
			imageDescriptor.SociIndexDigest,
			fastPull,
		)
		if err != nil {
			return err
		}
		fmt.Println("done pulling image ====>", imageDescriptor.ImageRef)
		imageChan <- SociBenchmarkImage{Image: &image, Desc: imageDescriptor.ToImageDescriptor()}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(imageChan)

	images := make([]SociBenchmarkImage, 0, lenSidecars+1)
	for image := range imageChan {
		images = append(images, image)
	}
	return images, nil
}

func runSociTasksWithSidecars(
	ctx context.Context,
	sociContainerdProc SociContainerdProcess,
	tasks []SociBenchmarkTask,
) ([]func(), error) {
	// eg, ctx := errgroup.WithContext(ctx)
	var eg errgroup.Group
	cleanUpChan := make(chan func(), len(tasks))
	for _, task := range tasks {
		eg.Go(func() error {
			fmt.Printf("running %s till readyline ====> %s\n", task.Desc.ShortName, task.Desc.ReadyLine)
			cleanupRun, err := sociContainerdProc.RunContainerTaskForReadyLine(
				ctx,
				task.Task,
				task.Desc.ReadyLine,
				task.Desc.Timeout(),
			)
			if err != nil {
				return err
			}
			cleanUpChan <- cleanupRun
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(cleanUpChan)

	cleanUps := make([]func(), 0, len(tasks))
	for cleanup := range cleanUpChan {
		cleanUps = append(cleanUps, cleanup)
	}
	return cleanUps, nil
}

func createSociContainersFromImages(
	ctx context.Context,
	sociContainerdProc SociContainerdProcess,
	images []SociBenchmarkImage,
) ([]SociBenchmarkContainer, []func(), error) {
	containers := make([]SociBenchmarkContainer, len(images))
	cleanupContainers := make([]func(), len(images))
	for i, image := range images {
		container, cleanupContainer, err := sociContainerdProc.CreateSociContainer(ctx, *image.Image, *image.Desc)
		if err != nil {
			return nil, nil, err
		}
		containers[i] = SociBenchmarkContainer{
			Container: &container,
			Desc:      image.Desc,
		}
		cleanupContainers[i] = cleanupContainer
	}
	return containers, cleanupContainers, nil
}

func createSociTasksFromContainers(
	ctx context.Context,
	sociContainerdProc SociContainerdProcess,
	sociContainers []SociBenchmarkContainer,
) ([]SociBenchmarkTask, []func(), error) {
	tasks := make([]SociBenchmarkTask, len(sociContainers))
	cleanUps := make([]func(), len(sociContainers))
	for i, sociContainer := range sociContainers {
		taskDetails, cleanupTask, err := sociContainerdProc.CreateTask(ctx, *sociContainer.Container)
		if err != nil {
			return nil, nil, err
		}
		fmt.Printf("created task for %s ====> PID: %v\n", sociContainer.Desc.ShortName, taskDetails.Task().Pid())
		tasks[i] = SociBenchmarkTask{
			Task: taskDetails,
			Desc: sociContainer.Desc,
		}
		cleanUps[i] = cleanupTask
	}
	return tasks, cleanUps, nil
}

func pullImageWithSidecars(
	ctx context.Context,
	containerdProc *framework.ContainerdProcess,
	imageDescriptor ImageDescriptorWithSidecars,
	platform string,
) ([]SociBenchmarkImage, error) {
	// eg, ctx := errgroup.WithContext(ctx)
	var eg errgroup.Group
	lenSidecars := len(imageDescriptor.ImageOptions.Sidecars)
	imageChan := make(chan SociBenchmarkImage, lenSidecars+1) // +1 for the main image
	// pull sidecars
	for _, sideImageDesc := range imageDescriptor.ImageOptions.Sidecars {
		eg.Go(func() error {
			fmt.Println("pulling sidecar ====>", sideImageDesc.ImageRef)
			image, err := containerdProc.PullImageFromRegistry(
				ctx,
				sideImageDesc.ImageRef,
				platform,
			)
			if err != nil {
				return err
			}
			fmt.Println("done pulling sidecar ====>", sideImageDesc.ImageRef)
			imageChan <- SociBenchmarkImage{Image: &image, Desc: &sideImageDesc}
			return nil
		})
	}
	// pull main image
	eg.Go(func() error {
		fmt.Println("pulling image ====>", imageDescriptor.ImageRef)
		image, err := containerdProc.PullImageFromRegistry(
			ctx,
			imageDescriptor.ImageRef,
			platform,
		)
		if err != nil {
			return err
		}
		fmt.Println("done pulling image ====>", imageDescriptor.ImageRef)
		imageChan <- SociBenchmarkImage{Image: &image, Desc: imageDescriptor.ToImageDescriptor()}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(imageChan)

	images := make([]SociBenchmarkImage, 0, lenSidecars+1)
	for image := range imageChan {
		images = append(images, image)
	}
	return images, nil
}

func createContainersFromImages(
	ctx context.Context,
	containerdProc *framework.ContainerdProcess,
	images []SociBenchmarkImage,
) ([]SociBenchmarkContainer, []func(), error) {
	containers := make([]SociBenchmarkContainer, len(images))
	cleanupContainers := make([]func(), len(images))
	for i, image := range images {
		container, cleanupContainer, err := containerdProc.CreateContainer(ctx, image.Desc.ContainerOpts(*image.Image)...)
		if err != nil {
			return nil, nil, err
		}
		containers[i] = SociBenchmarkContainer{
			Container: &container,
			Desc:      image.Desc,
		}
		cleanupContainers[i] = cleanupContainer
	}
	return containers, cleanupContainers, nil
}

func createTasksFromContainers(
	ctx context.Context,
	containerdProc *framework.ContainerdProcess,
	containers []SociBenchmarkContainer,
) ([]SociBenchmarkTask, []func(), error) {
	tasks := make([]SociBenchmarkTask, len(containers))
	cleanUps := make([]func(), len(containers))
	for i, container := range containers {
		taskDetails, cleanupTask, err := containerdProc.CreateTask(ctx, *container.Container)
		if err != nil {
			return nil, nil, err
		}
		fmt.Printf("created task for %s ====> PID: %v\n", container.Desc.ShortName, taskDetails.Task().Pid())
		tasks[i] = SociBenchmarkTask{
			Task: taskDetails,
			Desc: container.Desc,
		}
		cleanUps[i] = cleanupTask
	}
	return tasks, cleanUps, nil
}

func runTasksWithSidecars(
	ctx context.Context,
	containerdProc *framework.ContainerdProcess,
	tasks []SociBenchmarkTask,
) ([]func(), error) {
	// eg, ctx := errgroup.WithContext(ctx)
	var eg errgroup.Group
	cleanUpChan := make(chan func(), len(tasks))
	for _, task := range tasks {
		eg.Go(func() error {
			fmt.Printf("running %s till readyline ====> %s\n", task.Desc.ShortName, task.Desc.ReadyLine)
			cleanupRun, err := containerdProc.RunContainerTaskForReadyLine(
				ctx,
				task.Task,
				task.Desc.ReadyLine,
				task.Desc.Timeout(),
			)
			if err != nil {
				return err
			}
			cleanUpChan <- cleanupRun
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(cleanUpChan)

	cleanUps := make([]func(), 0, len(tasks))
	for cleanup := range cleanUpChan {
		cleanUps = append(cleanUps, cleanup)
	}
	return cleanUps, nil
}
