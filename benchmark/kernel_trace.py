#!/usr/bin/env python3
#
# fs_monitor.py - Monitor filesystem operations on a specific path
#
# This script uses eBPF to trace filesystem operations and measure their duration.
    
from bcc import BPF
import argparse
import os
import signal
import time
from datetime import datetime
import json
import sys
    
# Parse command line arguments
parser = argparse.ArgumentParser(description='Monitor filesystem operations on a specific path')
parser.add_argument('path', help='Path to monitor')
parser.add_argument('output', help='Output file for raw event data (JSON)')
args = parser.parse_args()
    
# Normalize and get absolute path
target_path = os.path.abspath(args.path)
print(f"Monitoring filesystem operations on: {target_path}")
    
# BPF program
bpf_text = """
#include <uapi/linux/ptrace.h>
#include <linux/fs.h>
#include <linux/sched.h>
#include <linux/dcache.h>
    
// Define a structure to store operation data
struct operation_data_t {
    u64 ts;                 // Timestamp
    u64 duration;           // Duration in nanoseconds
    u32 pid;                // Process ID
    char comm[TASK_COMM_LEN]; // Command name
    char operation[32];     // Operation name
    char path[256];         // File path
    int fd;                 // File descriptor
};

// Map to store entry timestamps
BPF_HASH(start_time, u64, u64);
    
// Map to store file descriptor to path mappings
BPF_HASH(fd_to_path, u64, struct operation_data_t);
    
// Output channel for events
BPF_PERF_OUTPUT(events);
    
// Function to check if a path matches our target
static inline bool path_matches(const char *path) {
    char target[256] = TARGET_PATH;
    
    // Simple string prefix match
    int i;
    for (i = 0; target[i] != '\\0' && path[i] != '\\0'; i++) {
        if (target[i] != path[i])
            return false;
    }
    
    return target[i] == '\\0';
}
    
// Trace open syscall entry
TRACEPOINT_PROBE(syscalls, sys_enter_open) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    
    // Store the timestamp
    start_time.update(&id, &ts);
    
    // Store the path for later use
    struct operation_data_t data = {};
    data.ts = ts;
    data.pid = id >> 32;
    bpf_get_current_comm(&data.comm, sizeof(data.comm));
    bpf_probe_read_str(data.path, sizeof(data.path), (void *)args->filename);
    __builtin_memcpy(data.operation, "open", 5);
    
    // Only store if the path matches our target
    if (path_matches(data.path)) {
        fd_to_path.update(&id, &data);
    }
    
    return 0;
}
    
// Trace openat syscall entry (more common in newer systems)
TRACEPOINT_PROBE(syscalls, sys_enter_openat) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    
    // Store the timestamp
    start_time.update(&id, &ts);
    
    // Store the path for later use
    struct operation_data_t data = {};
    data.ts = ts;
    data.pid = id >> 32;
    bpf_get_current_comm(&data.comm, sizeof(data.comm));
    bpf_probe_read_str(data.path, sizeof(data.path), (void *)args->filename);
    __builtin_memcpy(data.operation, "openat", 7);
    
    // Only store if the path matches our target
    if (path_matches(data.path)) {
        fd_to_path.update(&id, &data);
    }
    
    return 0;
}
    
// Trace open syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_open) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);
    
    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, datap->operation, sizeof(event.operation));
        __builtin_memcpy(event.path, datap->path, sizeof(event.path));
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
        
        // Store fd->path mapping if successful
        int fd = args->ret;
        if (fd >= 0) {
            u64 key = ((u64)event.pid << 32) | (u64)fd;
            datap->fd = fd;
            fd_to_path.update(&key, datap);
        }
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
    
// Trace openat syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_openat) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);
    
    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, datap->operation, sizeof(event.operation));
        __builtin_memcpy(event.path, datap->path, sizeof(event.path));
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
        
        // Store fd->path mapping if successful
        int fd = args->ret;
        if (fd >= 0) {
            u64 key = ((u64)event.pid << 32) | (u64)fd;
            datap->fd = fd;
            fd_to_path.update(&key, datap);
        }
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
    
// Trace read syscall entry
TRACEPOINT_PROBE(syscalls, sys_enter_read) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    u64 key = ((u64)(id >> 32) << 32) | (u64)args->fd;
    
    // Check if this fd is one we're tracking
    struct operation_data_t *datap = fd_to_path.lookup(&key);
    if (datap != 0) {
        // Store the timestamp
        start_time.update(&id, &ts);
        // Store id to path
        fd_to_path.update(&id, datap);
    }
    
    return 0;
}
    
// Trace read syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_read) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);

    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, "read", 5);
        __builtin_memcpy(event.path, datap->path, sizeof(datap->path));
        event.fd = datap->fd;
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
    
// Trace write syscall entry
TRACEPOINT_PROBE(syscalls, sys_enter_write) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    u64 key = ((u64)(id >> 32) << 32) | (u64)args->fd;
    
    // Check if this fd is one we're tracking
    struct operation_data_t *datap = fd_to_path.lookup(&key);
    if (datap != 0) {
        // Store the timestamp
        start_time.update(&id, &ts);
        // Store id to path
        fd_to_path.update(&id, datap);
    }
    
    return 0;
}
    
// Trace write syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_write) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);

    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, "write", 6);
        __builtin_memcpy(event.path, datap->path, sizeof(datap->path));
        event.fd = datap->fd;
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
    
// Trace close syscall entry
TRACEPOINT_PROBE(syscalls, sys_enter_close) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    u64 key = ((u64)(id >> 32) << 32) | (u64)args->fd;
    
    // Check if this fd is one we're tracking
    struct operation_data_t *datap = fd_to_path.lookup(&key);
    if (datap != 0) {
        // Store the timestamp
        start_time.update(&id, &ts);
        // Store id to path
        fd_to_path.update(&id, datap);
    }
    
    return 0;
}
    
// Trace close syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_close) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);

    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, "close", 6);
        __builtin_memcpy(event.path, datap->path, sizeof(datap->path));
        event.fd = datap->fd;
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
    
// Trace newstat syscall entry (modern replacement for stat)
TRACEPOINT_PROBE(syscalls, sys_enter_newstat) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    
    // Store the path for later use
    struct operation_data_t data = {};
    data.ts = ts;
    data.pid = id >> 32;
    bpf_get_current_comm(&data.comm, sizeof(data.comm));
    bpf_probe_read_str(data.path, sizeof(data.path), (void *)args->filename);
    __builtin_memcpy(data.operation, "stat", 5);
    
    // Only store if the path matches our target
    if (path_matches(data.path)) {
        // Store the timestamp
        start_time.update(&id, &ts);
        fd_to_path.update(&id, &data);
    }
    
    return 0;
}
    
// Trace newstat syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_newstat) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);
    
    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, datap->operation, sizeof(event.operation));
        __builtin_memcpy(event.path, datap->path, sizeof(event.path));
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
    
// Trace newlstat syscall entry (modern replacement for lstat)
TRACEPOINT_PROBE(syscalls, sys_enter_newlstat) {
    u64 id = bpf_get_current_pid_tgid();
    u64 ts = bpf_ktime_get_ns();
    
    // Store the path for later use
    struct operation_data_t data = {};
    data.ts = ts;
    data.pid = id >> 32;
    bpf_get_current_comm(&data.comm, sizeof(data.comm));
    bpf_probe_read_str(data.path, sizeof(data.path), (void *)args->filename);
    __builtin_memcpy(data.operation, "lstat", 6);
    
    // Only store if the path matches our target
    if (path_matches(data.path)) {
        // Store the timestamp
        start_time.update(&id, &ts);
        fd_to_path.update(&id, &data);
    }
    
    return 0;
}
    
// Trace newlstat syscall return
TRACEPOINT_PROBE(syscalls, sys_exit_newlstat) {
    u64 id = bpf_get_current_pid_tgid();
    u64 *tsp = start_time.lookup(&id);
    struct operation_data_t *datap = fd_to_path.lookup(&id);
    
    if (tsp != 0 && datap != 0) {
        // Calculate duration
        u64 duration = bpf_ktime_get_ns() - *tsp;
        
        // Create event data
        struct operation_data_t event = {};
        event.ts = *tsp;
        event.duration = duration;
        event.pid = id >> 32;
        bpf_get_current_comm(&event.comm, sizeof(event.comm));
        __builtin_memcpy(event.operation, datap->operation, sizeof(event.operation));
        __builtin_memcpy(event.path, datap->path, sizeof(event.path));
        
        // Submit event
        events.perf_submit(args, &event, sizeof(event));
    }
    
    // Clean up
    start_time.delete(&id);
    fd_to_path.delete(&id);
    
    return 0;
}
"""
    
