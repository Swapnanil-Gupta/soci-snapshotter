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

package kerneltrace

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/containerd/containerd"
	"github.com/containerd/log"
)

var (
	runNum  int
	enabled bool
)

type TaskNum int

const (
	FirstTask TaskNum = iota
	SecondTask
)

func Start(
	ctx context.Context,
	containerTask containerd.Task,
	testName string,
	outDir string,
	taskNum TaskNum,
) (func() error, error) {
	if !enabled {
		return nil, nil
	}

	pids, err := getPids(ctx, containerTask)
	if err != nil {
		return nil, err
	}

	traceCmd := exec.Command("strace", getKernelTraceCmdArgs(
		pids,
		outDir,
		testName,
		taskNum,
	)...)
	traceCmd.Stdout = log.G(ctx).Writer()
	traceCmd.Stderr = log.G(ctx).Writer()
	if err := traceCmd.Start(); err != nil {
		return nil, err
	}

	return func() error {
		return stop(traceCmd)
	}, nil
}

func stop(traceCmd *exec.Cmd) error {
	err := traceCmd.Wait()
	return err

	// if traceCmd != nil && traceCmd.Process != nil {
	// 	if err := traceCmd.Process.Signal(os.Interrupt); err != nil {
	// 		if err := traceCmd.Process.Kill(); err != nil {
	// 			return err
	// 		}
	// 		return err
	// 	}

	// 	err := traceCmd.Wait()
	// 	return err
	// }
	// return nil
}

func getPids(ctx context.Context, task containerd.Task) ([]uint32, error) {
	pids := []uint32{}
	procs, err := task.Pids(ctx)
	if err != nil {
		return nil, err
	}
	for _, proc := range procs {
		pids = append(pids, proc.Pid)
	}
	return pids, nil
}

func getKernelTraceCmdArgs(
	pids []uint32,
	outDir string,
	testName string,
	taskNum TaskNum,
) []string {
	args := []string{
		"-e", "trace=file,read,write,getxattr,setxattr", // pread, pwrite
		"-ttt",
		"-y",
		"-T",
		"-o",
		getKernelTraceOutPath(outDir, testName, runNum, taskNum),
		"-f",
	}
	for _, pid := range pids {
		args = append(args, "-p", fmt.Sprintf("%d", pid))
	}
	return args
}

func getKernelTraceOutPath(outDir string, testName string, runNum int, taskNum TaskNum) string {
	return fmt.Sprintf(
		"%s/%s_run_%d_task_%d.log",
		outDir,
		testName,
		runNum,
		taskNum+1,
	)
}

func Enable() {
	enabled = true
}

func IsEnabled() bool {
	return enabled
}

func IncRunNum() {
	runNum++
}

func ResetRunNum() {
	runNum = 0
}
