package kernel

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

var (
	traceCmd *exec.Cmd
	runNum   int
)

func StartTrace(outDir string) error {
	scriptPath := "../kernel_trace.py"
	scriptOutputPath := outDir + "/events_run_" + strconv.Itoa(runNum) + ".json"

	fmt.Println("Starting kernel trace...")
	traceCmd = exec.Command("python3", scriptPath, "/run/containerd", "--output="+scriptOutputPath)
	traceCmd.Stdout = os.Stdout
	traceCmd.Stderr = os.Stderr
	if err := traceCmd.Start(); err != nil {
		fmt.Println("Error running kernel trace script:", err)
		return err
	}
	fmt.Println("Kernel trace started.")
	runNum++
	return nil
}

func StopTrace() error {
	if traceCmd != nil && traceCmd.Process != nil {
		fmt.Println("Stopping kernel trace...")
		if err := traceCmd.Process.Kill(); err != nil {
			fmt.Println("Error stopping kernel trace script:", err)
			return err
		}
		fmt.Println("Kernel trace stopped.")
	}
	return nil
}
