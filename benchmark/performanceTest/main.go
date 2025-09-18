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
	bparser "github.com/awslabs/soci-snapshotter/benchmark/framework/parser"
)

var (
	outputDir = "../performanceTest/output"
)

func main() {

	var (
		numberOfTests           int
		jsonFile                string
		showCom                 bool
		parseFileAccessPatterns bool
		traceKernelFileAccess   bool
		traceSociCpuMemUsage    bool
		cpuMemTraceIntervalMs   int
		kernelTraceScriptOutDir string
		sociCpuMemTraceOutDir   string
		commit                  string
		imageList               []benchmark.ImageDescriptor
		err                     error
	)

	flag.BoolVar(&parseFileAccessPatterns, "parse-file-access", false, "Parse fuse file access patterns.")
	flag.BoolVar(&showCom, "show-commit", false, "tag the commit hash to the benchmark results")
	flag.BoolVar(&traceKernelFileAccess, "trace-kernel-file-access", false, "Trace fuse file access patterns.")
	flag.BoolVar(&traceSociCpuMemUsage, "trace-soci-cpu-mem-usage", false, "Trace CPU & memory usage of SOCI.")
	flag.IntVar(&numberOfTests, "count", 5, "Describes the number of runs a benchmarker should run. Default: 5")
	flag.IntVar(&cpuMemTraceIntervalMs, "cpu-mem-trace-interval-ms", 1000, "Describes the interval of cpu/mem tracing in milliseconds. Default: 1000ms = 1s")
	flag.StringVar(&jsonFile, "f", "default", "Path to a json file describing image details in this order ['Name','Image ref', 'Ready line', 'manifest ref']")

	flag.Parse()

	if showCom {
		commit, _ = benchmark.GetCommitHash()
	} else {
		commit = "N/A"
	}

	if parseFileAccessPatterns {
		fileAccessDir := outputDir + "/file_access_logs"
		err := os.RemoveAll(fileAccessDir)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(fileAccessDir, 0755)
		if err != nil {
			panic(err)
		}
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

	if traceSociCpuMemUsage {
		sociCpuMemTraceOutDir = outputDir + "/cpu_mem_trace_out"
		err := os.RemoveAll(sociCpuMemTraceOutDir)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(sociCpuMemTraceOutDir, 0755)
		if err != nil {
			panic(err)
		}
	}

	if jsonFile == "default" {
		imageList = benchmark.GetDefaultWorkloads()
	} else {
		imageList, err = benchmark.GetImageList(jsonFile)
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
		testName := "SociFull" + shortName
		driver := framework.BenchmarkTestDriver{
			TestName:      testName,
			NumberOfTests: numberOfTests,
			TestFunction: func(b *testing.B, testNum int) {
				benchmark.SociFullRun(
					ctx,
					b,
					testName,
					testNum,
					image,
					traceKernelFileAccess,
					traceSociCpuMemUsage,
					cpuMemTraceIntervalMs,
				)
			},
		}
		if parseFileAccessPatterns {
			driver.AfterAllFunctions = append(driver.AfterAllFunctions, func() error {
				err := bparser.ParseFileAccesses(shortName)
				return err
			})
		}

		if traceKernelFileAccess {
			driver.AfterAllFunctions = append(driver.AfterAllFunctions, func() error {
				return kerneltrace.Parse(kernelTraceScriptOutDir, testName, numberOfTests)
			})
		}

		if traceSociCpuMemUsage {
			driver.BeforeEachFunctions = append(driver.BeforeEachFunctions, func() error {
				return cpumemtrace.DropCaches()
			})
		}

		if traceSociCpuMemUsage {
			driver.AfterAllFunctions = append(driver.AfterAllFunctions, func() error {
				return cpumemtrace.Parse(sociCpuMemTraceOutDir, testName, numberOfTests)
			})
		}

		drivers = append(drivers, driver)
	}

	benchmarks := framework.BenchmarkFramework{
		OutputDir: outputDir,
		CommitID:  commit,
		Drivers:   drivers,
	}
	benchmarks.Run(ctx)
}
