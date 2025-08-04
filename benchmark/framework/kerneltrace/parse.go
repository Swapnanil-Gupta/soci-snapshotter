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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/montanaflynn/stats"
)

type event struct {
	Command    string  `json:"comm"`
	Operation  string  `json:"operation"`
	Path       string  `json:"path"`
	DurationNS float64 `json:"duration_ns"`
}

type info struct {
	Run             int         `json:"run"`
	FirstTaskStats  []*taskStat `json:"firstTaskStats"`
	SecondTaskStats []*taskStat `json:"secondTaskStats"`
}

type taskStat struct {
	Syscall string `json:"syscall"`
	Stat    *stat  `json:"stat"`
}

type stat struct {
	Durations []float64 `json:"durations"`
	Mean      float64   `json:"mean"`
	Min       float64   `json:"min"`
	Max       float64   `json:"max"`
	StdDev    float64   `json:"stdDev"`
	Pct25     float64   `json:"pct25"`
	Pct50     float64   `json:"pct50"`
	Pct75     float64   `json:"pct75"`
	Pct90     float64   `json:"pct90"`
}

func Parse(outputDir string, testname string, numTests int) error {
	infos := []*info{}
	for i := 1; i <= numTests; i++ {
		firstTaskStats, err := getTaskStatsFromFile(
			getKernelTraceScriptOutPath(outputDir, testname, i),
			runNumContainerIdsMap[i].firstTask,
		)
		if err != nil {
			return err
		}
		secondTaskStats, err := getTaskStatsFromFile(
			getKernelTraceScriptOutPath(outputDir, testname, i),
			runNumContainerIdsMap[i].secondTask,
		)
		if err != nil {
			return err
		}

		infos = append(infos, &info{
			Run:             i,
			FirstTaskStats:  firstTaskStats,
			SecondTaskStats: secondTaskStats,
		})
	}

	json, err := json.MarshalIndent(infos, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s_results.json", outputDir, testname), json, 0644); err != nil {
		return err
	}
	return nil
}

func getTaskStatsFromFile(path string, containerId string) ([]*taskStat, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	events := []event{}
	if err := decoder.Decode(&events); err != nil {
		return nil, err
	}

	syscallDurationsMap := make(map[string][]float64)
	for _, e := range events {
		if !strings.Contains(e.Path, containerId) {
			continue
		}
		if durations, ok := syscallDurationsMap[e.Operation]; ok {
			syscallDurationsMap[e.Operation] = append(durations, e.DurationNS)
		} else {
			syscallDurationsMap[e.Operation] = []float64{e.DurationNS}
		}
	}

	taskStats := []*taskStat{}
	for syscall, durations := range syscallDurationsMap {
		syscallTaskStat := &taskStat{
			Syscall: syscall,
			Stat: &stat{
				Durations: durations,
			},
		}

		syscallTaskStat.Stat.Min, err = stats.Min(durations)
		if err != nil {
			fmt.Printf("Error Calculating Min: %v\n", err)
			syscallTaskStat.Stat.Min = -1
		}
		syscallTaskStat.Stat.Max, err = stats.Max(durations)
		if err != nil {
			fmt.Printf("Error Calculating Max: %v\n", err)
			syscallTaskStat.Stat.Max = -1
		}
		syscallTaskStat.Stat.Mean, err = stats.Mean(durations)
		if err != nil {
			fmt.Printf("Error Calculating Mean: %v\n", err)
			syscallTaskStat.Stat.Mean = -1
		}
		syscallTaskStat.Stat.StdDev, err = stats.StandardDeviation(durations)
		if err != nil {
			fmt.Printf("Error Calculating StdDev: %v\n", err)
			syscallTaskStat.Stat.StdDev = -1
		}
		syscallTaskStat.Stat.Pct25, err = stats.Percentile(durations, 25)
		if err != nil {
			fmt.Printf("Error Calculating 25th Pct: %v\n", err)
			syscallTaskStat.Stat.Pct25 = -1
		}
		syscallTaskStat.Stat.Pct50, err = stats.Percentile(durations, 50)
		if err != nil {
			fmt.Printf("Error Calculating 50th Pct: %v\n", err)
			syscallTaskStat.Stat.Pct50 = -1
		}
		syscallTaskStat.Stat.Pct75, err = stats.Percentile(durations, 75)
		if err != nil {
			fmt.Printf("Error Calculating 75th Pct: %v\n", err)
			syscallTaskStat.Stat.Pct75 = -1
		}
		syscallTaskStat.Stat.Pct90, err = stats.Percentile(durations, 90)
		if err != nil {
			fmt.Printf("Error Calculating 90th Pct: %v\n", err)
			syscallTaskStat.Stat.Pct90 = -1
		}

		taskStats = append(taskStats, syscallTaskStat)
	}

	return taskStats, nil
}
