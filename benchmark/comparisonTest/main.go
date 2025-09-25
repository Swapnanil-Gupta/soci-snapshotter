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

package main

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/awslabs/soci-snapshotter/benchmark"
	"github.com/awslabs/soci-snapshotter/benchmark/framework"
	"github.com/awslabs/soci-snapshotter/benchmark/framework/cpumemtrace"
	"github.com/awslabs/soci-snapshotter/benchmark/framework/kerneltrace"
)

var (
	outputDir = "../comparisonTest/output"
)

func main() {

	var (
		numberOfTests           int
		jsonFile                string
		showCom                 bool
		traceKernelFileAccess   bool
		traceCpuMemUsage        bool
		cpuMemTraceIntervalMs   int
		kernelTraceScriptOutDir string
		cpuMemTraceOutDir       string
		imageList               []benchmark.ImageDescriptorWithSidecars
		err                     error
		commit                  string
	)

	flag.BoolVar(&showCom, "show-commit", false, "tag the commit hash to the benchmark results")
	flag.BoolVar(&traceKernelFileAccess, "trace-kernel-file-access", false, "Trace fuse file access patterns.")
	flag.BoolVar(&traceCpuMemUsage, "trace-cpu-mem-usage", false, "Trace CPU & memory usage.")
	flag.IntVar(&numberOfTests, "count", 5, "Describes the number of runs a benchmarker should run. Default: 5")
	flag.IntVar(&cpuMemTraceIntervalMs, "cpu-mem-trace-interval-ms", 1000, "Describes the interval of cpu/mem tracing in milliseconds. Default: 1000ms = 1s")
	flag.StringVar(&jsonFile, "f", "default", "Path to a json file describing image details in this order ['Name','Image ref', 'Ready line', 'manifest ref']")

	flag.Parse()

	if showCom {
		commit, _ = benchmark.GetCommitHash()
	} else {
		commit = "N/A"
	}

	if traceKernelFileAccess {
		kernelTraceScriptOutDir = outputDir + "/kernel_trace_out"
		err := os.RemoveAll(kernelTraceScriptOutDir)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(kernelTraceScriptOutDir, 0755)
		if err != nil {
			panic(err)
		}
	}

	if traceCpuMemUsage {
		cpuMemTraceOutDir = outputDir + "/cpu_mem_trace_out"
		err := os.RemoveAll(cpuMemTraceOutDir)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(cpuMemTraceOutDir, 0755)
		if err != nil {
			panic(err)
		}
	}

	if jsonFile == "default" {
		imageList = benchmark.GetDefaultWorkloadsWithSidecars()
	} else {
		imageList, err = benchmark.GetImageListWithSidecars(jsonFile)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to read file %s with error:%v\n", jsonFile, err)
			panic(errMsg)
		}
	}

	err = os.Mkdir(outputDir, 0755)
	if err != nil && !os.IsExist(err) {
		panic(err)
	}

	logFile, err := os.OpenFile(outputDir+"/benchmark_log", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0664)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()
	ctx, cancelCtx := framework.GetTestContext(logFile)
	defer cancelCtx()

	var drivers []framework.BenchmarkTestDriver
	for _, image := range imageList {
		shortName := image.ShortName

		overlayFsTestName := "OverlayFSFull" + shortName
		overlayFsTestDriver := framework.BenchmarkTestDriver{
			TestName:      overlayFsTestName,
			NumberOfTests: numberOfTests,
			TestFunction: func(b *testing.B, testNum int) {
				benchmark.OverlayFSFullRunWithSidecars(
					ctx,
					b,
					overlayFsTestName,
					testNum,
					image,
					traceCpuMemUsage,
					cpuMemTraceIntervalMs,
				)
			},
		}
		if traceKernelFileAccess {
			overlayFsTestDriver.AfterAllFunctions = append(overlayFsTestDriver.AfterAllFunctions, func() error {
				return kerneltrace.Parse(kernelTraceScriptOutDir, overlayFsTestName, numberOfTests)
			})
		}
		if traceCpuMemUsage {
			overlayFsTestDriver.BeforeEachFunctions = append(overlayFsTestDriver.BeforeEachFunctions, func() error {
				return cpumemtrace.DropCaches()
			})
		}
		if traceCpuMemUsage {
			overlayFsTestDriver.AfterAllFunctions = append(overlayFsTestDriver.AfterAllFunctions, func() error {
				return cpumemtrace.Parse(cpuMemTraceOutDir, overlayFsTestName, numberOfTests)
			})
		}
		drivers = append(drivers, overlayFsTestDriver)

		sociTestName := "SociFull" + shortName
		sociTestDriver := framework.BenchmarkTestDriver{
			TestName:      sociTestName,
			NumberOfTests: numberOfTests,
			TestFunction: func(b *testing.B, testNum int) {
				benchmark.SociFullRunWithSidecars(
					ctx,
					b,
					"SociFull"+shortName,
					testNum,
					image,
					traceCpuMemUsage,
					cpuMemTraceIntervalMs,
				)
			},
		}
		if traceKernelFileAccess {
			sociTestDriver.AfterAllFunctions = append(sociTestDriver.AfterAllFunctions, func() error {
				return kerneltrace.Parse(kernelTraceScriptOutDir, sociTestName, numberOfTests)
			})
		}
		if traceCpuMemUsage {
			sociTestDriver.BeforeEachFunctions = append(sociTestDriver.BeforeEachFunctions, func() error {
				return cpumemtrace.DropCaches()
			})
		}
		if traceCpuMemUsage {
			sociTestDriver.AfterAllFunctions = append(sociTestDriver.AfterAllFunctions, func() error {
				return cpumemtrace.Parse(cpuMemTraceOutDir, sociTestName, numberOfTests)
			})
		}
		drivers = append(drivers, sociTestDriver)

		sociFastPullTestName := "SociFastPullFull" + shortName
		sociFastPullTestDriver := framework.BenchmarkTestDriver{
			TestName:      sociFastPullTestName,
			NumberOfTests: numberOfTests,
			TestFunction: func(b *testing.B, testNum int) {
				benchmark.SociFastPullFullRunWithSidecars(
					ctx,
					b,
					sociFastPullTestName,
					testNum,
					image,
					traceCpuMemUsage,
					cpuMemTraceIntervalMs,
				)
			},
		}
		if traceKernelFileAccess {
			sociFastPullTestDriver.AfterAllFunctions = append(sociFastPullTestDriver.AfterAllFunctions, func() error {
				return kerneltrace.Parse(kernelTraceScriptOutDir, sociFastPullTestName, numberOfTests)
			})
		}
		if traceCpuMemUsage {
			sociFastPullTestDriver.BeforeEachFunctions = append(sociFastPullTestDriver.BeforeEachFunctions, func() error {
				return cpumemtrace.DropCaches()
			})
		}
		if traceCpuMemUsage {
			sociFastPullTestDriver.AfterAllFunctions = append(sociFastPullTestDriver.AfterAllFunctions, func() error {
				return cpumemtrace.Parse(cpuMemTraceOutDir, sociFastPullTestName, numberOfTests)
			})
		}
		drivers = append(drivers, sociFastPullTestDriver)
	}

	benchmarks := framework.BenchmarkFramework{
		OutputDir: outputDir,
		CommitID:  commit,
		Drivers:   drivers,
	}
	benchmarks.Run(ctx)
}
