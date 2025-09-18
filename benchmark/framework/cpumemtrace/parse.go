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
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type test struct {
	TestName string `json:"testName"`
	NumTests int    `json:"numTests"`
	Runs     []*run `json:"runs"`
}

type run struct {
	Task1 *task `json:"task1"`
	Task2 *task `json:"task2"`
}

type task struct {
	MaxCpuUsagePercent float64 `json:"maxCpuUsagePercent"`
	MaxMemUsage        float64 `json:"maxMemUsage"`
}

var cpuMemParseRegex = regexp.MustCompile(`^(\d+\.\d+),(\d+\.\d+)$`)
var prevIdleTime = -1.0
var prevTotalTime = -1.0

func resetTimes() {
	prevIdleTime = -1.0
	prevTotalTime = -1.0
}

func Parse(
	outDir string,
	testName string,
	numTests int,
) error {
	runs := make([]*run, numTests)
	for i := 1; i <= numTests; i++ {
		maxCpuUsages := make([]float64, 2)
		maxMemUsages := make([]float64, 2)
		for j := 0; j <= 1; j++ {
			maxCpuUsage := -1.0
			maxMemUsage := -1.0

			file, err := os.Open(getCpuMemTraceOutPath(outDir, testName, i, IntToTaskNum(j)))
			if err != nil {
				err = fmt.Errorf("failed to read cpu/mem trace output file: %w", err)
				return err
			}
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				cpuUsage, memUsage := parseLine(line)
				maxCpuUsage = max(maxCpuUsage, cpuUsage)
				maxMemUsage = max(maxMemUsage, memUsage)
			}

			maxCpuUsages[j] = maxCpuUsage
			maxMemUsages[j] = maxMemUsage
		}

		runs[i-1] = &run{
			Task1: &task{
				MaxCpuUsagePercent: maxCpuUsages[0],
				MaxMemUsage:        maxMemUsages[0],
			},
			Task2: &task{
				MaxCpuUsagePercent: maxCpuUsages[1],
				MaxMemUsage:        maxMemUsages[1],
			},
		}
	}

	t := &test{
		TestName: testName,
		NumTests: numTests,
		Runs:     runs,
	}
	jsonTest, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		err = fmt.Errorf("failed to marshal cpu/mem test struct: %w", err)
		return err
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s_parsed_cpu_mem_usage.json", outDir, testName), jsonTest, 0644); err != nil {
		return err
	}
	return nil
}

func parseLine(line string) (float64, float64) {
	if matches := cpuMemParseRegex.FindStringSubmatch(line); len(matches) == 3 {
		cpuUsage, err := strconv.ParseFloat(matches[1], 64)
		if err != nil {
			return -1, -1
		}
		memUsage, err := strconv.ParseFloat(matches[2], 64)
		if err != nil {
			return -1, -1
		}
		return cpuUsage, memUsage
	}
	return -1, -1
}

func parseCpuPercentage(cpuOutput string) float64 {
	/*
		sample output:
		cpu  2866036 532648 3395867 486174341 194979 0 144080 22252 0 0
		cpu0 346315 237981 667328 60378225 22047 0 53592 1031 0 0
		cpu1 362740 43581 392929 60812672 16541 0 44022 10275 0 0
		cpu2 359290 44580 388211 60840113 15966 0 17803 1550 0 0
		cpu3 357928 41109 388545 60844853 15513 0 8820 926 0 0
		cpu4 362436 41567 386090 60842457 14215 0 6996 1123 0 0
		cpu5 358442 37654 390042 60812025 45472 0 5285 4991 0 0
		cpu6 359017 45822 390838 60837657 16752 0 3979 1359 0 0
		cpu7 359864 40350 391880 60806335 48469 0 3580 993 0 0
		intr 1435070955 29 10 0 0 747 0 0 0 0 0 0 0 88 0 0 0 0 0 0 0 0 0 0 0 15 4556147 3159590 639152 2736120 2127981 2041854 2410196 2928063 2371474 2458041 2334282 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
		ctxt 2574910619
		btime 1757611684
		processes 8163852
		procs_running 3
		procs_blocked 0
		softirq 239359185 0 22870145 123947 23006899 14 0 632089 97384795 174 95341122
	*/

	if cpuOutput == "" {
		return -1.0
	}
	lines := strings.Split(cpuOutput, "\n")
	if len(lines) == 0 {
		return -1.0
	}

	firstLine := lines[0][5:]
	fields := strings.Fields(firstLine)
	idleTime, _ := strconv.ParseFloat(fields[3], 64)
	totalTime := 0.0
	for _, field := range fields {
		parsedTime, _ := strconv.ParseFloat(field, 64)
		totalTime += parsedTime
	}

	cpuPercent := -1.0
	if prevIdleTime != -1.0 && prevTotalTime != -1.0 {
		deltaIdleTime := idleTime - prevIdleTime
		deltaTotalTime := totalTime - prevTotalTime
		cpuPercent = (1.0 - (float64(deltaIdleTime) / float64(deltaTotalTime))) * 100.0
	}
	prevIdleTime = idleTime
	prevTotalTime = totalTime
	return cpuPercent
}

func parseMemUsage(memOutput string) float64 {
	/*
		sample output:
		              total        used        free      shared  buff/cache   available
		Mem:          15628        3451       11376           1         801       11896
		Swap:             0           0           0
	*/

	if memOutput == "" {
		return -1.0
	}

	lines := strings.Split(memOutput, "\n")
	if len(lines) == 0 {
		return -1.0
	}

	secondLine := lines[1]
	fields := strings.Fields(secondLine)
	memUsage, _ := strconv.ParseFloat(fields[2], 64)
	return memUsage
}
