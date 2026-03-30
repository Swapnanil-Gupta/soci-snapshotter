package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	outDir := "/tmp/detailed_test"
	os.MkdirAll(outDir, 0755)
	
	fmt.Printf("Testing detailed monitoring in: %s\n", outDir)
	fmt.Printf("PID: %d\n", os.Getpid())

	var monitorCmds []*exec.Cmd

	// Detailed disk I/O
	diskioCmd := exec.Command("bash", "-c", fmt.Sprintf(
		"iostat -x -d 1 > %s/test_diskio.log 2>&1 &", outDir))
	diskioCmd.Start()
	monitorCmds = append(monitorCmds, diskioCmd)

	// Detailed process monitoring
	processCmd := exec.Command("bash", "-c", fmt.Sprintf(
		"pidstat -r -d -u -w -p %d 1 > %s/test_process_%d.log 2>&1 &", 
		os.Getpid(), outDir, os.Getpid()))
	processCmd.Start()
	monitorCmds = append(monitorCmds, processCmd)

	// Detailed network monitoring
	networkCmd := exec.Command("bash", "-c", fmt.Sprintf(
		"while true; do echo \"=== $(date) ===\"; ss -tuln -i; echo; sleep 1; done > %s/test_network.log 2>&1 &", outDir))
	networkCmd.Start()
	monitorCmds = append(monitorCmds, networkCmd)

	// Memory monitoring
	memoryCmd := exec.Command("bash", "-c", fmt.Sprintf(
		"while true; do echo \"=== $(date) ===\"; free -m; cat /proc/meminfo | head -10; echo; sleep 1; done > %s/test_memory.log 2>&1 &", outDir))
	memoryCmd.Start()
	monitorCmds = append(monitorCmds, memoryCmd)

	fmt.Printf("Started %d detailed monitors\n", len(monitorCmds))
	
	// Create intensive activity
	fmt.Println("Creating intensive disk/network activity...")
	for i := 0; i < 5; i++ {
		// Large file I/O
		data := make([]byte, 1024*1024) // 1MB
		for j := range data {
			data[j] = byte(j % 256)
		}
		os.WriteFile(fmt.Sprintf("%s/large_%d.tmp", outDir, i), data, 0644)
		
		// Network activity
		if i%2 == 0 {
			exec.Command("ping", "-c", "2", "8.8.8.8").Run()
		}
		
		time.Sleep(2 * time.Second)
	}

	fmt.Println("Stopping monitors...")
	
	// Kill all monitors
	for _, cmd := range monitorCmds {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}

	time.Sleep(1 * time.Second)

	// Show results
	fmt.Println("\nDetailed monitoring files generated:")
	files, _ := os.ReadDir(outDir)
	for _, file := range files {
		if file.Name()[len(file.Name())-4:] == ".log" {
			info, _ := file.Info()
			fmt.Printf("  📄 %s (%d bytes)\n", file.Name(), info.Size())
		}
	}
	
	fmt.Printf("\nLog files location: %s\n", outDir)
	fmt.Println("Each file contains detailed stats for extraction and analysis.")
}
