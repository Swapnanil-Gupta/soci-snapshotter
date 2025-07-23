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
	"fmt"
	"os"
	"os/exec"
)

var (
	scriptPath = "../kernel_trace.py"
	traceCmd   *exec.Cmd
	runNum     int
	enabled    bool
)

type RunType int

const (
	FirstRun RunType = iota
	SecondRun
)

func Start(monitorPath string, outputDir string, testName string, runType RunType) error {
	if !enabled {
		return nil
	}

	traceCmd = exec.Command(
		"python3",
		scriptPath,
		monitorPath,
		"--output="+getKernelTraceScriptOutPath(outputDir, testName, runNum, runType))
	stdoutFile, err := os.Create(outputDir + "/kernel-trace-stdout")
	if err != nil {
		return err
	}
	stderrFile, err := os.Create(outputDir + "/kernel-trace-stderr")
	if err != nil {
		return err
	}
	traceCmd.Stdout = stdoutFile
	traceCmd.Stderr = stderrFile
	if err := traceCmd.Start(); err != nil {
		return err
	}
	return nil
}

func Stop() error {
	if !enabled {
		return nil
	}

	if traceCmd != nil && traceCmd.Process != nil {
		if err := traceCmd.Process.Signal(os.Interrupt); err != nil {
			if err := traceCmd.Process.Kill(); err != nil {
				return err
			}
			return err
		}

		err := traceCmd.Wait()
		traceCmd = nil
		return err
	}
	return nil
}

func getKernelTraceScriptOutPath(outputDir string, testName string, runNum int, runType RunType) string {
	return fmt.Sprintf("%s/kernel_trace_script_out/%s_run_%d_task_%d.json", outputDir, testName, runNum, runType+1)
}

func Enable() {
	enabled = true
}

func IsEnabled() bool {
	return enabled
}

func IncCounter() {
	runNum++
}

func ResetCounter() {
	runNum = 0
}
