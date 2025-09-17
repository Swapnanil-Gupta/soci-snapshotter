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

package cpumemtrace

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type TaskNum int

const (
	FirstTask TaskNum = iota
	SecondTask
)

func IntToTaskNum(i int) TaskNum {
	switch i {
	case 0:
		return FirstTask
	case 1:
		return SecondTask
	default:
		panic(fmt.Sprintf("invalid task number: %d", i))
	}
}

func Start(
	sociProcessCmd *exec.Cmd,
	testName string,
	testNum int,
	taskNum TaskNum,
	outDir string,
	intervalMs int,
) (func() error, error) {
	interval := time.Duration(intervalMs) * time.Millisecond
	sociPid := sociProcessCmd.Process.Pid

	outFile, err := os.Create(getCpuMemTraceOutPath(outDir, testName, testNum, taskNum))
	if err != nil {
		return nil, fmt.Errorf("failed to create cpu/mem trace file: %w", err)
	}
	writer := bufio.NewWriter(outFile)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				cmd := exec.Command("ps", "-p", fmt.Sprintf("%d", sociPid), "-o", "%cpu,rss")
				output, _ := cmd.Output()
				writer.WriteString(string(output))
				time.Sleep(interval)
			}
		}
	}()

	return func() error {
		cancel()
		err := writer.Flush()
		if err != nil {
			err = fmt.Errorf("failed to flush cpu/mem trace file: %w", err)
			return err
		}
		err = outFile.Close()
		if err != nil {
			err = fmt.Errorf("failed to close cpu/mem trace file: %w", err)
			return err
		}
		return nil
	}, nil
}

func getCpuMemTraceOutPath(outDir string, testName string, testNum int, taskNum TaskNum) string {
	return fmt.Sprintf("%s/%s_run_%d_task_%d.log", outDir, testName, testNum, taskNum+1)
}