# Replace the target path placeholder with the actual path
bpf_text = bpf_text.replace('TARGET_PATH', f'"{target_path}"')
    
# Load the BPF program
b = BPF(text=bpf_text)
all_events = []
    
def add_event(cpu, data, size):
    event = b["events"].event(data)
    all_events.append({
        "timestamp": event.ts,
        "human_time": datetime.fromtimestamp(event.ts / 1000000000).strftime('%H:%M:%S'),
        "pid": event.pid,
        "comm": event.comm.decode('utf-8'),
        "operation": event.operation.decode('utf-8'),
        "path": event.path.decode('utf-8'),
        "fd": event.fd,
        "duration_ns": event.duration
    })

# Register event callback
b["events"].open_perf_buffer(add_event)
    
# Function to export data
def export_data():
    with open(args.output, 'w') as f:
        json.dump(all_events, f, indent=2)
    print(f"Exported {len(all_events)} events to {args.output}")
    
# Handle Ctrl+C
def signal_handler(sig, frame):
    export_data()
    print("\nKernel trace script exiting...")
    sys.exit(0)
    
signal.signal(signal.SIGINT, signal_handler)
    
# Main loop
print("Tracing filesystem operations... Press Ctrl+C to exit.")
print(f"Tracing target path: {target_path}")
    
while True:
    try:
        b.perf_buffer_poll(timeout=100)
    except KeyboardInterrupt:
        signal_handler(signal.SIGINT, None)