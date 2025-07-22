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

type event struct {
	Command    string  `json:"comm"`
	Operation  string  `json:"operation"`
	Path       string  `json:"path"`
	DurationNS float64 `json:"duration_ns"`
}

type info struct {
	Run           int         `json:"run"`
	FirstTaskOps  []operation `json:"firstTaskOps"`
	SecondTaskOps []operation `json:"secondTaskOps"`
}

type operation struct {
	Operation string  `json:"operation"`
	Stats     opStats `json:"stats"`
}

type opStats struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}

type RunType int

const (
	FirstRun RunType = iota
	SecondRun
)

func Start(monitorPath string, outputDir string, testName string, runType RunType) error {
	if !enabled {
		return nil
	}

	fmt.Println("Starting kernel trace...")
	traceCmd = exec.Command("python3", scriptPath, monitorPath, "--output="+getPath(outputDir, testName, runNum, runType))
	traceCmd.Stdout = os.Stdout
	traceCmd.Stderr = os.Stderr
	if err := traceCmd.Start(); err != nil {
		fmt.Println("Error running kernel trace script:", err)
		return err
	}
	fmt.Println("Kernel trace started.")
	return nil
}

func Stop() error {
	if !enabled {
		return nil
	}

	if traceCmd != nil && traceCmd.Process != nil {
		fmt.Println("Stopping kernel trace...")
		if err := traceCmd.Process.Signal(os.Interrupt); err != nil {
			fmt.Println("Error stopping kernel trace script:", err)
			return err
		}
		fmt.Println("Kernel trace stopped.")
	}
	return nil
}

func getPath(outputDir string, testName string, runNum int, runType RunType) string {
	return fmt.Sprintf("%s/%s_run_%d_task_%d.json", outputDir, testName, runNum, runType)
}

func Enable() {
	enabled = true
}

func IncCounter() {
	runNum++
}

func ResetCounter() {
	runNum = 0
}
