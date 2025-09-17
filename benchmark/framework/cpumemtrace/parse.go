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
	MaxCpuUsage float64 `json:"maxCpuUsage"`
	MaxRssUsage float64 `json:"maxRssUsage"`
}

func Parse(
	outDir string,
	testName string,
	numTests int,
) error {
	runs := make([]*run, numTests)
	for i := 1; i <= numTests; i++ {
		maxCpuUsages := make([]float64, 2)
		maxRssUsages := make([]float64, 2)
		for j := 0; j <= 1; j++ {
			maxCpuUsage := -1.0
			maxRssUsage := -1.0

			file, err := os.Open(getCpuMemTraceOutPath(outDir, testName, i, IntToTaskNum(j)))
			if err != nil {
				err = fmt.Errorf("failed to read cpu/mem trace output file: %w", err)
				return err
			}
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				cpuUsage, rssUsage := parseLine(line)
				maxCpuUsage = max(maxCpuUsage, cpuUsage)
				maxRssUsage = max(maxRssUsage, rssUsage)
			}

			maxCpuUsages[j] = maxCpuUsage
			maxRssUsages[j] = maxRssUsage
		}

		runs[i-1] = &run{
			Task1: &task{
				MaxCpuUsage: maxCpuUsages[0],
				MaxRssUsage: maxRssUsages[0],
			},
			Task2: &task{
				MaxCpuUsage: maxCpuUsages[1],
				MaxRssUsage: maxRssUsages[1],
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
	regex := regexp.MustCompile(`(?<cpu>\d+(?:\.\d+)?)\s+(?<rss>\d+)`)
	if matches := regex.FindStringSubmatch(line); len(matches) == 3 {
		cpuUsage, err := strconv.ParseFloat(matches[1], 64)
		if err != nil {
			return -1, -1
		}
		rssUsage, err := strconv.ParseFloat(matches[2], 64)
		if err != nil {
			return -1, -1
		}
		return cpuUsage, rssUsage
	}
	return -1, -1
}
