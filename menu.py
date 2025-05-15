#!/usr/bin/env python3
import subprocess
import signal
import sys
import os
import json
import time

RUNNING_PROXIES_FILE = 'running_proxies.json'


def load_running_proxies():
    if not os.path.exists(RUNNING_PROXIES_FILE):
        return {}
    with open(RUNNING_PROXIES_FILE, 'r') as f:
        return json.load(f)


def save_running_proxies(proxies):
    with open(RUNNING_PROXIES_FILE, 'w') as f:
        json.dump(proxies, f)


def is_port_running(port, proxies):
    return str(port) in proxies


def start_proxy(port):
    # Launch proxy_server.py with port argument in background, detached from controlling terminal
    # Using nohup and setsid for Unix systems to keep running after session closed
    # This requires that python3 executable is in PATH
    command = [
        'nohup', sys.executable, 'proxy_server.py', str(port),
    ]
    # Redirect output to log file
    logfile = open(f'proxy_{port}.log', 'a')
    proc = subprocess.Popen(
        command,
        stdout=logfile,
        stderr=logfile,
        preexec_fn=os.setsid,
        close_fds=True
    )
    # Wait a moment for the proxy to start
    time.sleep(1)
    return proc.pid


def stop_proxy(port, pid):
    try:
        os.kill(pid, signal.SIGTERM)
        # Also kill process group to ensure subprocesses terminated
        os.killpg(os.getpgid(pid), signal.SIGTERM)
        return True
    except Exception as e:
        print(f"Error stopping proxy on port {port}: {e}")
        return False


def menu():
    proxies = load_running_proxies()

    while True:
        print("\nProxy Manager Menu")
        print("==================")
        print("1. Open port")
        print("2. Close port")
        print("3. List open ports")
        print("4. Exit")
        choice = input("Choose an option: ").strip()

        if choice == '1':
            port = input("Enter port number to open proxy: ").strip()
            if not port.isdigit():
                print("Invalid port number.")
                continue
            port = int(port)
            if is_port_running(port, proxies):
                print(f"Port {port} is already open.")
            else:
                pid = start_proxy(port)
                proxies[str(port)] = pid
                save_running_proxies(proxies)
                print(f"Started proxy on port {port} with PID {pid}")

        elif choice == '2':
            port = input("Enter port number to close proxy: ").strip()
            if not port.isdigit():
                print("Invalid port number.")
                continue
            port = int(port)
            if not is_port_running(port, proxies):
                print(f"Port {port} is not currently open.")
            else:
                pid = proxies[str(port)]
                if stop_proxy(port, pid):
                    print(f"Stopped proxy on port {port} with PID {pid}")
                    del proxies[str(port)]
                    save_running_proxies(proxies)
                else:
                    print(f"Failed to stop proxy on port {port}")

        elif choice == '3':
            if not proxies:
                print("No open proxy ports.")
            else:
                print("Open proxy ports and PIDs:")
                for p, pid in proxies.items():
                    print(f"  Port {p} -> PID {pid}")

        elif choice == '4':
            print("Exiting Proxy Manager.")
            sys.exit(0)

        else:
            print("Invalid choice. Please enter 1, 2, 3 or 4.")


if __name__ == "__main__":
    menu()

