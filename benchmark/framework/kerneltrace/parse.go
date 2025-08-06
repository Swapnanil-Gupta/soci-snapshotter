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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/montanaflynn/stats"
)

type eventLog struct {
	TestName string      `json:"test_name"`
	Runs     []*eventRun `json:"runs"`
}

type eventRun struct {
	Tasks []*eventTask `json:"tasks"`
}

type eventTask struct {
	Events []*event `json:"events"`
}

type event struct {
	Timestamp string  `json:"timestamp"`
	Pid       string  `json:"pid"`
	Syscall   string  `json:"syscall"`
	Args      string  `json:"args"`
	Ret       string  `json:"ret"`
	Duration  float64 `json:"duration"`
}

type timingLog struct {
	TestName string       `json:"test_name"`
	Runs     []*timingRun `json:"runs"`
}

type timingRun struct {
	Tasks []*timingTask `json:"tasks"`
}

type timingTask struct {
	SyscallTimings []*syscallTiming `json:"syscall_timings"`
}

type syscallTiming struct {
	Syscall     string       `json:"syscall"`
	Timings     []*timing    `json:"timings"`
	TimingStats *timingStats `json:"timing_stats"`
}

type timing struct {
	Timestamp string  `json:"timestamp"`
	Duration  float64 `json:"duration"`
}

type timingStats struct {
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
	Pct25 float64 `json:"pct25"`
	Pct50 float64 `json:"pct50"`
	Pct75 float64 `json:"pct75"`
	Pct90 float64 `json:"pct90"`
}

func Parse(outDir string, testname string, numTests int) error {
	elog := &eventLog{
		TestName: testname,
		Runs:     make([]*eventRun, numTests),
	}
	tlog := &timingLog{
		TestName: testname,
		Runs:     make([]*timingRun, numTests),
	}

	for i := range numTests {
		firstEvents, err := getEventsFromFile(
			getKernelTraceOutPath(outDir, testname, i+1, FirstTask),
		)
		if err != nil {
			return err
		}
		firstSyscallTimings, err := getSyscallTimings(firstEvents)
		if err != nil {
			return err
		}
		secondEvents, err := getEventsFromFile(
			getKernelTraceOutPath(outDir, testname, i+1, SecondTask),
		)
		if err != nil {
			return err
		}
		secondSyscallTimings, err := getSyscallTimings(secondEvents)
		if err != nil {
			return err
		}
		elog.Runs[i] = &eventRun{
			Tasks: []*eventTask{
				{
					Events: firstEvents,
				},
				{
					Events: secondEvents,
				},
			},
		}
		tlog.Runs[i] = &timingRun{
			Tasks: []*timingTask{
				{
					SyscallTimings: firstSyscallTimings,
				},
				{
					SyscallTimings: secondSyscallTimings,
				},
			},
		}
	}

	jsonElogs, err := json.MarshalIndent(elog, " ", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s_parsed_events.json", outDir, testname), jsonElogs, 0644); err != nil {
		return err
	}
	jsonTlogs, err := json.MarshalIndent(tlog, " ", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s_parsed_timings.json", outDir, testname), jsonTlogs, 0644); err != nil {
		return err
	}

	return nil
}

func getEventsFromFile(path string) ([]*event, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// example : `12856 21:21:18.383943 newfstatat(3, "", {st_mode=S_IFREG|0755, st_size=1922136, ...}, AT_EMPTY_PATH) = 0 <0.000196>`
	reg := regexp.MustCompile(
		`^(?<pid>\d+)\s+(?<timestamp>[\d\.:]+)\s+(?<syscall>[a-z]+)\((?<args>.+)\)\s+=\s+(?<ret>.+)\s+<(?<duration>\d+\.\d{6})>$`,
	)
	scanner := bufio.NewScanner(file)
	events := []*event{}
	for scanner.Scan() {
		matches := reg.FindStringSubmatch(scanner.Text())
		if len(matches) == 0 {
			continue
		}
		res := make(map[string]string)
		for i, name := range reg.SubexpNames() {
			if i == 0 || name == "" {
				continue
			}
			res[name] = matches[i]
		}

		duration, err := strconv.ParseFloat(res["duration"], 64)
		if err != nil {
			duration = -1
		}
		events = append(events, &event{
			Timestamp: getMapVal(res, "timestamp"),
			Pid:       getMapVal(res, "pid"),
			Syscall:   getMapVal(res, "syscall"),
			Args:      getMapVal(res, "args"),
			Ret:       getMapVal(res, "ret"),
			Duration:  duration,
		})
	}
	return events, nil
}

func getMapVal(regMap map[string]string, key string) string {
	val, ok := regMap[key]
	if !ok {
		return ""
	}
	return strings.Trim(val, " ")
}

func getSyscallTimings(events []*event) ([]*syscallTiming, error) {
	syscallTimingsMap := make(map[string][]*timing)
	for _, event := range events {
		syscall := event.Syscall
		timestamp := event.Timestamp
		duration := event.Duration
		if syscall == "" || timestamp == "" || duration == -1 {
			continue
		}
		if _, ok := syscallTimingsMap[syscall]; ok {
			syscallTimingsMap[syscall] = append(syscallTimingsMap[syscall], &timing{
				Timestamp: timestamp,
				Duration:  duration,
			})
		} else {
			syscallTimingsMap[syscall] = []*timing{
				{
					Timestamp: timestamp,
					Duration:  duration,
				},
			}
		}
	}

	timings := []*syscallTiming{}
	for k, v := range syscallTimingsMap {
		s := getTimingStats(v)
		timings = append(timings, &syscallTiming{
			Syscall:     k,
			Timings:     v,
			TimingStats: s,
		})
	}
	return timings, nil
}

func getTimingStats(timings []*timing) *timingStats {
	durations := []float64{}
	for _, t := range timings {
		durations = append(durations, t.Duration)
	}
	min, err := stats.Min(durations)
	if err != nil {
		min = -1
	}
	max, err := stats.Max(durations)
	if err != nil {
		max = -1
	}
	mean, err := stats.Mean(durations)
	if err != nil {
		mean = -1
	}
	pct25, err := stats.Percentile(durations, 25)
	if err != nil {
		pct25 = -1
	}
	pct50, err := stats.Percentile(durations, 50)
	if err != nil {
		pct50 = -1
	}
	pct75, err := stats.Percentile(durations, 75)
	if err != nil {
		pct75 = -1
	}
	pct90, err := stats.Percentile(durations, 90)
	if err != nil {
		pct90 = -1
	}
	return &timingStats{
		Min:   min,
		Max:   max,
		Mean:  mean,
		Pct25: pct25,
		Pct50: pct50,
		Pct75: pct75,
		Pct90: pct90,
	}
}
