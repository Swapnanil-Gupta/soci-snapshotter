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

func Parse(outputDir string, testname string, numTests int) error {
	infos := []*info{}
	for i := 1; i <= numTests; i++ {
		firstStats, err := getOpsFromFile(
			getKernelTraceScriptOutPath(outputDir, testname, i),
			runNumContainerIdsMap[i].first,
		)
		if err != nil {
			return err
		}
		secondStats, err := getOpsFromFile(
			getKernelTraceScriptOutPath(outputDir, testname, i),
			runNumContainerIdsMap[i].second,
		)
		if err != nil {
			return err
		}

		infos = append(infos, &info{
			Run:           i,
			FirstTaskOps:  firstStats,
			SecondTaskOps: secondStats,
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

func getOpsFromFile(path string, containerId string) ([]operation, error) {
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

	opMap := make(map[string][]float64)
	for _, e := range events {
		if !strings.Contains(e.Path, containerId) {
			continue
		}
		if durations, ok := opMap[e.Operation]; ok {
			opMap[e.Operation] = append(durations, e.DurationNS)
		} else {
			opMap[e.Operation] = []float64{e.DurationNS}
		}
	}

	resMap := []operation{}
	for k, v := range opMap {
		min, err := stats.Min(v)
		if err != nil {
			return nil, err
		}
		max, err := stats.Max(v)
		if err != nil {
			return nil, err
		}
		sum, err := stats.Sum(v)
		if err != nil {
			return nil, err
		}

		resMap = append(resMap, operation{
			Operation: k,
			Stats: opStats{
				Min: min,
				Max: max,
				Avg: sum / float64(len(v)),
			},
		})
	}
	return resMap, nil
}
