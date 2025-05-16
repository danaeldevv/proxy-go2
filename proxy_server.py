#!/usr/bin/env python3
import sys
import socket
import threading
import select
import errno
import signal
import os
import time

SSH_HOST = '127.0.0.1'
SSH_PORT = 22
BUFFER_SIZE = 8192

def handle_client(client_sock, client_addr):
    try:
        # Peek first few bytes from client to detect protocol
        client_sock.settimeout(5)
        initial_data = client_sock.recv(1024, socket.MSG_PEEK)
        if not initial_data:
            client_sock.close()
            return

        # Simple detection: SOCKS5 starts with 0x05, HTTP (WS handshake) starts with GET or other methods
        if initial_data.startswith(b'\x05'):
            # SOCKS5 handshake
            client_sock.recv(1024)  # consume the handshake completely (simplified)
            # Respond with HTTP/1.1 101 switching protocols (per user request)
            response = b"HTTP/1.1 101 Switching Protocols\r\n\r\n"
            client_sock.sendall(response)
        else:
            # Assume HTTP/WebSocket handshake
            # Read full HTTP header (simplified: read until double \r\n)
            request_buffer = b""
            while b"\r\n\r\n" not in request_buffer:
                chunk = client_sock.recv(1024)
                if not chunk:
                    break
                request_buffer += chunk
            # Send HTTP/1.1 200 OK response as requested
            response = b"HTTP/1.1 200 OK\r\n\r\n"
            client_sock.sendall(response)

        # Connect to OpenSSH server
        ssh_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        ssh_sock.connect((SSH_HOST, SSH_PORT))

        # Forward data between client and ssh_sock bi-directionally
        sockets = [client_sock, ssh_sock]
        while True:
            rlist, _, _ = select.select(sockets, [], [])
            for s in rlist:
                data = b''
                try:
                    data = s.recv(BUFFER_SIZE)
                except socket.error as e:
                    if e.errno != errno.ECONNRESET:
                        raise
                if not data:
                    # Connection closed
                    client_sock.close()
                    ssh_sock.close()
                    return
                if s is client_sock:
                    ssh_sock.sendall(data)
                else:
                    client_sock.sendall(data)

    except Exception as e:
        # On any error, close sockets and exit thread
        try:
            client_sock.close()
        except:
            pass
        try:
            ssh_sock.close()
        except:
            pass

def run_proxy(port):
    server_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server_sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server_sock.bind(('0.0.0.0', port))
    server_sock.listen(10000)
    print(f"Proxy server listening on port {port}")

    while True:
        try:
            client_sock, client_addr = server_sock.accept()
        except OSError:
            # Socket closed externally
            break
        thread = threading.Thread(target=handle_client, args=(client_sock, client_addr))
        thread.daemon = True
        thread.start()

def daemonize():
    # Fork the current process
    if os.fork() > 0:
        # Exit the parent process
        sys.exit(0)

    # Create a new session
    os.setsid()

    # Fork again to ensure the daemon is not a session leader
    if os.fork() > 0:
        sys.exit(0)

    # Redirect standard file descriptors
    sys.stdout.flush()
    sys.stderr.flush()
    with open(os.devnull, 'r') as devnull:
        os.dup2(devnull.fileno(), sys.stdin.fileno())
    with open('proxy.log', 'a+') as log_file:
        os.dup2(log_file.fileno(), sys.stdout.fileno())
        os.dup2(log_file.fileno(), sys.stderr.fileno())

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: proxy_server.py <port>")
        sys.exit(1)

    try:
        proxy_port = int(sys.argv[1])
        
        # Daemonize the process
        daemonize()
        
        # Run the proxy server
        run_proxy(proxy_port)

    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)
