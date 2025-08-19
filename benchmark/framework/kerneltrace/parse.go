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

type unfinishedEvent struct {
	Pid       string
	Timestamp string
	Syscall   string
	Args      string
}
type resumedEvent struct {
	Pid       string
	Timestamp string
	Syscall   string
	Args      string
	Ret       string
	Duration  float64
}

func Parse(outDir string, testname string, numTests int) error {
	events := &eventLog{
		TestName: testname,
		Runs:     make([]*eventRun, numTests),
	}
	eventsAfterSentinel := &eventLog{
		TestName: testname,
		Runs:     make([]*eventRun, numTests),
	}

	for i := range numTests {
		firstEvents, firstEventsAfterSentinel := getEventsFromFile(
			getKernelTraceOutPath(outDir, testname, i+1, FirstTask),
		)
		secondEvents, secondEventsAfterSentinel := getEventsFromFile(
			getKernelTraceOutPath(outDir, testname, i+1, SecondTask),
		)
		events.Runs[i] = &eventRun{
			Tasks: []*eventTask{
				{
					Events: firstEvents,
				},
				{
					Events: secondEvents,
				},
			},
		}
		eventsAfterSentinel.Runs[i] = &eventRun{
			Tasks: []*eventTask{
				{
					Events: firstEventsAfterSentinel,
				},
				{
					Events: secondEventsAfterSentinel,
				},
			},
		}
	}

	jsonEventLogs, err := json.MarshalIndent(events, " ", " ")
	if err != nil {
		return err
	}
	jsonEventLogsAfterSentinel, err := json.MarshalIndent(eventsAfterSentinel, " ", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s_parsed_events.json", outDir, testname), jsonEventLogs, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s_parsed_events_after_sentinel.json", outDir, testname), jsonEventLogsAfterSentinel, 0644); err != nil {
		return err
	}

	return nil
}

func getEventsFromFile(path string) ([]*event, []*event) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return []*event{}, []*event{}
	}

	file, err := os.Open(path)
	if err != nil {
		return []*event{}, []*event{}
	}
	defer file.Close()

	// example: "6766  1755041143.660596 access("/etc/ld.so.preload", R_OK) = -1 ENOENT (No such file or directory) <0.000624>"
	finishedSyscallReg := regexp.MustCompile(
		`^(?<pid>\d+)\s+(?<timestamp>[\d\.:]+)\s+(?<syscall>[a-z]+)\((?<args>.+)\)\s+=\s+(?<ret>.+)\s+<(?<duration>\d+\.\d{6})>$`,
	)
	// example: "6766 1755041143.584802 execve("/usr/bin/python", ["python", "./train.py"], 0xc0001ae4b0 /* 4 vars */ <unfinished ...>"
	unfinishedSyscallReg := regexp.MustCompile(
		`^(?<pid>\d+)\s+(?<timestamp>[\d\.:]+)\s+(?<syscall>[a-z]+)\((?<args>.+)\s<unfinished\s\.\.\.>$`,
	)
	// example: "6766 1755041143.659896 <... execve resumed> ) = 0 <0.074956>"
	resumedSyscallReg := regexp.MustCompile(
		`^(?<pid>\d+)\s+(?<timestamp>[\d:\.]+)\s+<\.+\s+(?<syscall>[a-z]+)\s+resumed>(?<args>.+)\)\s+=\s+(?<ret>.+)\s+<(?<duration>\d\.\d{6})>`,
	)

	scanner := bufio.NewScanner(file)
	allEvents := []*event{}
	eventsAfterSentinel := []*event{}
	unfinishedMap := make(map[string]*unfinishedEvent)
	for scanner.Scan() {
		line := scanner.Text()

		// reset after sentinel events list when sentinel is encountered
		if strings.Contains(line, "__SENTINEL__") {
			eventsAfterSentinel = []*event{}
			continue
		}
		if finishedMatches := finishedSyscallReg.FindStringSubmatch(line); len(finishedMatches) > 0 {
			m := getMapFromMatches(finishedSyscallReg, finishedMatches)
			duration, err := strconv.ParseFloat(getMapVal(m, "duration"), 64)
			if err != nil {
				duration = 0
			}
			e := &event{
				Timestamp: getMapVal(m, "timestamp"),
				Pid:       getMapVal(m, "pid"),
				Syscall:   getMapVal(m, "syscall"),
				Args:      getMapVal(m, "args"),
				Ret:       getMapVal(m, "ret"),
				Duration:  duration,
			}
			allEvents = append(allEvents, e)
			eventsAfterSentinel = append(eventsAfterSentinel, e)
		} else if unfinishedMatches := unfinishedSyscallReg.FindStringSubmatch(line); len(unfinishedMatches) > 0 {
			m := getMapFromMatches(unfinishedSyscallReg, unfinishedMatches)
			ue := &unfinishedEvent{
				Pid:       getMapVal(m, "pid"),
				Timestamp: getMapVal(m, "timestamp"),
				Syscall:   getMapVal(m, "syscall"),
				Args:      getMapVal(m, "args"),
			}
			unfinishedMap[ue.Pid+"_"+ue.Syscall] = ue
		} else if resumedMatches := resumedSyscallReg.FindStringSubmatch(line); len(resumedMatches) > 0 {
			m := getMapFromMatches(resumedSyscallReg, resumedMatches)
			duration, err := strconv.ParseFloat(getMapVal(m, "duration"), 64)
			if err != nil {
				duration = 0
			}
			re := &resumedEvent{
				Pid:       getMapVal(m, "pid"),
				Timestamp: getMapVal(m, "timestamp"),
				Syscall:   getMapVal(m, "syscall"),
				Args:      getMapVal(m, "args"),
				Ret:       getMapVal(m, "ret"),
				Duration:  duration,
			}
			if ue, ok := unfinishedMap[re.Pid+"_"+re.Syscall]; ok {
				e := &event{
					Timestamp: ue.Timestamp,
					Pid:       ue.Pid,
					Syscall:   ue.Syscall,
					Args:      ue.Args + " " + re.Args,
					Ret:       re.Ret,
					Duration:  re.Duration,
				}
				allEvents = append(allEvents, e)
				eventsAfterSentinel = append(eventsAfterSentinel, e)
			}
		}
	}

	if len(eventsAfterSentinel) == len(allEvents) {
		// no sentinel encountered, so set to nil
		eventsAfterSentinel = []*event{}
	}

	return allEvents, eventsAfterSentinel
}

func getMapFromMatches(reg *regexp.Regexp, matches []string) map[string]string {
	res := make(map[string]string)
	for i, name := range reg.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		res[name] = matches[i]
	}
	return res
}

func getMapVal(m map[string]string, key string) string {
	val, ok := m[key]
	if !ok {
		return ""
	}
	return strings.Trim(val, " ")
}
