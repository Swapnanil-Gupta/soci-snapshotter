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
	traceCmd              *exec.Cmd
	runNum                int
	enabled               bool
	runNumContainerIdsMap = make(map[int]containerIds)
)

type containerIds struct {
	firstTask  string
	secondTask string
}

func Start(
	scriptPath string,
	monitorPath string,
	outputDir string,
	testName string,
) (func() error, error) {
	if !enabled {
		return nil, nil
	}

	traceCmd = exec.Command(
		"python3",
		scriptPath,
		monitorPath,
		getKernelTraceScriptOutPath(outputDir, testName, runNum),
	)
	if err := traceCmd.Start(); err != nil {
		return nil, err
	}

	return stop, nil
}

func stop() error {
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

func getKernelTraceScriptOutPath(outputDir string, testName string, runNum int) string {
	return fmt.Sprintf(
		"%s/%s_run_%d.json",
		outputDir,
		testName,
		runNum,
	)
}

func ReportContainerId(containerId string) {
	if c, ok := runNumContainerIdsMap[runNum]; ok {
		c.secondTask = containerId
	} else {
		runNumContainerIdsMap[runNum] = containerIds{
			firstTask: containerId,
		}
	}
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

func Reset() {
	runNum = 0
	runNumContainerIdsMap = make(map[int]containerIds)
}
